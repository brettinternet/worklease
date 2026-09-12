// Package testkit provides small deterministic helpers shared by Worklease's
// Go acceptance tests.
//
// Git fixtures are intentionally added only by tests that consume them. Those
// fixtures must document and assert their context contract: a main checkout
// resolves to its own root, a linked worktree remains distinct where handles
// are checkout-scoped, and a symlinked nested path resolves consistently with
// its real path. Fixture creation must use bounded commands and cleanup.
package testkit
