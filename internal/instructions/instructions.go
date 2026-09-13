// Package instructions contains the canonical, concise guidance printed by the CLI.
package instructions

import "fmt"

var topics = map[string][]string{
	"loop": {
		"Use one shared Worklease authority and the same exact canonical resource for every contender.",
		"1. Resolve the authoritative task or path resource and verify its dependencies are ready; a task resource does not authorize arbitrary paths.",
		"2. Acquire before delegation or edits; the CLI uses a private contextual handle by default. On conflict, wait or select other ready work.",
		"3. Set a distinct WORKLEASE_SESSION_ID for each independent loop, or use --handle PATH for concurrent or automated leases in one context. Heartbeat before half the TTL and around long work.",
		"4. Revalidate claim ownership and authoritative provider state before each durable write.",
		"5. Persist and verify provider-visible progress; checkpoint local recovery metadata when useful; then release.",
		"6. Recover an uncertain pending request by retrying that exact request. For MCP acquire, retry by lease reference only when the error returns one; after a definitive failure with no reference, start a fresh acquire.",
		"7. Stop immediately on stale-claim. A resumed worker acquires a fresh claim and never adopts another claim.",
		"8. Never log or hand off bearer tokens or private handle contents.",
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
func Topics() []string { return []string{"loop", "safety"} }

// For returns the canonical instructions for topic.
func For(topic string) ([]string, error) {
	values, ok := topics[topic]
	if !ok {
		return nil, fmt.Errorf("unknown instruction topic %q", topic)
	}
	return append([]string(nil), values...), nil
}
