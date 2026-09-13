---
id: doc-3
title: Go Rewrite Capability Inventory
type: specification
created_date: '2026-09-12 06:33'
updated_date: '2026-09-13 00:20'
tags:
  - go-rewrite
  - inventory
---
# Go Rewrite Capability Inventory

## Evidence baseline

This inventory was derived from repository commit `6a92441` before Go implementation began. After the Python proof of concept is retired, every cited path and named test remains retrievable with `git show 6a92441:<path>`.

The normative source is `docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md` at that commit. Python code and tests are evidence of useful behavior and past failure modes, not parity requirements. The inventory groups capabilities rather than enumerating every Python module or test.

Discovery at the recorded commit found:

- 26 `add_parser(...)` registrations in `src/worklease/cli.py`.
- Seven public MCP tools in `WorkleaseMCPServer.tools()` and `_tool_functions()` in `src/worklease/mcp_server.py`: `key`, `acquire`, `status`, `list`, `heartbeat`, `checkpoint`, and `release`.
- Core behavior spread across `src/worklease/*.py`, policies in `src/worklease/adapters/`, schemas in `src/worklease/schemas/v1/`, release tooling in `scripts/*.py`, the source SDK in `packages/worklease-source-sdk/`, human references in `README.md` and `docs/*.md`, and the reusable coordination contract in `skills/worklease-workflow/`.

## Discovered Python surfaces

### CLI

The 26 parser registrations are:

1. `instructions`
2. `key`
3. `policy`
4. `policy list`
5. `policy describe`
6. `acquire`
7. `acquire-bundle`
8. `status-bundle`
9. `status`
10. `history`
11. `events`
12. `inspect-operation`
13. `inspect-operation-bundle`
14. `gc`
15. `reconcile-operation`
16. `reconcile-operation-bundle`
17. `checkpoint`
18. `transfer`
19. `list`
20. `heartbeat`
21. `release`
22. `exec`
23. `heartbeat-bundle`
24. `release-bundle`
25. `exec-bundle`
26. `replace-file`

The source also defines legacy bundle aliases such as `bundle-acquire`, `bundle-status`, `inspect-bundle`, `bundle-heartbeat`, `bundle-release`, and `bundle-exec`. They are aliases, not additional parser registrations. The Go contract replaces this split with one claim model over 1 to 32 resources and a nested command tree in contract section 4.

### MCP

Python exposes seven tools: `key`, `acquire`, `status`, `list`, `heartbeat`, `checkpoint`, and `release`. The Go contract deliberately expands this to the eleven tools in section 12 by adding `verify`, `watch`, `events`, and `instructions`. CLI-only operations remain documented rather than being added for parity.

## Capability ownership and safety evidence

Each row names exactly one primary Go task. A later consumer may wire or polish the capability, but ownership remains with the listed task.

