// Package instructions contains the canonical, concise guidance printed by the CLI.
package instructions

import "fmt"

var topics = map[string][]string{
	"setup": {
		"Set up Worklease for the user's intended topology; instructions print guidance and do not configure anything.",
		"1. Inspect existing configuration and project instructions first. Ask only for missing decisions: local workers, joining an existing remote authority, or hosting a new authority; work source and exact resource convention; project versus user scope.",
		"2. For local workers, use the same owner-private local authority/state home on one host, with distinct sessions. Separate installs or users do not automatically share an authority. Use --local explicitly when bindings or defaults select remote; never use local as a fallback for a failed remote setup.",
		"3. To join an existing authority, read worklease instructions remote. To host a new claim authority, read worklease instructions server. Remote authority is experimental.",
		"4. Use the CLI by default. MCP and native edit guards are optional: preview worklease setup mcp or worklease setup guard and apply only the requested integration/scope. See https://github.com/brettinternet/worklease/blob/main/docs/setup.md; use documentation matching worklease version.",
		"5. Verify every intended worker selects the same authority and exact resource convention. Run worklease doctor with that selection (and --resource KEY for remote admission). On an agreed disposable key, acquire with one unique session, confirm a second independent session conflicts, then release the test claim. Do not disturb existing claims or declare completion on a failed check.",
		"6. Print worklease setup instructions, fill in the non-secret authority selection, work source, and exact resource convention, and merge the block into project agent instructions without replacing unrelated content. Do not store invitations, credentials, or private handles there.",
		"7. Read worklease instructions loop and worklease instructions safety before work. Report the verified selection, resource convention, checks, and any remaining blocker; installing the binary alone is not verified coordination.",
	},
	"remote": {
		"Join an existing experimental remote claim authority; this is not permission to create or administer a server.",
		"1. Obtain an administrator-issued invite through a private channel and confirm the intended authority with the administrator. Keep invite files and credentials outside checkouts and logs; never paste bearer secrets into chat or argv.",
		"2. Inspect worklease profile list and worklease profile default before enrollment. If no user default exists, enrollment makes the enrolled profile the user-wide default; obtain approval for that scope change before proceeding. Enroll with worklease enroll --invite-file PATH (or the hidden prompt with bare worklease enroll). Artifact enrollment creates or reuses a private profile but preserves an existing default; inspect worklease profile show NAME afterward.",
		"3. Prefer explicit --profile NAME while verifying; use worklease profile bind NAME for this checkout when requested. Run worklease profile show without a name in each worker's environment to check effective selection. Do not change a user-wide default without authorization. Selection is --profile, WORKLEASE_PROFILE, checkout binding, user default, then local; --local is an explicit override, never a remote-failure fallback.",
		"4. Agree on identical cross-host resource bytes admitted by the authority. Host-local path, backlog-md, and markdown keys are rejected remotely; do not invent a translation or imply remote claims fence provider writes.",
		"5. Run worklease doctor --profile NAME --resource KEY, then the two-session contention check in worklease instructions setup with the same profile. Where possible, run the contenders on the intended worker hosts. Stop on identity, credential, transport, or admission failures; request the missing administrator action instead of switching authorities.",
		"6. After successful enrollment, remove the consumed one-time invite using the user's approved cleanup method. Keep credentials private and record only non-secret selection and resource conventions in project instructions.",
		"For enrollment, profiles, and troubleshooting, see https://github.com/brettinternet/worklease/blob/main/docs/remote-claim-authority.md (use the installed version's documentation).",
	},
	"server": {
		"Operate an experimental remote claim authority (worklease serve), not the optional worklease mcp stdio server. Hosting requires explicit authorization.",
		"1. Confirm deployment host, endpoint, admitted resource prefixes, private persistent storage, TLS, and who administers the authority. Inspect existing deployment/state before initializing anything; do not overwrite or reset it.",
		"2. For a new loopback authority, worklease server init prepares local TLS and a bootstrap invite; worklease serve runs it. Before exposing a non-loopback listener, obtain authorization and set an explicit reachable --endpoint, --listen, and --confirm-non-loopback on server init. Never open firewall ports, weaken TLS, or choose cleartext implicitly.",
		"3. Check enrollment scope as described in worklease instructions remote, choose a non-conflicting profile NAME for this authority, then enroll the administrator privately with worklease enroll --profile NAME --invite-file PATH_PRINTED_BY_INIT. Issue separate worker invites with worklease invite issue --profile NAME (write role by default); do not distribute the bootstrap admin invite to workers. Keep invites, installation credentials, and server state outside repositories and logs.",
		"4. Use one serve process and one SQLite writer per namespace. Agree on service supervision and persistent storage before calling a deployment complete; no HA, multi-namespace serving, or provider fencing is provided.",
		"5. Follow worklease instructions remote on intended worker hosts and pass the doctor and contention checks from worklease instructions setup. Report the endpoint and non-secret authority selection, never bootstrap secrets.",
		"6. Before restart, backup/restore, revocation, or recovery, read the operator and recovery sections at https://github.com/brettinternet/worklease/blob/main/docs/remote-claim-authority.md. Container deployment: https://github.com/brettinternet/worklease/blob/main/docs/container.md. Use documentation matching the installed version; do not reset state to resolve uncertainty.",
	},
	"loop": {
		"Use one shared Worklease authority and the same exact canonical resource for every contender.",
		"1. Resolve the authoritative task or path resource and verify its dependencies are ready; a task resource does not authorize arbitrary paths.",
		"2. Acquire before delegation or edits; the CLI uses a private contextual handle by default. On conflict, wait or select other ready work.",
		"3. Set a distinct WORKLEASE_SESSION_ID for each independent loop, or use --handle PATH for concurrent or automated leases in one context. Heartbeat before half the TTL and around long work. MCP automatic heartbeat is process-scoped and may stop on disconnect, server shutdown, or loop replacement; never rely on it across agent turns, waits, or loop iterations.",
		"4. Before blocking work, verify ownership and ensure the operation is bounded to finish before expiry with margin; otherwise heartbeat first and shorten or split it. Long mutating work requires a guarded operation or another mechanism that maintains ownership.",
		"5. After every subagent run, wait, long command, human pause, resumed session, or new loop iteration, verify the exact claim and refresh authoritative provider state before any filesystem edit, mutating command, provider write, commit, merge, or cleanup.",
		"6. On stale-claim, claim-expired, or ownership-lost, stop without further mutation. Claim expiry does not prove the prior worker stopped and does not authorize resuming in-progress work; require explicit handoff or authoritative abandonment evidence.",
		"7. Persist and verify provider-visible progress; checkpoint local recovery metadata when useful; then release.",
		"8. Recover an uncertain pending request by retrying that exact request. For MCP acquire, retry by lease reference only when the error returns one; after a definitive failure with no reference, start a fresh acquire.",
		"9. Never log or hand off bearer tokens or private handle contents.",
	},
	"safety": {
		"The backing provider remains authoritative for eligibility, progress, completion, and retries.",
		"Worklease coordinates only callers using the same authority and exact resource.",
		"Only guarded local operations are fenced; native check-to-edit and provider writes are cooperative, and coordination-only claims are not provider-fenced.",
		"The CLI keeps its contextual handle under state home; use --handle PATH only for concurrent or automated leases in one context.",
		"Keep private handles and bearer tokens out of repositories, logs, checkpoints, and handoffs.",
		"Retry an uncertain pending request exactly; do not create a fresh operation or grant. An MCP acquire that definitively fails returns no lease reference and requires a fresh acquire.",
		"For an unknown operation outcome, inspect the provider before retrying, then reconcile explicitly.",
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
