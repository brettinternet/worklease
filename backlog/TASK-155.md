---
id: TASK-155
title: Shorten CI critical path and avoid duplicate release validation
status: done
type: enhancement
priority: high
labels: [ci, testing, release, reviewed]
blocked_by: [TASK-156]
rank: ab3a62ce98b3a6230
refs: [.github/workflows/ci.yml, .github/workflows/release.yml, mise.toml, 'https://github.com/brettinternet/worklease/actions/runs/39329277019', 'https://github.com/brettinternet/worklease/actions/runs/39527899385']
created: 2026-10-21T01:56:00Z
updated: 2026-10-22T07:09:00Z
---

Publishing v1.9.3 launched CI runs 39527899383 and 39527899273 for the same commit via branch and tag pushes. Release run 39527899385 finished archive and container validation in about six minutes, then waited for the Linux quality job. Baseline run 39329277019 took about 24 minutes, dominated by 19m38s of sequential race tests. TASK-149 intentionally serialized gates and split CLI tests to prevent resource contention and package-deadline failures on small runners; simply restoring same-runner parallelism would regress that fix. The workflow also keys immutable Go build caches only by OS, architecture, and go.sum, excluding the Go toolchain version. Scope is workflow scheduling, cache identity, timing visibility, and exact-commit release gating; test-body optimization is separate.

## Acceptance criteria

- [x] Independent quality gates and race partitions can execute on separate runners with visible timings; every existing test and gate remains required, and partitions have demonstrably complete coverage without multiplying full-suite execution.
- [x] A branch and release-tag push for the same commit does not launch duplicate full CI suites; tag-only release attempts still obtain required validation, and publication fails closed unless the exact tagged commit passes all required gates.
- [x] Go build-cache identity distinguishes toolchain versions as well as platform and dependencies, and evidence demonstrates a new toolchain can populate a reusable cache.
- [x] Record comparable before/after CI critical-path and runner-minute measurements, showing reduced release wait without reintroducing resource-contention failures; verify both ordinary CI and the release-gating path.

## Plan

1. Capture post-queue-removal CI baseline and inspect all existing gates.
2. Extract reusable validation with isolated static, test/platform, race-partition, vulnerability, and e2e runners; preserve gates and add timing/cache toolchain identity.
3. Remove tag-triggered CI duplication; reuse exact-SHA main CI from release, falling back to reusable validation for tag-only commits, failing closed.
4. Validate workflow syntax, partition completeness, gating regressions, hooks, and local gates; perform one focused independent review.
5. Obtain authorized live CI/release dry-run measurements and cache evidence, then commit, merge, finalize task, and clean owned worktree.

Validation eligibility also includes explicitly dispatched CI for the identical SHA; PR merge/head runs remain excluded. This permits the authorized branch dry-run to exercise reuse without pushing main or publishing a tag.

## Notes

2026-10-21: Take before and after measurements on a tree where TASK-156 has removed the queue, so the comparison reflects the workflow changes and not the deleted suites.

Claimed in primary checkout. Owned implementation checkout: /Users/brett/dev/me/worklease/.worktrees/task-155, branch task-155; receipt .git/worktrees/task-155/agent-creation.json records session 01a1620c-a6dc-7288-ae92-fa6b5a32a500 and loop c1e7ab2c-e80b-4cb7-b494-91c34b6a286f. User authorized pushing task-155 and dispatching non-publishing CI/release dry runs; no tag creation, publication, or PR. Baseline post-queue-removal CI 39726608192 (f9b0f084) succeeded: 422s wall, 1162s summed job time (19.37 runner-minutes), quality 383s. A closer baseline run 39727343536 at cf903b6 is being collected.

Implementation commit 12bbe49 pushed only to origin/task-155 as authorized. Local checks passed: actionlint, shellcheck, clean script LSP diagnostics, staticcheck, format-check, go vet, full package tests, full race suite, e2e including remote smoke and doc-test, man generation, govulncheck, hooks. Partition audit: 1 CLI + 28 rest = all 29 packages with no overlap. Single focused reviewer pass returned no validated findings. Release dry-run 39728503358 at 12bbe49 dispatched first to exercise tag-only fallback with no existing exact-SHA CI. Closer warm baseline 39727343536 at cf903b6: 177s wall, jobs 171+127+41+65=404s (6.73 runner-minutes); all successful. Earlier 39726608192 was colder setup and took 422s wall / 1162 job-seconds.

Fallback lookup in release dry-run 39728503358 completed successfully and launched all nine required validation gates. Dispatched ordinary CI 39728926141 at the identical 12bbe49 SHA, then release reuse dry-run 39728952024 (serialized by existing release concurrency group). Awaiting live completion/cache timings; automatic condition watch resumes this session. No publication jobs run for workflow_dispatch.

Live fallback dry-run 39728503358 succeeded: archives and containers passed, no existing exact-SHA CI found, all nine required validation gates passed, release authorization passed, and both publication jobs were skipped. Ordinary CI 39728926141 attempt 1 passed all nine gates: 295s wall, no contention failures. Nine Go 1.27.3 gate/platform-specific caches were saved under go-v2 keys. Rerunning the same CI now that those caches exist to verify reusable warm caches and comparable warm measurements. Reuse release dry-run 39728952024 is running after queued serialization.

Negative release-gate verification: intentionally cancelled ordinary CI attempt 2 after prior successful attempt 1. Reuse dry-run 39728952024 must reject the now-cancelled latest validation rather than publish/reuse historical success. After it fails closed, rerun CI successfully for warm-cache timings, then rerun failed release-gating jobs. This cancellation is a deliberate authorization-boundary check, not a skipped/weakened test.