| Capability family | Go decision | Primary owner | Contract sections | Representative evidence at `6a92441` |
| --- | --- | --- | --- | --- |
| Binary, configuration, errors, envelopes, and command skeleton | Redesign as the typed Go entry point with flag/env/YAML/default precedence, stable reason families, injected writers, and schema version 2 output. | TASK-85.2 | 1.1, 2 D1/D10/D11, 3-6, 14, 18 | `src/worklease/__main__.py`, `cli.py`, `models.py`; `tests/test_cli.py::CliContractTests::test_short_flags_have_one_meaning_across_parser_tree`, `test_option_abbreviations_are_rejected`, `test_json_and_format_conflicts_are_order_independent`, `test_unrecognized_option_keeps_the_json_error_envelope`, `test_non_utf8_arguments_fail_as_invalid_arguments` |
| Hermetic Go test foundation | Replace Python fixtures with injected clocks/IDs, isolated homes, bounded concurrency helpers, and subprocess helpers. Tests express contract behavior rather than one-to-one ports. | TASK-85.3 | 2 D14/D15, 3, 14, 21 | `tests/test_store.py::StoreTests::test_concurrent_acquire_has_one_winner_and_independent_resources_proceed`, `test_forward_clock_step_past_expiry_still_expires`; `tests/test_execution.py::ExecutionTests::test_ownership_loss_terminates_running_process_group` |
| SQLite driver proof | Select and prove the native driver, including WAL-visible read-only access, cancellation, serialization, and filesystem limits. | TASK-85.4 | 2 D2/D3/D8, 8, 14, 15 | `src/worklease/sqlite.py`, `locking.py`; `tests/test_store.py::StoreTests::test_state_home_and_files_are_private_with_a_permissive_umask`, `test_state_database_symlink_is_rejected`, `test_epoch_schema_migration_rolls_back_atomically` |
| Resource identities and policies | Retain deterministic keys, redesign as static built-ins `backlog-md`, `markdown`, `github`, `linear`, `generic`, and `path`; remove entry-point plugins. | TASK-85.5 | 7.1, 7.11, 7.13, 16, 20 | `src/worklease/adapters/`; `tests/test_adapters.py::AdapterKeyTests::test_backlog_and_markdown_use_repository_local_identity`, `test_nested_source_keys_match_across_linked_worktrees`, `test_generic_policy_is_explicit_and_unknown_names_fail`, `test_bundled_adapters_reject_provider_fencing` |
| Secure authority store and schema | Redesign the Python store into one owner-private SQLite authority with claims, epochs, operations, reconciliations, and an append-only event sequence. No Python schema import. | TASK-85.6 | 2 D8/D9/D14, 7.6, 8, 20 | `src/worklease/sqlite.py`, `store.py`, `locking.py`; `tests/test_store.py::StoreTests::test_state_database_symlink_is_rejected`, `test_concurrent_acquire_has_one_winner_and_independent_resources_proceed`, `test_small_backward_clock_step_keeps_a_live_lease`, `test_forward_clock_step_past_expiry_still_expires` |
| Singleton lifecycle, checkpoints, transfer, contention, and exact replay | Retain acquire/status/list/heartbeat/checkpoint/release/transfer, redesign around client-held credentials, revisions, bounded request windows, authenticated exact replay, and atomic successor grants. | TASK-85.7 | 4, 7.2-7.9, 9, 18, 21 | `src/worklease/acquisition.py`, `claims.py`, `lifecycle.py`, `operations.py`; `tests/test_store.py::StoreTests::test_checkpoint_renews_replays_and_rejects_stale_owner`, `test_transfer_replaces_owner_atomically_and_preserves_checkpoint`, `test_transfer_rolls_back_on_interruption_without_free_interval`, `test_heartbeat_retry_is_idempotent_and_rejects_request_mismatch` |
| Atomic many-resource ownership | Replace separate singleton/bundle paths with one ordered, unique, all-or-none claim over 1 to 32 resources. | TASK-85.8 | 2 D4, 7.5, 7.10, 8 | `src/worklease/acquisition.py`, `claims.py`; `tests/test_store.py::StoreTests::test_bundle_acquire_is_atomic_and_single_member_mutations_are_rejected`, `test_overlapping_bundles_across_processes_have_one_winner`, `test_bundle_acquire_rolls_back_after_partial_member_failure` |
| Operation ledger, inspection, reconciliation, events, and history | Retain diagnostics and recovery while redesigning around request hashes, request deadlines, started/completed/reconciled states, predecessor recovery, one event sequence, and redacted projections. | TASK-85.9 | 7.4, 7.5, 7.12, 8, 11, 21 | `src/worklease/operations.py`, `reconciliation.py`, `projections.py`; `tests/test_store.py::StoreTests::test_reconcile_operation_is_authorized_idempotent_and_append_only`, `test_inspect_operation_rejects_reused_operation_id_as_ambiguous`; `tests/test_history.py::HistoryProjectionTests::test_reconciliation_is_attributed_to_resolver_and_never_exports_evidence`, `test_events_cursor_preserves_ties_and_concurrent_insert_semantics` |
| Contextual/MCP handles and credential sources | Retain private convenience handles, redesign them as session-scoped and authority-bound with pending exact requests, safe locking, atomic persistence, and file/fd credential sources. Remove argv tokens and `ownerId`. | TASK-85.10 | 2 D5-D7, 4, 6.2-6.3, 9, 21 | `src/worklease/credentials.py`, `lease_context.py`, `lease_file.py`, `execution_context.py`; `tests/test_contextual_cli.py::ContextualCliTests::test_contextual_acquire_is_serialized_across_processes`; `tests/test_credentials.py::CredentialResolverTests::test_sources_are_mutually_exclusive`, `test_file_source_requires_owner_only_regular_file`, `test_file_source_rejects_symlinks_and_never_leaks_path`; `tests/test_lease_context.py::LeaseContextTests::test_linked_worktree_has_a_distinct_context` |
| Read views and garbage collection | Retain status/list/history/events and GC, redesign retention as contiguous prefixes that protect active claims, unresolved effects, authentication, replay windows, and fresh retirement evidence. | TASK-85.11 | 7.12, 11, 13 | `src/worklease/garbage_collection.py`, `projections.py`; `tests/test_gc.py::GarbageCollectionTests::test_apply_is_atomic_and_preserves_resource_revision`, `test_old_expired_claim_with_unresolved_operation_is_explained`, `test_gc_serializes_expired_retirement_acquire_and_heartbeat`, `test_history_coverage_tracks_partial_and_complete_epoch_gc`; `tests/test_history.py::HistoryProjectionTests::test_projection_never_reads_secret_blob_columns_or_unrelated_rows` |
| Guarded exec, atomic replacement, and ownership verification | Retain local guards but narrow claims honestly. Exec is supervised coordination with process-group termination and unknown outcomes; replace-file alone reports local serialized replacement; verify supports claim or path coverage. | TASK-85.12 | 6.2-6.3, 7.4-7.6, 7.11, 10, 21 | `src/worklease/execution.py`, `replacement.py`; `tests/test_execution.py::ExecutionTests::test_exec_uses_argv_captures_output_and_replays_without_side_effect`, `test_ownership_loss_kills_descendant_after_leader_exit`, `test_exec_timeout_kills_inherited_pipe_descendant_and_is_inspectable`, `test_started_intent_is_unknown_outcome_and_never_reruns`, `test_replacement_is_atomic_preserves_mode_and_replays`, `test_replacement_keeps_ownership_during_atomic_write`; `tests/test_cli.py::CliContractTests::test_replace_file_rejects_coordination_only_claims` |
| Cursor-based watches | Add the contract-defined event and resource watch. No current top-level Python `watch` command or dedicated watch test was found; expiry without events and last-scanned cursors require new executable evidence. | TASK-85.13 | 7.6, 11, 20, 21 | Related evidence only: `tests/test_history.py::HistoryProjectionTests::test_events_cursor_preserves_ties_and_concurrent_insert_semantics`, `test_events_rejects_invalid_cursor_without_opening_database`; `tests/test_store.py::StoreTests::test_forward_clock_step_past_expiry_still_expires` |
| Complete CLI, diagnostics, and canonical instructions | Retain useful instructions and CLI ergonomics; add read-only doctor; make common human and JSON journeys setup-free, structured, and actionable. | TASK-85.14 | 1.1, 4-6, 13, 18 | `src/worklease/cli.py`, `instructions.py`; `tests/test_cli.py::CliContractTests::test_wait_retries_transient_contention_until_acquire`, `test_acquire_defaults_generate_and_echo_identifiers`; `tests/test_skills.py::SkillBundleTests::test_worklease_is_one_self_contained_skill` |
| Stdio MCP orchestration | Retain stdio serving but redesign from seven tools to the eleven in section 12, typed schemas, modern and legacy lifecycles, bounded concurrency/cancellation, private lease references, and hold-bounded automatic heartbeat. | TASK-85.15 | 1.1, 6, 9, 12, 21 | `src/worklease/mcp_server.py`; `tests/test_mcp.py::MCPTests::test_stdio_tool_surface_schemas_instructions_and_protocol_errors`, `test_token_redaction_checkpoint_rejection_and_failure_paths`, `test_concurrent_mutation_returns_stable_busy_error`, `test_automatic_heartbeat_runs_before_half_ttl`, `test_max_hold_stops_without_release_then_claim_expires`, `test_stdio_eof_stops_renewal_without_releasing`, `test_restart_does_not_renew_existing_handle_and_list_cleans_it` |
| Agent setup and optional mutation guards | Add managed MCP and native edit-hook setup while preserving unrelated client config. Default claim coverage is explicit about its limits; path coverage is opt-in; shell/provider writes are not allowlisted. | TASK-85.16 | 1.1, 10.3, 13, 20-21 | `skills/worklease-workflow/` documents the current cooperative workflow; related tests are `tests/test_skills.py::SkillBundleTests::test_skill_links_exist_and_stay_inside_skill_root` and `tests/test_adapters.py::AdapterKeyTests::test_bundled_adapters_reject_provider_fencing`. New setup/hook behavior needs Go tests. |
| Documentation, skill, build, installation, and release | Rewrite the human docs and reusable skill for Go; replace Python/uv/PyInstaller release machinery with four native archives, checksums, man page, smoke tests, and explicit publication authority. | TASK-85.17 | 1.1, 2 D1-D3/D12, 3, 13-14, 19 | `README.md`, `docs/cli-reference.md`, `docs/claim-model.md`, `docs/mcp.md`, `skills/worklease-workflow/`, `scripts/*.py`; `tests/test_release.py::ReleaseValidationTests::test_checksum_verification_rejects_changed_artifact`, `test_downloaded_native_artifact_is_installed_and_smoke_tested`, `test_release_documentation_is_concise_current_and_executable`; `tests/test_skills.py::SkillBundleTests::test_skill_frontmatter_is_portable` |
| Incompatible cutover and retirement | Remove the Python core, schemas, SDK, plugins, packaging, benchmark, and compatibility document only after every retained inventory row is delivered or explicitly rejected. Preserve Python-era state for optional recoverable disposal. | TASK-85.18 | 1, 2 D10/D12/D16, 14, 16, 19 | `src/worklease/`, `src/worklease/schemas/v1/`, `packages/worklease-source-sdk/`, `benchmarks/mcp_lifecycle.py`, `pyproject.toml`, `uv.lock`; `tests/test_package_smoke.py::PackageSmokeTests::test_public_facade_exports_supported_interfaces`, `test_documented_adapter_facade_exports_extension_interfaces`; `tests/test_policy_plugins.py::ExternalPolicyPackagingTests::test_wheel_install_discovers_external_policy` identify surfaces to remove, not parity obligations. |

