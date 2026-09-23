---
id: TASK-126.5
title: 'Probe GitHub sync, dependency, and identity behavior'
status: To Do
assignee: []
created_date: '2026-09-23 04:29'
updated_date: '2026-09-23 04:30'
labels:
  - work-queue
  - github
milestone: m-1
dependencies: []
references:
  - 'https://docs.github.com/en/rest/issues/issue-dependencies'
  - >-
    https://docs.github.com/en/rest/using-the-rest-api/best-practices-for-using-the-rest-api
  - >-
    https://docs.github.com/en/graphql/overview/rate-limits-and-node-limits-for-the-graphql-api
documentation:
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-126
priority: high
type: spike
ordinal: 6000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The GitHub loading design (plan section 14, D14) depends on behavior nobody has verified live. Answer these questions with recorded evidence:
1. Does adding or removing a blocked_by dependency change the issue's updatedAt?
2. Does a conditional REST GET of `repos/{o}/{r}/issues?state=all&sort=updated&direction=desc&per_page=1` return 304 when nothing has changed? Does it return a new ETag after an issue edit, a comment, a label change, and a dependency change?
3. Does GraphQL `repository.issues` support `filterBy: {since}`, and `orderBy` on both CREATED_AT and UPDATED_AT, with cursor paging? Is totalCount stable across pages while issues change? Do `blockedBy` and `blocking` expose totalCount and their own pagination?
4. How many IDs does `nodes(ids:)` accept per query, and what does a query cost?
5. Which GraphQL rate-limit fields and headers come back?
6. How do a repository rename and an issue transfer appear: redirects, a changed nameWithOwner, a moved issue? How does an issue look to an account that loses access (404 versus 410)?
7. Where a GitHub Enterprise Server instance is accessible, which of these fields exist?

Human input is required. Before making any request, ask the user for a throwaway repository they own or approve. Mutations are allowed only in that repository. Use `gh` (including `gh api` and `gh api graphql`) for all GitHub access.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Each numbered question in the description is answered yes, no, or unknown. The task notes record the sanitized request, the relevant response fields and headers, the gh version, and the date
- [ ] #2 The GitHub table in plan section 3 summarizes the results, and plan section 14 (GitHub loading) and D14 are updated, or explicitly confirmed unchanged, in the same commit
- [ ] #3 Every unknown records why it could not be answered and which later task it affects
- [ ] #4 The recorded evidence contains no tokens, no private repository content, and no personal data beyond the approved repository's name
- [ ] #5 All mutations were confined to the user-approved repository
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run doc-test` passes and every changed relative link resolves
- [ ] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->
