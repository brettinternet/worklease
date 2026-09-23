# Queue benchmark measurements (TASK-129.6, preliminary)

Reference host: Apple M1 Max, 32 GiB, macOS arm64, 2026-09-23. Run `mise run queue-benchmark` for the fixed, deterministic Go fixtures and sample quantiles. The runner reports nearest-rank p50/p95/p99; 20 samples except SQLite (3, because each run populates 10,000 records). Subprocess peak RSS includes the Go test process and fixture setup; it is **not** a standalone TUI RSS measurement. Time is Update+View, not physical terminal input-to-paint. GitHub is an in-process fake GraphQL service with 5 ms per data page; request and response totals exclude authentication. These measurements must not be presented as an end-to-end pass of §14.

| Measured operation | p50 / p95 / p99 (ms) | Other evidence |
| --- | --- | --- |
| Warm first view, 10,000 summaries | 56.06 / 60.29 / 62.33 | 55.7 MB allocated per operation; one process |
| Navigation Update+View, 10,000 summaries | 0.331 / 0.419 / 1.150 | 78 KB allocated/op; warm projection |
| Navigation Update+View, 50,000 summaries across five sources | 1.172 / 1.321 / 1.604 | 78 KB allocated/op; peak process RSS 286.9 MB in one-sample run |
| Navigation Update+View, 100,000 summaries across five sources | 2.149 / 2.333 / 2.339 | 78 KB allocated/op; peak process RSS 616.2 MB in one-sample run |
| Indexed FTS match among 10,000 summaries | 52.31 / 54.10 / 54.10 | ~22.0 MB SQLite database plus WAL; three samples |
| GitHub 10,000-summary enumeration | 609.25 / 619.46 / 628.29 | 100 GraphQL data requests, 1,252,057 response bytes, 5 ms/page; quota cost to be checked against real GraphQL cost semantics |

The initial uncached TUI navigation benchmark measured p95 147.50 ms at 10,000 and 4,957.92 ms at 100,000. Caching the sorted projection removes repeated full-corpus sorting on each keystroke. The 50,000-summary RSS provisional 256 MiB budget is **not proven** by a 286.9 MB test-process peak; the standalone process and concurrent-refresh peak remain to be measured. The 100,000-summary failure boundary, cold remote view, terminal paint, combined fault/refresh scenario, Backlog process metrics, quota costs, and renewal margin remain open. Do not check off §14 until those measurements and decisions are recorded.