## Intentional removals

Contract section 16 intentionally removes without compatibility obligations:

- the Python public API and `worklease.__all__`;
- the `worklease-mcp` entry point;
- `worklease_source_sdk`, its example plugin, and dynamic resource-policy entry points;
- PyInstaller, wheel/`uv` runtime packaging, schema version 1, and Python SQLite schemas/state import;
- Python lease-file formats and `context-leases`/`mcp-leases` directories;
- `ownerId` and argv `--token`;
- bundle-specific commands and aliases;
- `--provider-directory` in favor of `--cwd`, `--format` in favor of `--json`, and the MCP lifecycle benchmark;
- `docs/source-provider-sdk-compatibility.md`.

Python-era state on disk is not deleted automatically. TASK-85.18 documents optional recoverable disposal.

## Changed semantics

The rewrite is intentionally incompatible. In particular:

- one claim model replaces separate singleton and bundle models;
- session-scoped, authority-bound handles replace checkout-global Python lease handles;
- client-side pending requests and bounded authenticated replay replace unrecoverable or token-returning paths;
- one event sequence replaces Python cursor and projection details;
- `guarantee: local-coordination` replaces claim-wide fencing language;
- only expected-hash local replacement may report `mutationProtection: local-serialized-replace`;
- predecessor started operations require explicit outcome and cessation evidence before guarded work resumes;
- Go JSON uses schema version 2 and only `--json`; text, schemas, command names, aliases, state, APIs, and exact output do not require Python parity;
- the Go MCP surface has eleven purposefully selected tools, not CLI parity.

