---
id: TASK-41
title: Document distributed Cloudflare claim authority
status: done
type: docs
rank: a4a8dec3d14a8de82
files: [docs/distributed-cloudflare-claim-authority.md]
created: 2026-08-23T17:28:00Z
updated: 2026-08-23T17:32:00Z
---

Record the recommended architecture, guarantees, deployment boundary, CLI integration, authentication, storage model, and implementation sequence for a distributed Worklease claim authority hosted on Cloudflare. The report must cite only Cloudflare or other general public documentation and must not identify or reference the repository supplied during the originating discussion.

## Acceptance criteria

- [x] A report under docs/ explains the recommended Worker and Durable Object architecture
- [x] The report distinguishes distributed coordination from provider-mutation fencing
- [x] The report recommends how the hosted service and existing worklease CLI should be separated and integrated
- [x] The report cites only Cloudflare or other general public documentation

## Plan

1. Review existing documentation conventions and current claim contract. 2. Write the architecture report under docs/ without referencing the user-supplied repository. 3. Verify links, required guarantee language, and formatting. 4. Commit the report and finalize this task with evidence.

## Notes

Created docs/distributed-cloudflare-claim-authority.md. Manual review confirmed the Worker/Durable Object design, remote coordination versus provider-fencing boundary, service/CLI separation, protocol examples with required claimId, and proposed schema-version-2 fence field. Every external reference is a Cloudflare documentation URL; the Durable Objects, SQLite storage, D1 Sessions, Access service token, and custom-domain documentation resolved successfully. Validation passed: mise run lint; mise run format-check; mise run test (193 core and 19 SDK tests); mise run typecheck.

## Summary

Documented the recommended distributed Cloudflare claim authority: a separately deployed Worker using namespace-scoped SQLite-backed Durable Objects, consumed through an integrated worklease CLI backend. The report defines guarantees, protocol and data model, authentication, bundle boundaries, verification, and staged implementation. Verified by manual content and public-source review plus all project quality gates.
