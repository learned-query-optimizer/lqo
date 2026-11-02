# Join Order Benchmark in ORM-style transactions through lqo serve --mode active

- **LQO:** lqo 0.5.0 (go1.26.10, linux/amd64)
- **Server:** PostgreSQL 16.15 (Ubuntu 16.15-1.pgdg24.04+2) on x86_64-pc-linux-gnu, compiled by gcc (Ubuntu 13.3.0-6ubuntu2~24.04.1) 13.3.0, 64-bit
- **Hints:** the 46 shapes proven in test/results/job-join-hints/job.json
- **Each call:** BEGIN; the query; a statement reading every planner setting a hint can change; COMMIT; extended protocol
- **Rounds:** 3, each query once with LQO's hints off and once on, alternating which goes first; times are medians

## Result

- Calls hinted, as lqo serve counts them: 151 of the 159 calls of proven shapes made with hints on
- Statements after a hinted query that saw any of its settings: 0
- One pass over all 113 queries: 132.9 s with hints off, 72.9 s on (-45%)
- The queries of proven shapes alone: 92.7 s off, 32.6 s on (-65%)

## Queries with a hint

| Query | Shape | Hint | Off (ms) | On (ms) | Change |
|---|---|---|---:|---:|---:|
| 1a | `0cf295d8` | no sequential scan | 83.3 | 6.5 | -92% |
| 1b | `078dfa3f` | no parallel workers | 79.6 | 1.1 | -99% |
| 1c | `ae99dc85` | no parallel workers | 77.3 | 4.3 | -94% |
| 1d | `450fe5f6` | no parallel workers | 74.5 | 1.0 | -99% |
| 2a | `dd1b70c1` | row counts | 355.5 | 268.9 | -24% |
| 2b | `dd1b70c1` | row counts | 330.3 | 250.6 | -24% |
| 2c | `dd1b70c1` | row counts | 313.1 | 22.8 | -93% |
| 2d | `dd1b70c1` | row counts | 419.0 | 436.7 | +4% |
| 7a | `cc66d5be` | row counts | 473.4 | 177.8 | -62% |
| 7b | `46b42be8` | row counts | 121.3 | 72.4 | -40% |
| 7c | `b8a77e31` | no index scan | 2121.2 | 1132.1 | -47% |
| 9c | `dd477274` | row counts, second pass | 1481.7 | 487.9 | -67% |
| 10a | `70b0406f` | no hash join | 326.3 | 241.7 | -26% |
| 10b | `6eab06ef` | no hash join | 219.0 | 111.4 | -49% |
| 11a | `c3eba7e4` | row counts | 47.9 | 27.9 | -42% |
| 11c | `850e1a80` | no parallel workers | 187.9 | 101.4 | -46% |
| 11d | `8461866c` | no parallel workers | 166.2 | 87.2 | -48% |
| 12b | `729ccb90` | no parallel workers | 81.5 | 3.4 | -96% |
| 13a | `91a61e77` | row counts | 1120.4 | 533.3 | -52% |
| 13b | `718b520c` | row counts | 561.8 | 157.0 | -72% |
| 13c | `718b520c` | row counts | 543.5 | 108.0 | -80% |
| 15b | `407c64ff` | no parallel workers | 35.0 | 27.1 | -23% |
| 15d | `5798935c` | no index scan | 487.4 | 377.6 | -23% |
| 16b | `8f30d4e5` | row counts | 11774.4 | 3554.3 | -70% |
| 17a | `6d17eb01` | row counts | 7054.6 | 2519.9 | -64% |
| 17b | `a801be56` | no nested loop | 3942.0 | 2583.8 | -34% |
| 17c | `a801be56` | no nested loop | 3669.2 | 2365.1 | -36% |
| 17d | `73f27e86` | row counts | 3736.4 | 755.2 | -80% |
| 17e | `85cf65fe` | row counts | 7011.1 | 3055.5 | -56% |
| 17f | `73f27e86` | row counts | 5415.9 | 2717.3 | -50% |
| 18a | `2656274c` | row counts | 2455.3 | 1277.4 | -48% |
| 19c | `fa4f3af1` | no index scan | 1349.9 | 552.3 | -59% |
| 19d | `8cf8918f` | no memoize | 8159.0 | 3001.3 | -63% |
| 21b | `0cc52afa` | row counts | 53.1 | 31.2 | -41% |
| 23a | `6d1d5dbb` | no hash join | 206.9 | 140.0 | -32% |
| 23c | `6d1d5dbb` | no hash join | 358.8 | 245.9 | -31% |
| 24a | `39172b8c` | no memoize | 386.5 | 182.8 | -53% |
| 24b | `6ef26656` | no parallel workers | 86.1 | 60.2 | -30% |
| 26a | `2066cfd6` | no sequential scan | 17801.6 | 728.3 | -96% |
| 26c | `7483ed43` | no index scan | 3783.9 | 961.3 | -75% |
| 27a | `188e45da` | no parallel workers | 71.6 | 50.4 | -30% |
| 27b | `4a1dc513` | no memoize | 61.9 | 44.2 | -29% |
| 28b | `e4943b34` | no bitmap scan | 341.4 | 219.4 | -36% |
| 28c | `4e8b8a70` | no memoize | 1285.3 | 237.4 | -82% |
| 29a | `ff8164ee` | nested loop only | 100.7 | 69.2 | -31% |
| 29b | `05ae6e9d` | no parallel workers | 95.6 | 69.7 | -27% |
| 29c | `631a5812` | no merge join | 246.5 | 182.4 | -26% |
| 30a | `c213f9bf` | no sequential scan | 1150.6 | 615.8 | -46% |
| 30c | `2b1949c5` | nested loop only | 1914.3 | 1453.6 | -24% |
| 31b | `311123fe` | row counts | 145.8 | 98.6 | -32% |
| 33a | `ddcd52ba` | nested loop only | 93.0 | 52.8 | -43% |
| 33b | `6744415b` | nested loop only | 82.0 | 50.6 | -38% |
| 33c | `0da43d7b` | no merge join | 141.5 | 79.5 | -44% |
