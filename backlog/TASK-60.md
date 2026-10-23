---
id: TASK-60
title: Make documentation easier to scan
status: done
type: docs
priority: medium
assignees: ['@brett']
rank: a712dcf7ea712dc94
created: 2026-09-07T16:11:00Z
updated: 2026-09-07T16:40:00Z
---

Reduce dense prose across the README, user-facing docs, and reusable workflow documentation. Prefer short sections, copyable examples, tables, and simple diagrams while preserving technical guarantees and command accuracy.

## Acceptance criteria

- [x] Long prose blocks are replaced or split into concise, plain-language sections without losing required behavior or safety constraints
- [x] Concept-heavy sections use useful examples, tables, or diagrams where those formats improve comprehension
- [x] README and documentation links, commands, and technical claims remain accurate
- [x] Repository lint, formatting, tests, and type checks pass

## Plan

Audit README.md, docs/*.md, and skills/worklease-workflow documentation for dense prose. Rewrite the highest-density sections into shorter text, examples, tables, and diagrams while preserving normative meaning. Validate links and repository checks, review the final diff, then commit and push only this task's files.

## Notes

Audited README.md, top-level docs, and workflow skill documentation. Reworked dense timeout, clock, lifecycle, identifier, garbage-collection, text-output, Durable Object, token, provider SDK, bundle, capability, and guarantee sections into shorter prose, tables, lists, and diagrams.

Validation: dense prose blocks of 60+ words across the six edited content files fell from 21 to 8; all local Markdown links resolve; git diff --check passed; mise run lint, format-check, test (253 core and 19 SDK), and typecheck passed. Independent review found three accuracy regressions in CLI and default-scope wording; all three were corrected.

## Summary

Reworked dense README, CLI, claim-model, Cloudflare authority, SDK, and workflow sections into shorter prose, tables, lists, and lifecycle diagrams while preserving command and guarantee boundaries. Verified links, diff hygiene, repository quality checks, and an independent accuracy review.
