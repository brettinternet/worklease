#!/usr/bin/env python3
"""Reproducible remote-authority load run; retains private evidence under --output.

Requires a built worklease binary and Python 3. Runs only against a freshly initialized
loopback authority, never against the caller's configured authority. No cleanup is
performed: inspect the retained private installation and server artifacts afterward.
"""

import argparse
import concurrent.futures
from datetime import datetime
import json
import os
from pathlib import Path
import socket
import subprocess
import sys
import threading
import time


def percentile(samples, fraction):
    ordered = sorted(samples)
    index = (len(ordered) - 1) * fraction
    low = int(index)
    return round(ordered[low] + (ordered[min(low + 1, len(ordered) - 1)] - ordered[low]) * (index - low), 4)


def command(binary, arguments, env, timeout=60):
    started = time.monotonic()
    completed = subprocess.run([str(binary), "--json", *arguments], env=env, capture_output=True, text=True, timeout=timeout)
    if completed.returncode:
        raise RuntimeError(f"worklease {arguments[0]} failed (exit {completed.returncode}): {(completed.stdout + completed.stderr)[:700]}")
    response = json.loads(completed.stdout)
    if not response.get("ok"):
        raise RuntimeError(f"worklease {arguments[0]}: {response.get('error', response)}")
    return response, time.monotonic() - started


def stats(samples):
    return {"count": len(samples), "p50Ms": round(percentile(samples, 0.5) * 1000, 2),
            "p95Ms": round(percentile(samples, 0.95) * 1000, 2),
            "p99Ms": round(percentile(samples, 0.99) * 1000, 2)}


def environment(root, certificate=None):
    env = dict(os.environ)
    for name in ("WORKLEASE_PROFILE", "WORKLEASE_CONFIG", "WORKLEASE_HOME", "WORKLEASE_SESSION_ID", "WORKLEASE_HANDLE"):
        env.pop(name, None)
    env.update(XDG_CONFIG_HOME=str(root / "config"), XDG_STATE_HOME=str(root / "state"),
               WORKLEASE_HOME=str(root / "home"))
    if certificate:
        env["SSL_CERT_FILE"] = str(certificate)
    return env


def one_claim(binary, env, handle, number, ttl):
    response, elapsed = command(binary, ["--profile", "bench", "acquire", "--resource", f"coordination:benchmark:{number}",
                                         "--handle", str(handle), "--session", f"benchmark-{number}", "--ttl", ttl], env)
    return response, elapsed


def one_renew(binary, env, handle, ttl, previous_expiry=None):
    result, elapsed = command(binary, ["--profile", "bench", "heartbeat", "--handle", str(handle), "--ttl", ttl], env)
    margin = None if previous_expiry is None else datetime.fromisoformat(previous_expiry.replace("Z", "+00:00")).timestamp() - time.time()
    return result, elapsed, margin


