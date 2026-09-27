#!/usr/bin/env python3
"""Reference queue worker: confirm the supervised run holds the exact handoff.

The queue starts launch actions under `worklease run`, which acquires the exact
resources, renews the claim, and releases it when the worker exits. A worker
only verifies that the claim it inherited matches the handoff before working,
and may write a structured result to WORKLEASE_RUN_RESULT.
"""

import json
import os
import subprocess
import sys


def main():
    expected = os.environ.get("WORKLEASE_QUEUE_AUTHORITY_ID", "")
    resources = json.loads(os.environ.get("WORKLEASE_QUEUE_RESOURCES", "null"))
    session = os.environ.get("WORKLEASE_SESSION_ID", "")
    if not expected or not isinstance(resources, list) or not 1 <= len(resources) <= 32 or any(
        not isinstance(key, str) or not key or "\n" in key for key in resources
    ):
        raise ValueError("invalid queue authority or resources")
    if not session:
        raise ValueError("not started by worklease run; no inherited claim session")
    observed = subprocess.run(
        ["worklease", "queue", "authority-id", "--json"], check=True, capture_output=True, text=True
    )
    authority = json.loads(observed.stdout)["authorityId"]
    if authority != expected:
        raise ValueError("authority-mismatch: worker selected a different authority")
    # Public output may redact portable generic keys. Verify each exact key via
    # the run's private contextual handle rather than comparing redacted JSON.
    for key in resources:
        subprocess.run(
            ["worklease", "verify", "--session", session, "--resource", key],
            check=True, capture_output=True, text=True,
        )
    summary = {"authorityId": authority, "resources": resources, "sessionId": session}
    result = os.environ.get("WORKLEASE_RUN_RESULT")
    if result:
        with open(result, "w", encoding="utf-8") as handle:
            json.dump({"outcome": "done", "summary": "verified queue handoff"}, handle)
    print(json.dumps(summary))


if __name__ == "__main__":
    try:
        main()
    except (ValueError, KeyError, json.JSONDecodeError, subprocess.CalledProcessError) as error:
        print(f"queue worker refused: {error}", file=sys.stderr)
        sys.exit(1)