Fail-closed behavior verified live: release 39728952024 attempt 1 failed at Reuse exact-commit CI on the cancelled latest CI attempt; Tag-only validation, Authorize tagged release, and both publish jobs were skipped despite the earlier successful CI attempt. Started CI 39728926141 attempt 3 with all nine gates and reran failed release jobs (39728952024 attempt 2); this also exercises waiting for in-progress exact-SHA validation. No implementation corrections required.

Final live evidence (GitHub attempts API, post-TASK-156 Go sources/dependencies unchanged):

| CI sample | Run / attempt | Wall seconds | Summed job seconds | Unrounded runner-minutes |
|---|---|---:|---:|---:|
| Before, colder setup | 39726608192 / 1 | 423 | 1142 | 19.03 |
| After, cold Go-v2 caches | 39728926141 / 1 | 296 | 1145 | 19.08 |
| Before, warm | 39727343536 / 1 | 177 | 404 | 6.73 |
| After, warm | 39728926141 / 3 | 132 | 398 | 6.63 |

These final API numbers supersede preliminary 422/1162 and 295 figures above. Wall is run_started_at to updated_at; runner-minutes are summed job durations, not rounded/billing-weighted costs. Cold wall improved 30%; warm wall improved 25%, without materially increasing aggregate execution. Linux required-gate readiness went from 383s colder / 127s warm serial quality to max 163s cold / 41s warm isolated gates; macOS-x64 is now critical (290s cold / 124s warm). No unintended test failures, deadlines, or contention failures occurred. Every platform test, static build, format, lint, vet, hook exercise, race package, vulnerability scan, e2e smoke, doc-test and manual validation remains present and passed. Race partition audit proved 1 CLI + 28 rest = all 29 packages, with empty intersection.

Cache evidence: fallback release 39728503358 logs show all nine go-v2 platform/toolchain/gate/dependency keys initially missing and successfully saved for go1.27.3. Ordinary CI 39728926141 attempt 3 logs show all nine restored from the exact matching keys, including both race gates and all four platforms. Actual GOVERSION is read after mise installs the selected toolchain; neither primary nor restore keys cross toolchain versions.

Release paths: fallback 39728503358 / 1 succeeded in 506s wall / 1684 summed job seconds (28.07 min), validating all nine gates when no eligible exact-SHA CI existed. Reuse 39728952024 / 2 succeeded, watched in-progress CI 39728926141 / 3 for exact SHA 12bbe49e38114e3c353217e32b742c67a68424c3, skipped the whole tag-only suite, and authorized release. Its lookup/wait job took 124s; authorization 3s. The rerun took 151s wall; API reports 548 job seconds including reused archive/container jobs, so that sum is not a fresh full-release cost. Both publication jobs were skipped in every dispatch. Reuse attempt 1 rejected deliberately cancelled latest CI despite a successful older attempt, and skipped authorization/publication.

Release wait interpretation: warm old CI already finishes before typical archive/container readiness, so do not claim a positive warm release-wait reduction from zero. With the measured 246s archive/container readiness from the fallback run and simultaneous starts, the measured colder CI durations imply remaining CI wait falls from 177s to 50s (127s less); this is a matched-schedule estimate, not a separately observed before-release run. The live reuse retry deliberately starts while CI is running and demonstrates actual bounded waiting and no duplicate validation. Branch+tag no longer duplicates CI because only main pushes/PRs/explicit dispatch trigger CI; tag-only releases obtain the same reusable gates.

Single focused independent review found no concrete defects; all local checks and hooks passed. No implementation corrections or extra general review passes were needed. Live evidence URLs: https://github.com/brettinternet/worklease/actions/runs/39728926141/attempts/3 ; https://github.com/brettinternet/worklease/actions/runs/39728503358 ; https://github.com/brettinternet/worklease/actions/runs/39728952024/attempts/2 .

Delivery complete: implementation 12bbe49 integrated into local main by merge dbf256b after evidence commit 041ff2d. Post-merge actionlint, lint, format-check, typecheck, and package tests passed; workflow/race-script/mise files match the exact live-tested implementation SHA. Verified receipt and Worktrunk listing, inspected exact Herdr workspace wJ6 (one idle zsh pane), then removed owned task-155 checkout and local branch with wt remove --foreground. Post-remove hook closed wJ6; subsequent workspace/worktree listings confirm it disappeared. No owned checkout or workspace remains. Remote origin/task-155 is intentionally retained: remote-branch deletion is prohibited by the applicable instructions. Main was not pushed; no tag, publication, or PR was created. No remaining blocker or resumable item-scoped work; task claim released by terminal status and clearing assignee.

Review 2026-10-22: fixed two release-path gaps not exercised by the dispatch dry runs. (1) publish and publish-container used an implicit success() while tag-only validation is a skipped ancestor on the reuse path, which GitHub propagates as a skip; they now gate on !cancelled() && push && release-gate success. (2) CI concurrency grouped main pushes by ref with cancel-in-progress, so a later main push could cancel the tagged commit's CI and fail the release; only PR runs now share a group. actionlint and go test ./cmd/worklease-release pass.

## Summary

Split all required gates across isolated runners; exhaustive CLI/rest race partitions preserve full coverage. CI no longer runs on release tags; releases reuse exact-SHA CI or run the same required validation for tag-only commits, rejecting cancelled/failed CI. Toolchain/platform/gate-aware caches were populated and all nine restored live. Comparable post-queue-removal warm CI improved 177s to 132s (25%) at 6.73 to 6.63 unrounded runner-minutes; colder CI 423s to 296s at essentially equal aggregate time. Recorded actual release fallback/reuse and fail-closed evidence, plus explicitly labeled release-wait estimates. Local checks, full race/e2e, live CI, both release dry-run paths, and focused review passed. Merged locally as dbf256b; owned worktree/local branch/workspace removed. Main unpushed; remote task branch retained.
