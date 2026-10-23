---
id: TASK-87
title: Audit documentation after the Go cutover
status: done
type: docs
priority: high
assignees: ['@brett']
rank: ac20563b48c205590
refs: [README.md, docs/cli-reference.md, docs/claim-model.md, docs/mcp.md, docs/setup.md, skills/worklease-workflow/SKILL.md]
created: 2026-09-12T22:07:00Z
updated: 2026-09-12T22:26:00Z
---

The Go rewrite is complete, but current user and agent documentation may still describe retired Python behavior or use longer explanatory prose where a command example would be clearer. Audit maintained documentation against the shipped Go CLI, MCP server, setup output, and workflow contract while preserving historical backlog records and the deferred remote-authority proposal.

## Acceptance criteria

- [x] All maintained README, docs, and workflow-skill pages match the shipped Go command, configuration, credential, lease, and MCP behavior
- [x] Retired Python-era instructions and stale links are removed from maintained documentation; historical backlog evidence and explicitly deferred proposals remain intact
- [x] Documentation is concise, plain, direct, and uses executable examples wherever they communicate behavior better than prose
- [x] Documentation checks and repository quality checks pass

## Plan

1. Inventory maintained documentation and compare it with the Go command, MCP, setup, and workflow surfaces.
2. Rewrite stale or indirect sections with short, executable examples; update generated Backlog documentation only through the Backlog CLI.
3. Run executable documentation checks and full repository quality checks.
4. Independently review the final documentation for stale Go-cutover claims and clarity, fix findings, finalize the task, and commit.

## Notes

Audited maintained docs against the Go CLI, MCP, setup, policy, and doctor surfaces. Removed migration-only comparisons and duplicated MCP inventory, replaced retired Python resource-policy plugin guidance with the shipped static Go policy set, tightened workflow wording, updated doc-1 through the Backlog CLI, and corrected the stale doctor MCP diagnostic with a regression assertion. Focused doctor/CLI tests and mise run doc-test pass.

Final verification: maintained local Markdown links validated across 23 files; mise run lint, format-check, test, typecheck, doc-test, hooks-install, and hooks passed. Independent review found five factual issues in path coverage, MCP maxHold, resource-policy selection, guarantee composition, and version output; all were corrected, and follow-up review passed.

## Summary

Updated the README, CLI/claim/MCP guides, workflow guide, and skill references for the shipped Go-only product. Removed retired Python plugin and migration guidance, replaced duplicated explanation with linked examples, fixed five additional factual errors found in independent review, and corrected the stale doctor MCP diagnostic with a regression assertion. Verified executable docs, 23 files of local links, all repository quality checks, and pre-commit hooks.