Representative Python tests preserve failure evidence, not required names, fixtures, module boundaries, output bytes, schema shapes, or one-to-one ports.

## Deferred remote authority

`docs/distributed-cloudflare-claim-authority.md` is preserved as deferred design evidence. Contract section 20 keeps typed domain behavior independent of local CLI/MCP encoding and SQLite callbacks, but v1 adds no HTTP client, backend registry, Worker, authority credentials, deployment tooling, remote exec, or fencing counter. Configured remote failure must never silently fall back to local coordination in a future design.

## Safety-gap and amendment assessment

This pre-implementation inventory does not assert that read-only SQLite opens are side-effect-free. TASK-88 later amended contract sections 8 and 13: read-only commands do not create the home or main database, but SQLite/modernc may recreate absent owner-private `-wal`/`-shm` sidecars for an existing WAL database in a writable directory.

No material gap was found in the normative Go Product Contract at commit `6a92441`. Its section 21 assigns each identified concurrency, recovery, handle, cursor, GC, MCP, and guard counterexample to an owning task. The missing behavior observed in Python, including durable pending requests, eleven MCP tools, expiry-aware watches, static policies, one claim model, native verification, and predecessor reconciliation, is planned implementation work under TASK-85.2 through TASK-85.18 rather than a contract omission.

No section 15 amendment is proposed. Existing TASK-86 amendments already resolve the reviewed safety issues and are reflected in the current contract and task descriptions.