def snapshot_process(pid, database):
    process = subprocess.run(["ps", "-p", str(pid), "-o", "time=,rss="], capture_output=True, text=True, check=True)
    cpu, rss = process.stdout.strip().split()
    wal = Path(str(database) + "-wal")
    return {"cpuTime": cpu, "rssKiB": int(rss), "walBytes": wal.stat().st_size if wal.exists() else 0,
            "databaseBytes": database.stat().st_size}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True, help="built worklease executable")
    parser.add_argument("--output", type=Path, required=True, help="new, private directory for evidence and credentials")
    parser.add_argument("--clients", type=int, default=25)
    parser.add_argument("--claims", type=int, default=500)
    parser.add_argument("--warmup-seconds", type=int, default=75)
    parser.add_argument("--short-ttl", default="30s")
    parser.add_argument("--catch-up-limit-seconds", type=int, default=600,
                        help="fail when any overlay has not drained the renewal events to head by then")
    args = parser.parse_args()
    binary = args.binary.resolve(strict=True)
    output = args.output.resolve()
    if output.exists() or args.clients < 1 or args.claims < args.clients:
        parser.error("--output must not exist, and claims must be at least clients")
    output.mkdir(mode=0o700, parents=True)
    if output.stat().st_mode & 0o077:
        parser.error("--output must be owner-private")
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        port = listener.getsockname()[1]
    address = f"127.0.0.1:{port}"
    root = output / "server"
    root.mkdir(mode=0o700)
    server_env = environment(root)
    configuration = output / "server.yaml"
    bootstrap = output / "bootstrap.invite"
    setup, _ = command(binary, ["server", "init", "--guided", "--server-config", str(configuration),
                                "--bootstrap-invite-file", str(bootstrap), "--listen", address,
                                "--endpoint", f"https://{address}", "--transport", "tls",
                                "--admitted-prefix", "coordination:"], server_env)
    certificate = Path(setup["certificateFile"])
    # Enrollment is fixture setup, not load under test. Each enrollment also
    # queries metadata; raise both setup limits in the disposable authority.
    generated = configuration.read_text()
    if "enrollmentRate: 20\n" not in generated or "metadataRate: 60\n" not in generated:
        raise RuntimeError("unexpected generated setup rates")
    configuration.write_text(generated.replace("enrollmentRate: 20\n", "enrollmentRate: 1000\n")
                                      .replace("metadataRate: 60\n", "metadataRate: 20000\n"))
    log = (output / "server.log").open("w")
    server = subprocess.Popen([str(binary), "serve", "--server-config", str(configuration)],
                              env=server_env, stdout=log, stderr=subprocess.STDOUT)
    report = {"schemaVersion": 1, "clients": args.clients, "claims": args.claims, "ttlSeconds": 600,
              "watchTimeoutSeconds": 1, "watchPollDefaultMs": 250, "serverPID": server.pid,
              "authorityId": setup["authorityId"], "startedAt": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())}
    try:
        (output / "server.pid").write_text(str(server.pid) + "\n")
        for _ in range(100):
            if server.poll() is not None:
                raise RuntimeError("authority exited; see server.log")
            try:
                command(binary, ["--profile", "bench", "doctor", "--resource", "coordination:benchmark:probe"],
                        environment(output / "clients" / "0", certificate), timeout=2)
                break
            except Exception:
                time.sleep(0.1)
        # Bootstrap enrollment also configures the pinned profile for client zero.
        clients = []
        first = environment(output / "clients" / "0", certificate)
        command(binary, ["enroll", "--profile", "bench", "--invite-file", str(bootstrap), "--label", "bench-0"], first)
        clients.append(first)
        for index in range(1, args.clients):
            invite = output / f"client-{index}.invite"
            command(binary, ["--profile", "bench", "invite", "issue", "--role", "write", "--invite-file", str(invite),
                             "--label", f"bench-{index}"], first)
            env = environment(output / "clients" / str(index), certificate)
            # Enrollment has a separate authority rate limit. This is setup,
            # not part of the measured claim workload.
            for attempt in range(10):
                try:
                    command(binary, ["enroll", "--profile", "bench", "--invite-file", str(invite), "--label", f"bench-{index}"], env)
                    break
                except RuntimeError as error:
                    if '"reason":"rate-limited"' not in str(error) or attempt == 9:
                        raise
                    time.sleep(1)
            clients.append(env)
        handles = [output / "clients" / str(i % args.clients) / "handles" / f"claim-{i}.json" for i in range(args.claims)]
        for handle in handles:
            handle.parent.mkdir(parents=True, exist_ok=True)
        with concurrent.futures.ThreadPoolExecutor(max_workers=args.clients) as pool:
            burst = list(pool.map(lambda i: one_claim(binary, clients[i % args.clients], handles[i], i, "10m"), range(args.claims)))
        report["burstAcquire"] = stats([elapsed for _, elapsed in burst])
        database = root / "state" / "worklease" / "server" / "worklease.db"
        if not database.exists():
            raise RuntimeError(f"authority database not found at {database}")
        report["beforeWatch"] = snapshot_process(server.pid, database)
        # Each client keeps one namespace watch and refreshes its 20-resource
        # overlay projection with one batch status read after every watch result.
        # Cursor is obtained from events.
        stop = threading.Event()
        renewal_done = threading.Event()
        renewal_done_at = [0.0]
        ready = threading.Barrier(args.clients + 1, timeout=30)
        active_lock = threading.Lock()
        active = [0]
        def watch_client(index):
            env = clients[index]
            cursor, _ = command(binary, ["--profile", "bench", "events", "--limit", "1"], env)
            position = cursor["nextCursor"]
            polls = status_reads = 0
            estimated_store_polls = 0
            renewed_events = gaps = 0
            catch_up_seconds = None
            resources = [f"coordination:benchmark:{i}" for i in range(index, args.claims, args.clients)]
            with active_lock:
                active[0] += 1
            try:
                ready.wait()
                while not stop.is_set():
                    # Checked before the watch: a timeout observed after the
                    # renewal burst ended proves this client drained to head.
                    drained_after_renewal = renewal_done.is_set()
                    watched, wait_seconds = command(binary, ["--profile", "bench", "watch", "--cursor", position, "--timeout", "1s"], env, timeout=5)
                    if (watched.get("event") or {}).get("kind") == "renewed":
                        renewed_events += 1
                    # Derived from elapsed wall time and the server's 250 ms poll
                    # interval, not a direct SQLite statement counter.
                    estimated_store_polls += 1 + int(wait_seconds / 0.25)
                    position = watched["nextCursor"]
                    if watched.get("gap"):
                        gaps += 1
                        cursor, _ = command(binary, ["--profile", "bench", "events", "--limit", "1"], env)
                        position = cursor["nextCursor"]
                    status_args = ["--profile", "bench", "status"]
                    for resource in resources:
                        status_args.extend(["--resource", resource])
                    command(binary, status_args, env)
                    polls += 1
                    status_reads += 1
                    if drained_after_renewal and watched.get("timedOut"):
                        catch_up_seconds = time.monotonic() - renewal_done_at[0]
                        break
            finally:
                with active_lock:
                    active[0] -= 1
            return polls, status_reads, estimated_store_polls, renewed_events, catch_up_seconds, gaps
        with concurrent.futures.ThreadPoolExecutor(max_workers=args.clients) as watch_pool:
            watches = [watch_pool.submit(watch_client, i) for i in range(args.clients)]
            try:
                ready.wait()
                watch_start = time.monotonic()
                # Allow the authority's mutation quota to reset after the acquire
                # burst while staying inside half of the ten-minute TTL.
                time.sleep(min(65, args.warmup_seconds / 2 if args.warmup_seconds < 65 else 65))
                with active_lock:
                    report["watchersAtRenewalStart"] = active[0]
                if report["watchersAtRenewalStart"] != args.clients or any(watch.done() for watch in watches):
                    raise RuntimeError("not all namespace watches survived until renewal")
                with concurrent.futures.ThreadPoolExecutor(max_workers=args.clients) as renewal_pool:
                    renewal = list(renewal_pool.map(lambda i: one_renew(binary, clients[i % args.clients], handles[i], "10m", burst[i][0]["expiresAt"]), range(args.claims)))
                with active_lock:
                    report["watchersAtRenewalEnd"] = active[0]
                if report["watchersAtRenewalEnd"] != args.clients or any(watch.done() for watch in watches):
                    raise RuntimeError("namespace watch ended during renewal")
                # Every client's overlay must consume the renewal events and
                # reach head (an empty watch) before the run is accepted.
                renewal_done_at[0] = time.monotonic()
                renewal_done.set()
                _, pending = concurrent.futures.wait(watches, timeout=args.catch_up_limit_seconds)
                if pending:
                    raise RuntimeError(f"{len(pending)} watch clients did not drain to head within {args.catch_up_limit_seconds}s")
            finally:
                stop.set()
            report["watchDurationSeconds"] = round(time.monotonic() - watch_start, 3)
            report["watches"] = [watch.result() for watch in watches]
        catch_up = [row[4] for row in report["watches"]]
        report["overlayCatchUp"] = {"clients": len(catch_up), "maxSeconds": round(max(catch_up), 3),
                                    "p50Seconds": percentile(catch_up, 0.5),
                                    "renewedEventsPerClientMin": min(row[3] for row in report["watches"]),
                                    "renewedEventsPerClientMax": max(row[3] for row in report["watches"]),
                                    "gaps": sum(row[5] for row in report["watches"]),
                                    "limitSeconds": args.catch_up_limit_seconds}
        if report["overlayCatchUp"]["renewedEventsPerClientMin"] < args.claims:
            raise RuntimeError(f"a watch client drained without observing every renewal event: {report['overlayCatchUp']}")
        report["renewal"] = stats([elapsed for _, elapsed, _ in renewal])
        margins = [margin for _, _, margin in renewal]
        report["minimumPreviousTTLAtCompletionSeconds"] = round(min(margins), 3)
        report["p99RenewalPreviousTTLMarginSeconds"] = round(percentile(margins, 0.01), 3)
        report["budgetMet"] = report["p99RenewalPreviousTTLMarginSeconds"] >= 300
        report["observedWatchResponses"] = sum(row[0] for row in report["watches"])
        # Modeled from elapsed wall time and the 250 ms poll interval; the
        # authority exposes no store-read counter, so this is not a measurement.
        report["estimatedStorePolls"] = sum(row[2] for row in report["watches"])
        report["estimatedStorePollsPerSecond"] = round(report["estimatedStorePolls"] / report["watchDurationSeconds"], 2)
        report["modeledSteadyStorePollsPerSecond"] = args.clients * 1000 // report["watchPollDefaultMs"]
        report["afterWatch"] = snapshot_process(server.pid, database)
        # Saturation after the renewal burst is intentional; report rejected
        # operations, but never replay an ambiguous/pending mutation.
        def short_renew(index, previous_expiry):
            try:
                result, elapsed, margin = one_renew(binary, clients[index % args.clients], handles[index], args.short_ttl, previous_expiry)
                return elapsed, None, margin, result["receipt"]["result"]["expiresAt"]
            except RuntimeError as error:
                if '"reason":"rate-limited"' not in str(error) or '"commitState":"not-committed"' not in str(error):
                    raise
                return None, "rate-limited", None, None
        with concurrent.futures.ThreadPoolExecutor(max_workers=args.clients) as pool:
            short = list(pool.map(lambda i: short_renew(i, renewal[i][0]["receipt"]["result"]["expiresAt"]), range(args.claims)))
        report["shortTTL"] = stats([row[0] for row in short if row[0] is not None])
        report["shortTTL"]["rejected"] = sum(row[1] is not None for row in short)
        # A second short renewal within the same minute tests steady-state
        # admission, not merely the first 30-second TTL change.
        time.sleep(5)
        with concurrent.futures.ThreadPoolExecutor(max_workers=args.clients) as pool:
            saturated = list(pool.map(lambda i: short_renew(i, short[i][3]), range(args.claims)))
        report["shortTTLSaturation"] = stats([row[0] for row in saturated if row[0] is not None]) if any(row[0] is not None for row in saturated) else {"count": 0}
        report["shortTTLSaturation"]["rejected"] = sum(row[1] is not None for row in saturated)
        report["shortTTLSaturation"]["minimumPreviousTTLAtCompletionSeconds"] = round(min(row[2] for row in saturated if row[2] is not None), 3) if any(row[2] is not None for row in saturated) else None
        report["completedAt"] = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
        (output / "report.json").write_text(json.dumps(report, indent=2) + "\n")
        print(json.dumps(report, indent=2))
    finally:
        server.terminate()
        try:
            server.wait(timeout=10)
        except subprocess.TimeoutExpired:
            server.kill()
            server.wait()
        log.close()
        print(f"Private evidence retained at {output}", file=sys.stderr)


if __name__ == "__main__":
    main()
