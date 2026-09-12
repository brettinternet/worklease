"""Canonical agent instructions shared by all Worklease interfaces."""

from __future__ import annotations

_INSTRUCTIONS = {
    "loop": (
        "Use one shared Worklease authority and the same exact canonical resource for every contender.",
        "1. Resolve the authoritative item and verify its dependencies are ready.",
        "2. Acquire before delegation or edits; the CLI uses a private contextual handle by default. On conflict, wait or select other ready work.",
        "3. Use -L PATH only for concurrent or automated leases in one context. Heartbeat before half the TTL and around long work.",
        "4. Revalidate claim ownership and authoritative provider state before each durable write.",
        "5. Persist and verify provider-visible progress; checkpoint local recovery metadata when useful; then release.",
        "6. Stop immediately on stale-claim. A resumed worker acquires a fresh claim and never adopts another claim.",
        "7. Never log or hand off bearer tokens or lease-file contents.",
    ),
    "safety": (
        "The backing provider remains authoritative for eligibility, progress, completion, and retries.",
        "Worklease coordinates only callers using the same authority and exact resource.",
        "Only guarded local operations are fenced; coordination-only claims and external provider writes are not provider-fenced.",
        "The CLI keeps its contextual handle under state home; use -L PATH only for concurrent or automated leases in one context.",
        "Keep lease files and bearer tokens private and out of repositories, logs, checkpoints, and handoffs.",
        "On stale-claim or expiry, stop mutating and acquire a fresh claim; never adopt the old ownership epoch.",
        "For an unknown operation outcome, inspect the provider before retrying, then reconcile explicitly.",
    ),
}


def agent_instructions(topic: str) -> tuple[str, ...]:
    """Return the canonical concise instructions for one supported topic."""

    try:
        return _INSTRUCTIONS[topic]
    except KeyError as error:
        raise ValueError(topic) from error


__all__ = ["agent_instructions"]
