---
id: TASK-110
title: 'Zero-flag remote authority: predictable defaults with a flag ladder'
status: To Do
assignee: []
created_date: '2026-09-16 04:23'
updated_date: '2026-09-16 04:29'
labels:
  - ergonomics
  - remote-authority
dependencies: []
references:
  - docs/remote-claim-authority.md
  - docs/cli-reference.md
priority: high
type: bug
ordinal: 152000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Trying the remote authority for the first time currently requires discovering the guided mode, six or more flags, a manual owner-private directory, and explicit output paths for every artifact. Worse, the zero-flag path is a trap: `worklease server init` without `--guided` writes a legacy cleartext-HTTP config with no advertised endpoint, and re-running it can report success while reusing an expired bootstrap grant. Generated server and invite paths can also drift from the process actually served.

Design principle for this task (and for the CLI generally): ergonomics first, customizability layered on top. Every command whose intent is unambiguous must work with zero flags using safe, predictable, documented system defaults; the no-flag outcome must be the one a first-time user would want. Flags exist only to (a) deviate from the default, (b) grant consent to a risk (LAN exposure, cleartext), or (c) name a resource, identity, or destructive target that cannot be guessed safely. Capability should form a ladder: bare commands for the local demo → one or two flags for a LAN deployment → config file, env vars, and advanced flags for full control. Remove modes that split the happy path (legacy vs guided) rather than documenting around them. Where a value can be derived from state the CLI already owns (server config, selected profile, default paths), derive it instead of asking. Every command that produces state should print the exact next command.

This task also audits the full command tree so the no-flag contract is deliberate rather than limited to the observed bootstrap failure.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 From clean user state and no environment variables, this exact transcript is the documented local journey and completes without prompts on a terminal or in CI: `worklease server init`, `worklease serve`, `worklease enroll` (interactive hidden prompt, or `--invite-file` pointing at the path init printed), `worklease invite issue`, second client `worklease enroll`, then `acquire`/`list`/`heartbeat`/`release` on `coordination:demo`. Commands create any owner-private directories they need; no `install -d` step is documented.
- [ ] #2 `worklease server init` with no flags produces the secure loopback setup: listen 127.0.0.1:8443, advertised endpoint https://127.0.0.1:8443, self-signed TLS with the fingerprint pinned into the bootstrap artifact, admitted prefix `coordination:`, default config, state, cert, and bootstrap paths under the XDG user directories. The legacy cleartext local config is no longer the default output of any command; `--guided` becomes a no-op alias or is removed, and the previous guided flags remain available as overrides without a mode flag.
- [ ] #3 Re-running `worklease server init` against an initialized authority is idempotent and truthful: it never reports a usable bootstrap artifact when the recorded grant is expired, used, revoked, or belongs to different config/state; if the bootstrap is expired and unredeemed it is reissued in place by the same zero-flag command with an immediately usable artifact; if it was redeemed the output says so and names `server bootstrap-reissue` as the next step. Mismatched config/state paths fail with a message naming both paths.
- [ ] #4 Listener and advertised endpoint defaults are distinct and client-reachable: a wildcard listen address (0.0.0.0, ::) is never emitted as an endpoint, in output, in artifacts, or in prompts. LAN setup is `worklease server init --listen 0.0.0.0:8443 --endpoint https://HOST:8443` plus a single consent (`--confirm-non-loopback` or an interactive yes); cleartext requires the additional explicit acknowledgement as today.
- [ ] #5 `worklease invite issue` works with no flags for the selected admin profile: role `write`, label defaulting to the profile name, documented default expiry, artifact written to a documented owner-private default path (created 0700/0600) that is printed along with the enroll command to run on the client; bearer material is never printed. `--invite-file`, `--invite-fd`, `--role`, `--label`, and `--expires-at` remain as overrides.
- [ ] #6 `worklease serve`, `worklease server bootstrap-reissue`, and `worklease server retire` derive `--home`, config, and default artifact paths from the same server configuration resolution as `server init` (flag, `WORKLEASE_SERVER_CONFIG`, XDG default); only retire still requires explicit intent for the destructive action, and it names the resolved home in its guidance.
- [ ] #7 Every command in the tree is audited in a table (command, zero-flag behavior, why) checked into the task or docs and enforced by a test: read-only and contextual commands use predictable defaults where intent is unambiguous; commands that need a resource, child command, identity, recovery evidence, or destructive target fail fast with one-line guidance naming the missing input and an example, never by guessing. Each state-producing command prints the exact next command.
- [ ] #8 Automated acceptance tests cover: the clean zero-flag transcript in AC1 end to end; repeated `server init` after bootstrap expiry, after redemption, and with mismatched config/state; zero-flag `invite issue`; both enrollments; the two-client coordination demo; and the LAN variant with wildcard listen asserting the endpoint is not the wildcard.
- [ ] #9 Docs (`docs/remote-claim-authority.md`, `docs/cli-reference.md`, README quickstart, `docs/remote-demo.tape`) lead with the zero-flag local transcript, then the LAN variant, then a short 'customize' section listing the override flags, env vars, and config keys in ladder order. Flags appear only where consent or machine-specific addressing requires them.
<!-- AC:END -->
