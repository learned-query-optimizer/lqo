# Join Order Benchmark in ORM-style transactions through lqo serve --mode active

- **LQO:** lqo 0.6.0 (go1.26.10, linux/amd64)
- **Server:** PostgreSQL 16.15 (Ubuntu 16.15-1.pgdg24.04+2) on x86_64-pc-linux-gnu, compiled by gcc (Ubuntu 13.3.0-6ubuntu2~24.04.1) 13.3.0, 64-bit
- **Hints:** the 44 shapes proven in test/results/job-join-hints/job.json
- **Each call:** BEGIN; the query; a statement reading every planner setting a hint can change; COMMIT; extended protocol
- **Rounds:** 3, each query once with LQO's hints off and once on, alternating which goes first; times are medians

## Result

- Calls hinted, as lqo serve counts them: 153 of the 153 calls of proven shapes made with hints on
- Statements after a hinted query that saw any of its settings: 0
- One pass over all 113 queries: 152.8 s with hints off, 63.7 s on (-58%)
- The queries of proven shapes alone: 117.6 s off, 28.9 s on (-75%)

## Queries with a hint

| Query | Shape | Hint | Hinted calls | Off (ms) | On (ms) | Change |
|---|---|---|---:|---:|---:|---:|
| 1a | `0cf295d8` | no sequential scan | 3 of 3 | 72.1 | 5.7 | -92% |
| 1b | `078dfa3f` | no parallel workers | 3 of 3 | 66.6 | 1.1 | -98% |
| 1c | `ae99dc85` | no hash join | 3 of 3 | 68.5 | 3.7 | -95% |
| 1d | `450fe5f6` | no hash join | 3 of 3 | 63.0 | 1.0 | -98% |
| 2a | `dd1b70c1` | row counts | 3 of 3 | 305.1 | 227.4 | -25% |
| 2b | `dd1b70c1` | row counts | 3 of 3 | 273.0 | 215.0 | -21% |
| 2c | `dd1b70c1` | row counts | 3 of 3 | 258.8 | 20.9 | -92% |
| 2d | `dd1b70c1` | row counts | 3 of 3 | 370.2 | 390.1 | +5% |
| 7a | `cc66d5be` | row counts, second pass | 3 of 3 | 498.6 | 185.4 | -63% |
| 7b | `46b42be8` | row counts | 3 of 3 | 112.2 | 71.7 | -36% |
| 7c | `b8a77e31` | no bitmap scan | 3 of 3 | 1638.5 | 988.8 | -40% |
| 10a | `70b0406f` | no hash join | 3 of 3 | 311.0 | 241.2 | -22% |
| 10b | `6eab06ef` | no hash join | 3 of 3 | 206.5 | 103.1 | -50% |
| 11a | `c3eba7e4` | row counts | 3 of 3 | 39.5 | 23.7 | -40% |
| 11c | `850e1a80` | no parallel workers | 3 of 3 | 148.9 | 81.1 | -46% |
| 11d | `8461866c` | no parallel workers | 3 of 3 | 136.8 | 68.5 | -50% |
| 12b | `729ccb90` | no parallel workers | 3 of 3 | 68.8 | 2.9 | -96% |
| 13a | `91a61e77` | row counts | 3 of 3 | 953.5 | 506.2 | -47% |
| 13b | `718b520c` | row counts | 3 of 3 | 463.5 | 133.8 | -71% |
| 13c | `718b520c` | row counts | 3 of 3 | 448.9 | 96.2 | -79% |
| 15b | `407c64ff` | no parallel workers | 3 of 3 | 30.1 | 23.4 | -22% |
| 15d | `5798935c` | no index scan | 3 of 3 | 409.7 | 314.1 | -23% |
| 16b | `8f30d4e5` | row counts | 3 of 3 | 9365.7 | 3157.5 | -66% |
| 17a | `6d17eb01` | row counts | 3 of 3 | 5804.1 | 2518.1 | -57% |
| 17b | `a801be56` | no nested loop | 3 of 3 | 4112.5 | 2230.0 | -46% |
| 17c | `a801be56` | no nested loop | 3 of 3 | 3911.7 | 1944.6 | -50% |
| 17d | `73f27e86` | row counts | 3 of 3 | 3995.7 | 637.7 | -84% |
| 17e | `85cf65fe` | no nested loop | 3 of 3 | 5887.9 | 2887.4 | -51% |
| 17f | `73f27e86` | row counts | 3 of 3 | 5149.4 | 2369.8 | -54% |
| 18a | `2656274c` | row counts | 3 of 3 | 2183.6 | 347.6 | -84% |
| 19d | `8cf8918f` | no memoize | 3 of 3 | 6666.8 | 2460.6 | -63% |
| 21b | `0cc52afa` | row counts | 3 of 3 | 40.3 | 24.5 | -39% |
| 23a | `6d1d5dbb` | no hash join | 3 of 3 | 252.9 | 128.0 | -49% |
| 23c | `6d1d5dbb` | no hash join | 3 of 3 | 332.2 | 230.4 | -31% |
| 24b | `6ef26656` | no parallel workers | 3 of 3 | 67.9 | 47.4 | -30% |
| 25c | `92588c28` | no nested loop | 3 of 3 | 4506.6 | 2944.8 | -35% |
| 26a | `2066cfd6` | no index scan | 3 of 3 | 12230.9 | 479.9 | -96% |
| 26b | `135e5bc8` | no hash join | 3 of 3 | 126.7 | 83.7 | -34% |
| 26c | `7483ed43` | no hash join | 3 of 3 | 39484.7 | 1023.2 | -97% |
| 27a | `188e45da` | nested loop only | 3 of 3 | 53.5 | 41.9 | -22% |
| 27b | `4a1dc513` | no memoize | 3 of 3 | 47.5 | 34.4 | -27% |
| 27c | `c862384f` | no parallel workers | 3 of 3 | 61.2 | 47.3 | -23% |
| 28b | `e4943b34` | nested loop only | 3 of 3 | 638.4 | 190.7 | -70% |
| 29a | `ff8164ee` | no parallel workers | 3 of 3 | 77.0 | 55.6 | -28% |
| 29b | `05ae6e9d` | no memoize | 3 of 3 | 73.3 | 57.6 | -21% |
| 29c | `631a5812` | no memoize | 3 of 3 | 829.2 | 97.3 | -88% |
| 30a | `c213f9bf` | no parallel workers | 3 of 3 | 4360.7 | 889.1 | -80% |
| 31b | `311123fe` | row counts | 3 of 3 | 104.2 | 84.2 | -19% |
| 33a | `ddcd52ba` | nested loop only | 3 of 3 | 68.8 | 45.2 | -34% |
| 33b | `6744415b` | nested loop only | 3 of 3 | 73.8 | 43.1 | -42% |
| 33c | `0da43d7b` | nested loop only | 3 of 3 | 102.4 | 48.4 | -53% |
