// Package instructions contains the canonical, concise guidance printed by the CLI.
package instructions

import "fmt"

var topics = map[string][]string{
	"setup": {
		"Set up Worklease for the user's topology. These instructions only guide; they change nothing.",
		"1. Read existing configuration and project instructions first. Ask only for missing decisions: the topology (local workers, joining an existing remote authority, or hosting a new one), the work source, the exact resource convention, and project or user scope.",
		"2. Local workers on one host share one owner-private state home and use distinct sessions. Separate installs or users do not share an authority. Pass --local when bindings or defaults select remote. Never fall back to local when remote setup fails.",
		"3. To join an existing authority, run worklease instructions remote. To host a new one, run worklease instructions server. Remote authority is experimental.",
		"4. Use the CLI by default. MCP and native edit guards are optional: preview worklease setup mcp or worklease setup guard, then apply only the integration and scope the user asked for. Docs: https://github.com/brettinternet/worklease/blob/main/docs/setup.md (match the installed version).",
		"5. Verify that every worker selects the same authority and resource convention. Run worklease doctor with that selection, adding --resource KEY for remote. On an agreed disposable key, acquire with one session, confirm a second session conflicts, then release. Do not touch existing claims, and do not report success if a check fails.",
		"6. Run worklease setup instructions, fill in the non-secret authority selection, work source, and resource convention, and merge the block into the project's agent instructions without replacing other content. Never store invites, credentials, or private handles there.",
		"7. Read worklease instructions loop and worklease instructions safety before starting work. Report the verified selection, resource convention, checks, and any blocker. Installing the binary alone is not verified coordination.",
	},
	"remote": {
		"Join an existing remote claim authority (experimental). This does not authorize creating or administering a server.",
		"1. Get an invite from the administrator over a private channel and confirm which authority it is for. Keep invite files and credentials out of checkouts and logs. Never put secrets in chat or command arguments.",
		"2. Run worklease profile list and worklease profile default before enrollment. If no user default exists, enrollment makes the new profile the user-wide default, so get approval for that scope change before proceeding. Enroll with worklease enroll --invite-file PATH, or bare worklease enroll for a hidden prompt. Enrolling from a file creates or reuses a private profile and preserves an existing default. Check the result with worklease profile show NAME.",
		"3. Pass --profile NAME explicitly while verifying. Run worklease profile bind NAME only when asked to bind this checkout. In each worker's environment, run worklease profile show without a name to see the selected profile. Do not change the user-wide default without approval. Selection order: --profile, WORKLEASE_PROFILE, checkout binding, user default, then local. --local is an explicit override, never a fallback when remote fails.",
		"4. Agree on resource keys that are byte-identical across hosts and admitted by the authority. Remote rejects host-local path, backlog-md, and markdown keys. Do not invent a translation, and do not imply remote claims fence provider writes.",
		"5. Run worklease doctor --profile NAME --resource KEY, then the two-session contention check from worklease instructions setup with the same profile, on the real worker hosts where possible. On an identity, credential, transport, or admission failure, stop and ask the administrator for the missing action; do not switch authorities.",
		"6. After enrolling, delete the used one-time invite in the way the user approves. Record only the non-secret selection and resource convention in project instructions.",
		"Docs for enrollment, profiles, and troubleshooting: https://github.com/brettinternet/worklease/blob/main/docs/remote-claim-authority.md (match the installed version).",
	},
	"server": {
		"Host a remote claim authority with worklease serve (experimental). This is not the worklease mcp stdio server. Hosting requires explicit authorization.",
		"1. Confirm the host, endpoint, admitted resource prefixes, private persistent storage, TLS, and administrator. Inspect any existing deployment and state before initializing; never overwrite or reset it.",
		"2. For a new loopback authority, worklease server init creates local TLS and a bootstrap invite, and worklease serve runs it. Before exposing a non-loopback listener, get authorization and pass --endpoint, --listen, and --confirm-non-loopback to server init. Never open firewall ports, weaken TLS, or choose cleartext on your own.",
		"3. Check enrollment scope as in worklease instructions remote and pick an unused profile NAME. Enroll the administrator privately with worklease enroll --profile NAME --invite-file PATH_PRINTED_BY_INIT. Issue each worker its own invite with worklease invite issue --profile NAME (write role by default). Never give workers the bootstrap admin invite. Keep invites, credentials, and server state out of repositories and logs.",
		"4. Run one serve process and one SQLite writer per namespace. Agree on service supervision and persistent storage before calling the deployment done. There is no HA, multi-namespace serving, or provider fencing.",
		"5. On each worker host, follow worklease instructions remote and pass the doctor and contention checks from worklease instructions setup. Report the endpoint and non-secret selection, never bootstrap secrets.",
		"6. Before restart, backup, restore, revocation, or recovery, read the operator and recovery sections of https://github.com/brettinternet/worklease/blob/main/docs/remote-claim-authority.md. Containers: https://github.com/brettinternet/worklease/blob/main/docs/container.md. Match docs to the installed version. Never reset state to resolve uncertainty.",
	},
	"loop": {
		"Every contender uses the same authority and the same exact canonical resource.",
		"1. Resolve the authoritative task or path resource and check that its dependencies are ready. A task claim does not cover arbitrary paths.",
		"2. Claim as soon as you choose a candidate, before reading its full intent, planning, delegating, or editing. Discover and select work through the caller-selected task provider, then use worklease acquire or MCP acquire on its exact claim resources. For every source, on conflict do not wait: pick the next ready candidate. Hold at most one claim while selecting. If acquisition is uncertain, stop and recover the pending handle or MCP lease reference (step 9).",
		"3. Give each independent loop one stable, unique session ID. Use a harness-provided loop-run ID unchanged; otherwise generate a full UUID and persist it before work. CLI: set WORKLEASE_SESSION_ID. MCP: pass it as sessionId to acquire, persist the returned lease before work, and use that lease for all later calls. Never truncate the ID or derive it from agent names, labels, or other non-unique data.",
		"4. Use --handle PATH only when you need an explicit private CLI handle. Heartbeat before half the TTL and during long work. MCP automatic heartbeat lasts only while the server process runs and can stop on disconnect, shutdown, or loop replacement; do not rely on it across turns, waits, or iterations.",
		"5. Before blocking work, verify ownership and make sure the work finishes well before expiry; otherwise heartbeat first, or shorten or split the work. Long mutating work needs a guarded operation or another way to keep ownership.",
		"6. After any subagent run, wait, long command, human pause, resumed session, or new loop iteration, verify the exact claim and refresh provider state before any file edit, mutating command, provider write, commit, merge, or cleanup.",
		"7. On stale-claim, claim-expired, or ownership-lost, stop and change nothing else. Expiry does not prove the previous worker stopped and does not authorize resuming its work; require an explicit handoff or authoritative evidence of abandonment. Current owners continue under their verified claim. Use the task provider to manage readiness and authorized status transitions.",
		"8. Save and verify progress in the provider, checkpoint local recovery data when useful, then release.",
		"9. To recover an uncertain request, retry that exact request. For MCP acquire, retry with the lease reference only if the error returned one; after a definitive failure with no reference, start a fresh acquire.",
		"10. Never log or hand off bearer tokens or private handle contents.",
		"In a supervised worklease run (WORKLEASE_RUN_ID is set), the run holds, renews, and releases the claim for you: do not acquire, heartbeat, or release. Use the inherited WORKLEASE_SESSION_ID and WORKLEASE_HANDLE for verify and checkpoint, and write {\"outcome\":\"done|blocked|review|failed\",\"summary\":\"...\"} to WORKLEASE_RUN_RESULT before exiting.",
	},
	"safety": {
		"The provider is the source of truth for eligibility, progress, completion, and retries.",
		"Worklease coordinates only callers that use the same authority and exact resource.",
		"Only guarded local operations are fenced. Native check-then-edit and provider writes rely on cooperation, and coordination-only claims are not fenced by the provider.",
		"The CLI keeps its contextual handle in the state home. Use --handle PATH only for concurrent or automated leases in one context.",
		"Keep private handles and bearer tokens out of repositories, logs, checkpoints, and handoffs.",
		"Retry an uncertain request exactly; do not start a new operation or grant. An MCP acquire that fails definitively returns no lease reference, so start a fresh acquire.",
		"If an operation's outcome is unknown, inspect the provider before retrying, then reconcile explicitly.",
	},
}

// Topics returns the supported instruction topics in stable order.
func Topics() []string { return []string{"setup", "remote", "server", "loop", "safety"} }

// For returns the canonical instructions for topic.
func For(topic string) ([]string, error) {
	values, ok := topics[topic]
	if !ok {
		return nil, fmt.Errorf("unknown instruction topic %q", topic)
	}
	return append([]string(nil), values...), nil
}
