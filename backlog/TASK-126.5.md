---
id: TASK-126.5
title: Probe GitHub sync, dependency, and identity behavior
status: done
type: spike
priority: high
assignees: ['@brettinternet']
labels: [work-queue, github, reviewed]
milestone: m-1
parent: TASK-126
rank: a0ac7691840ac7688
refs: ['https://docs.github.com/en/rest/issues/issue-dependencies', 'https://docs.github.com/en/rest/using-the-rest-api/best-practices-for-using-the-rest-api', 'https://docs.github.com/en/graphql/overview/rate-limits-and-node-limits-for-the-graphql-api', docs/work-queue-tui-proposal.md]
created: 2026-09-23T04:29:00Z
updated: 2026-09-23T20:59:00Z
---

The GitHub loading design (plan section 14, D14) depends on behavior nobody has verified live. Answer these questions with recorded evidence:
1. Does adding or removing a blocked_by dependency change the issue's updatedAt?
2. Does a conditional REST GET of `repos/{o}/{r}/issues?state=all&sort=updated&direction=desc&per_page=1` return 304 when nothing has changed? Does it return a new ETag after an issue edit, a comment, a label change, and a dependency change?
3. Does GraphQL `repository.issues` support `filterBy: {since}`, and `orderBy` on both CREATED_AT and UPDATED_AT, with cursor paging? Is totalCount stable across pages while issues change? Do `blockedBy` and `blocking` expose totalCount and their own pagination?
4. How many IDs does `nodes(ids:)` accept per query, and what does a query cost?
5. Which GraphQL rate-limit fields and headers come back?
6. How do a repository rename and an issue transfer appear: redirects, a changed nameWithOwner, a moved issue? How does an issue look to an account that loses access (404 versus 410)?
7. Where a GitHub Enterprise Server instance is accessible, which of these fields exist?

Human input is required. Before making any request, ask the user for a throwaway repository they own or approve. Mutations are allowed only in that repository. Use `gh` (including `gh api` and `gh api graphql`) for all GitHub access.

## Acceptance criteria

- [x] Each numbered question in the description is answered yes, no, or unknown. The task notes record the sanitized request, the relevant response fields and headers, the gh version, and the date
- [x] The GitHub table in plan section 3 summarizes the results, and plan section 14 (GitHub loading) and D14 are updated, or explicitly confirmed unchanged, in the same commit
- [x] Every unknown records why it could not be answered and which later task it affects
- [x] The recorded evidence contains no tokens, no private repository content, and no personal data beyond the approved repository's name
- [x] All mutations were confined to the user-approved repository

## Definition of done

- [x] `mise run doc-test` passes and every changed relative link resolves
- [x] Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit

## Plan

1. Probe seven GitHub API behavior questions in the user-approved brettinternet/worklease repository using bounded, serial gh requests; perform only clearly labeled synthetic fixture mutations and clean up created issues, labels, and dependency edges. 2. Record sanitized requests, relevant response fields/headers, gh version/date, and unknowns with downstream task impacts in task notes. 3. Update proposal §3, §14, and D14 conservatively for measured evidence and unknowns. 4. Run doc-test and link validation; record evidence and finalize task through Backlog CLI.

## Notes

Live probe evidence (2026-09-23; gh version 2.101.0; github.com repository brettinternet/worklease, the user-approved target). Requests were serial. Only synthetic fixtures were mutated; no tokens, identities, or real issue payloads are recorded.

1. Dependency timestamps: POST /repos/{owner}/{repo}/issues/{blocked-number}/dependencies/blocked_by with issue_id set to blocker database ID returned success and an issue summary with updated_at advanced. DELETE /repos/{owner}/{repo}/issues/{blocked-number}/dependencies/blocked_by/{blocker-database-id} returned success; subsequent GET /issues/{number} retained exact pre-removal updated_at. Answer: adding yes; removing no.
2. Conditional REST: GET /repos/{owner}/{repo}/issues?state=all&sort=updated&direction=desc&per_page=1 returned 200 with weak ETag; repeating with If-None-Match returned 304 and ETag. Subsequent reads returned changed ETags after synthetic issue title edit, comment, label change, and dependency addition. Answer: 304 yes; new ETag after each tested change yes. This does not prove ETag changes for dependency removal.
3. GraphQL: repository.issues(first, after, filterBy:{since}, orderBy:{field:UPDATED_AT|CREATED_AT,direction}) returned successful cursor pages for both order fields and since; nodes included updatedAt, blockedBy{totalCount,pageInfo}, and blocking{totalCount,pageInfo}. Both dependency connections returned their own totalCount and pageInfo. On a two-page CREATED_AT scan, totalCount was 2 on page one; after creating a synthetic issue before page two, totalCount was 3 on page two. Answer: support yes; totalCount stable across changing pages no.
4. GraphQL nodes(ids:): 100 repeated valid synthetic-fixture IDs were accepted; 101 returned ARGUMENT_LIMIT (no more than 100 node ids). 100-ID query reported rateLimit.cost: 1. Answer: maximum 100; observed cost 1 for that query.
5. GraphQL response fields: rateLimit{cost,remaining,resetAt,used}; sample was cost 1, remaining 4819, used 181, with reset timestamp. REST and GraphQL response headers included X-RateLimit-Limit (5000), X-RateLimit-Remaining, X-RateLimit-Reset, X-RateLimit-Resource (core/graphql), and X-RateLimit-Used. REST also exposed ETag on the conditional endpoint. No retry-after header was present in sampled responses.
6. Rename, transfer, and lost-access response: unknown; user prohibited repository renames, issue transfers, and access/permission changes. Do not infer from docs. Downstream impact: TASK-128.5 (GitHub Issues source identity/visibility).
7. GHES: unknown; no instance was provided. Downstream impact: TASK-128.5 (per-host schema/capability discovery).

Cleanup evidence: all three synthetic issues are closed with no remaining label or dependency edge; synthetic label lookup returned 404 after deletion. Existing issues were read only for repository-level metadata/list metadata; no existing issue content was inspected. Initial dependency add using an issue number instead of the documented database ID returned 404 and had no effect; retry with database ID succeeded. No repository rename, transfer, access change, push, or PR occurred.

Validation: `mise run doc-test` passed (`worklease documentation examples passed`). `git diff --check -- docs/work-queue-tui-proposal.md` passed. No relative links were added or changed.

Review: proposal §3 no longer claims ETags miss dependency removals; that behavior is recorded as unmeasured.

## Summary

Probed GitHub Issues REST and GraphQL behavior with bounded synthetic fixtures in the approved repository, recorded sanitized evidence and unknowns, and updated proposal section 3, section 14, and D14. `mise run doc-test` and diff whitespace validation passed; all created fixtures were cleaned up (issues closed, edge and label removed).
