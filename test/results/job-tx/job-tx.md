# Join Order Benchmark in ORM-style transactions through lqo serve --mode active

- **LQO:** lqo 0.6.0 (go1.26.10, linux/amd64)
- **Server:** PostgreSQL 16.15 (Ubuntu 16.15-1.pgdg24.04+2) on x86_64-pc-linux-gnu, compiled by gcc (Ubuntu 13.3.0-6ubuntu2~24.04.1) 13.3.0, 64-bit
- **Hints:** the 46 shapes proven in test/results/job-join-hints/job.json
- **Each call:** BEGIN; the query; a statement reading every planner setting a hint can change; COMMIT; extended protocol
- **Rounds:** 3, each query once with LQO's hints off and once on, alternating which goes first; times are medians

## Result

- Calls hinted, as lqo serve counts them: 151 of the 159 calls of proven shapes made with hints on
- Statements after a hinted query that saw any of its settings: 0
- One pass over all 113 queries: 143.3 s with hints off, 107.9 s on (-25%)
- The queries of proven shapes alone: 95.3 s off, 59.7 s on (-37%)

## Queries with a hint

| Query | Shape | Hint | Hinted calls | Off (ms) | On (ms) | Change |
|---|---|---|---:|---:|---:|---:|
| 1a | `0cf295d8` | no sequential scan | 3 of 3 | 82.2 | 6.3 | -92% |
| 1b | `078dfa3f` | no parallel workers | 3 of 3 | 75.8 | 1.2 | -98% |
| 1c | `ae99dc85` | no parallel workers | 3 of 3 | 81.8 | 4.6 | -94% |
| 1d | `450fe5f6` | no parallel workers | 3 of 3 | 80.3 | 1.1 | -99% |
| 2a | `dd1b70c1` | row counts | 3 of 3 | 365.2 | 273.2 | -25% |
| 2b | `dd1b70c1` | row counts | 1 of 3 | 334.0 | 238.5 | -29% |
| 2c | `dd1b70c1` | row counts | 2 of 3 | 323.9 | 22.4 | -93% |
| 2d | `dd1b70c1` | row counts | 1 of 3 | 427.0 | 497.5 | +17% |
| 7a | `cc66d5be` | row counts | 3 of 3 | 532.2 | 229.9 | -57% |
| 7b | `46b42be8` | row counts | 3 of 3 | 132.9 | 70.6 | -47% |
| 7c | `b8a77e31` | no index scan | 3 of 3 | 2142.2 | 1140.2 | -47% |
| 9c | `dd477274` | row counts, second pass | 3 of 3 | 1545.4 | 488.1 | -68% |
| 10a | `70b0406f` | no hash join | 3 of 3 | 326.7 | 238.8 | -27% |
| 10b | `6eab06ef` | no hash join | 3 of 3 | 220.1 | 107.3 | -51% |
| 11a | `c3eba7e4` | row counts | 3 of 3 | 46.6 | 28.5 | -39% |
| 11c | `850e1a80` | no parallel workers | 3 of 3 | 193.4 | 104.2 | -46% |
| 11d | `8461866c` | no parallel workers | 3 of 3 | 167.3 | 91.3 | -45% |
| 12b | `729ccb90` | no parallel workers | 3 of 3 | 83.6 | 3.8 | -95% |
| 13a | `91a61e77` | row counts | 3 of 3 | 1109.2 | 526.6 | -53% |
| 13b | `718b520c` | row counts | 3 of 3 | 545.8 | 158.8 | -71% |
| 13c | `718b520c` | row counts | 2 of 3 | 525.9 | 106.6 | -80% |
| 15b | `407c64ff` | no parallel workers | 3 of 3 | 35.0 | 28.6 | -18% |
| 15d | `5798935c` | no index scan | 3 of 3 | 493.9 | 400.9 | -19% |
| 16b | `8f30d4e5` | row counts | 3 of 3 | 12223.7 | 3625.6 | -70% |
| 17a | `6d17eb01` | row counts | 3 of 3 | 7159.7 | 2575.4 | -64% |
| 17b | `a801be56` | no nested loop | 3 of 3 | 4322.3 | 2563.2 | -41% |
| 17c | `a801be56` | no nested loop | 1 of 3 | 4084.4 | 2374.5 | -42% |
| 17d | `73f27e86` | row counts | 3 of 3 | 4157.3 | 3803.2 | -9% |
| 17e | `85cf65fe` | row counts | 3 of 3 | 7288.3 | 3277.8 | -55% |
| 17f | `73f27e86` | row counts | 3 of 3 | 5787.8 | 2781.8 | -52% |
| 18a | `2656274c` | row counts | 3 of 3 | 2471.4 | 1326.1 | -46% |
| 19c | `fa4f3af1` | no index scan | 3 of 3 | 1390.2 | 534.3 | -62% |
| 19d | `8cf8918f` | no memoize | 3 of 3 | 8230.8 | 3033.5 | -63% |
| 21b | `0cc52afa` | row counts | 3 of 3 | 55.6 | 33.8 | -39% |
| 23a | `6d1d5dbb` | no hash join | 3 of 3 | 288.9 | 144.1 | -50% |
| 23c | `6d1d5dbb` | no hash join | 3 of 3 | 360.7 | 248.7 | -31% |
| 24a | `39172b8c` | no memoize | 3 of 3 | 385.6 | 368.0 | -5% |
| 24b | `6ef26656` | no parallel workers | 3 of 3 | 86.3 | 60.1 | -30% |
| 26a | `2066cfd6` | no sequential scan | 3 of 3 | 17918.2 | 17888.8 | -0% |
| 26c | `7483ed43` | no index scan | 3 of 3 | 1679.1 | 2930.7 | +75% |
| 27a | `188e45da` | no parallel workers | 3 of 3 | 73.1 | 56.4 | -23% |
| 27b | `4a1dc513` | no memoize | 3 of 3 | 64.0 | 45.7 | -29% |
| 28b | `e4943b34` | no bitmap scan | 3 of 3 | 351.9 | 325.3 | -8% |
| 28c | `4e8b8a70` | no memoize | 3 of 3 | 1270.1 | 254.4 | -80% |
| 29a | `ff8164ee` | nested loop only | 3 of 3 | 111.9 | 72.4 | -35% |
| 29b | `05ae6e9d` | no parallel workers | 3 of 3 | 98.5 | 71.6 | -27% |
| 29c | `631a5812` | no merge join | 3 of 3 | 1058.3 | 1039.8 | -2% |
| 30a | `c213f9bf` | no sequential scan | 3 of 3 | 1669.0 | 2142.5 | +28% |
| 30c | `2b1949c5` | nested loop only | 3 of 3 | 2375.9 | 3049.5 | +28% |
| 31b | `311123fe` | row counts | 3 of 3 | 142.9 | 104.1 | -27% |
| 33a | `ddcd52ba` | nested loop only | 3 of 3 | 88.4 | 53.6 | -39% |
| 33b | `6744415b` | nested loop only | 3 of 3 | 84.4 | 52.6 | -38% |
| 33c | `0da43d7b` | no merge join | 3 of 3 | 145.5 | 64.7 | -56% |
