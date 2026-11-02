# Join Order Benchmark in ORM-style transactions through lqo serve --mode active

- **LQO:** lqo 0.7.0 (go1.26.10, linux/amd64)
- **Server:** PostgreSQL 16.15 (Ubuntu 16.15-1.pgdg24.04+2) on x86_64-pc-linux-gnu, compiled by gcc (Ubuntu 13.3.0-6ubuntu2~24.04.1) 13.3.0, 64-bit
- **Hints:** the 46 shapes proven in test/results/job-join-hints/job.json
- **Each call:** BEGIN; the query; a statement reading every planner setting a hint can change; COMMIT; extended protocol
- **Rounds:** 3, each query once with LQO's hints off and once on, alternating which goes first; times are medians

## Result

- Calls hinted, as lqo serve counts them: 159 of the 159 calls of proven shapes made with hints on
- Statements after a hinted query that saw any of its settings: 0
- One pass over all 113 queries: 180.7 s with hints off, 68.5 s on (-62%)
- The queries of proven shapes alone: 147.5 s off, 35.3 s on (-76%)

## Queries with a hint

| Query | Shape | Hint | Hinted calls | Off (ms) | On (ms) | Change |
|---|---|---|---:|---:|---:|---:|
| 1a | `0cf295d8` | no parallel workers | 3 of 3 | 69.9 | 7.0 | -90% |
| 1b | `078dfa3f` | no parallel workers | 3 of 3 | 64.2 | 1.3 | -98% |
| 1c | `ae99dc85` | no parallel workers | 3 of 3 | 66.3 | 4.5 | -93% |
| 1d | `450fe5f6` | no hash join | 3 of 3 | 63.1 | 1.4 | -98% |
| 2a | `dd1b70c1` | row counts | 3 of 3 | 381.8 | 233.6 | -39% |
| 2b | `dd1b70c1` | row counts | 3 of 3 | 323.9 | 203.2 | -37% |
| 2c | `dd1b70c1` | row counts | 3 of 3 | 308.4 | 20.6 | -93% |
| 2d | `dd1b70c1` | row counts | 3 of 3 | 407.9 | 400.2 | -2% |
| 7a | `cc66d5be` | row counts, second pass | 3 of 3 | 555.8 | 187.7 | -66% |
| 7b | `46b42be8` | row counts | 3 of 3 | 135.4 | 96.7 | -29% |
| 7c | `b8a77e31` | no index scan | 3 of 3 | 1944.0 | 1026.6 | -47% |
| 10a | `70b0406f` | no hash join | 3 of 3 | 290.7 | 218.8 | -25% |
| 10b | `6eab06ef` | no hash join | 3 of 3 | 193.7 | 97.9 | -49% |
| 10c | `534b68fc` | no nested loop | 3 of 3 | 5165.6 | 2309.2 | -55% |
| 11a | `c3eba7e4` | row counts | 3 of 3 | 46.5 | 39.0 | -16% |
| 11c | `850e1a80` | row counts | 3 of 3 | 949.8 | 78.2 | -92% |
| 11d | `8461866c` | no parallel workers | 3 of 3 | 160.0 | 78.3 | -51% |
| 12a | `46c94c7f` | row counts | 3 of 3 | 176.7 | 171.7 | -3% |
| 12b | `729ccb90` | no parallel workers | 3 of 3 | 69.0 | 3.7 | -95% |
| 12c | `fc50c266` | no hash join | 3 of 3 | 672.5 | 590.8 | -12% |
| 13a | `91a61e77` | row counts | 3 of 3 | 1063.4 | 480.2 | -55% |
| 13b | `718b520c` | row counts | 3 of 3 | 508.6 | 148.9 | -71% |
| 13c | `718b520c` | row counts | 3 of 3 | 497.8 | 103.8 | -79% |
| 15d | `5798935c` | no index scan | 3 of 3 | 483.0 | 362.5 | -25% |
| 16b | `8f30d4e5` | row counts | 3 of 3 | 11279.6 | 3500.3 | -69% |
| 17a | `6d17eb01` | row counts | 3 of 3 | 6798.8 | 2408.6 | -65% |
| 17b | `a801be56` | no nested loop | 3 of 3 | 4302.5 | 2173.7 | -49% |
| 17c | `a801be56` | no nested loop | 3 of 3 | 4008.6 | 1914.7 | -52% |
| 17d | `73f27e86` | row counts | 3 of 3 | 4095.7 | 673.5 | -84% |
| 17e | `85cf65fe` | row counts | 3 of 3 | 6596.5 | 3052.2 | -54% |
| 17f | `73f27e86` | row counts | 3 of 3 | 5605.7 | 2519.1 | -55% |
| 18a | `2656274c` | row counts | 3 of 3 | 2356.4 | 334.3 | -86% |
| 19d | `8cf8918f` | no memoize | 3 of 3 | 7739.9 | 2798.1 | -64% |
| 21b | `0cc52afa` | row counts | 3 of 3 | 54.1 | 31.9 | -41% |
| 23a | `6d1d5dbb` | no hash join | 3 of 3 | 258.3 | 140.6 | -46% |
| 23c | `6d1d5dbb` | no hash join | 3 of 3 | 349.5 | 242.9 | -31% |
| 24b | `6ef26656` | no parallel workers | 3 of 3 | 86.1 | 59.3 | -31% |
| 25c | `92588c28` | no nested loop | 3 of 3 | 4706.3 | 3180.0 | -32% |
| 26a | `2066cfd6` | no index scan | 3 of 3 | 16105.0 | 455.8 | -97% |
| 26c | `7483ed43` | no bitmap scan | 3 of 3 | 51616.7 | 1571.2 | -97% |
| 27a | `188e45da` | nested loop only | 3 of 3 | 69.5 | 53.6 | -23% |
| 27b | `4a1dc513` | nested loop only | 3 of 3 | 63.3 | 45.4 | -28% |
| 27c | `c862384f` | nested loop only | 3 of 3 | 79.1 | 53.1 | -33% |
| 28c | `4e8b8a70` | no parallel workers | 3 of 3 | 445.8 | 276.8 | -38% |
| 29b | `05ae6e9d` | nested loop only | 3 of 3 | 96.0 | 69.8 | -27% |
| 29c | `631a5812` | no hash join | 3 of 3 | 159.1 | 125.6 | -21% |
| 30a | `c213f9bf` | no bitmap scan | 3 of 3 | 3200.6 | 684.8 | -79% |
| 30c | `2b1949c5` | no bitmap scan | 3 of 3 | 2132.3 | 1544.4 | -28% |
| 31b | `311123fe` | row counts | 3 of 3 | 125.6 | 95.2 | -24% |
| 31c | `442faf79` | row counts, second pass | 3 of 3 | 289.5 | 227.3 | -21% |
| 33a | `ddcd52ba` | nested loop only | 3 of 3 | 88.2 | 58.9 | -33% |
| 33b | `6744415b` | nested loop only | 3 of 3 | 83.5 | 52.5 | -37% |
| 33c | `0da43d7b` | no memoize | 3 of 3 | 133.7 | 64.7 | -52% |
