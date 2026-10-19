# LQO, a learned query optimizer for PostgreSQL

PostgreSQL's planner picks each query's plan from row counts it estimates. Most of
the time the estimates are close enough. Sometimes they're off by a factor of a
thousand, usually because two columns are related in a way the statistics can't
see, and the planner picks a plan built for ten rows that then runs for ten
thousand. The query that took 40 ms takes four seconds.

LQO finds those queries for you. It reads your workload, runs each kind of query
under PostgreSQL's default plan and under a set of planner settings, on a replica
or a copy, and tells you which queries a setting makes clearly and consistently
faster, with the exact `SET LOCAL` lines to use.

It doesn't replace PostgreSQL's optimizer. It steers it, with PostgreSQL's own
planner switches (`enable_nestloop`, `enable_hashjoin` and the rest), so every
plan it suggests is one PostgreSQL already knows how to run.

LQO works two ways. `lqo advise` reads a SQL file or a server log and tests
what's in it. `lqo serve`, new in 0.2, sits between your application and the
database as a proxy, measures every query as it runs, and learns on a replica
which settings help which queries. Neither changes anything on the server: they
report, and you decide.

- Website: https://lqo.net
- License: Apache 2.0
- One binary, no dependencies, for Linux, macOS and Windows
- Works with PostgreSQL 14 to 18, managed services included, with no extension

## Contents

1. [Install](#install)
2. [Try it in two minutes](#try-it-in-two-minutes)
3. [Use it on your database](#use-it-on-your-database)
4. [Reading the report](#reading-the-report)
5. [How LQO decides](#how-lqo-decides)
6. [Learn from live traffic: lqo serve](#learn-from-live-traffic-lqo-serve)
7. [What LQO will and won't run](#what-lqo-will-and-wont-run)
8. [Commands](#commands)
9. [What 0.2 doesn't do](#what-02-doesnt-do)
10. [Building and testing](#building-and-testing)

## Install

Download the archive for your system from the
[latest release](https://github.com/learned-query-optimizer/lqo/releases/latest)
and put `lqo` somewhere on your PATH.

```sh
# Linux, x86-64
curl -LO https://github.com/learned-query-optimizer/lqo/releases/latest/download/lqo_linux_amd64.tar.gz
tar -xzf lqo_linux_amd64.tar.gz lqo
./lqo version
```

The other files are `lqo_linux_arm64.tar.gz`, `lqo_darwin_arm64.tar.gz` (Apple
silicon), `lqo_darwin_amd64.tar.gz` (Intel Mac) and `lqo_windows_amd64.zip`.
`SHA256SUMS` lists their checksums. macOS may ask you to allow the binary the first
time, since it isn't signed by Apple; `xattr -d com.apple.quarantine lqo` does it
from a terminal.

With Go 1.24 or later you can also build it: `go install github.com/learned-query-optimizer/lqo/cmd/lqo@latest`.

## Try it in two minutes

You need a PostgreSQL server you can create a schema on. A test database or a
throwaway container is ideal:

```sh
docker run -d --name pg -e POSTGRES_PASSWORD=secret -p 5432:5432 postgres:18
export LQO_DSN="postgres://postgres:secret@localhost:5432/postgres?sslmode=disable"
```

`lqo demo` builds a small shop in the `lqo_demo` schema, 200,000 customers, a
million orders and three million order lines, made so the planner's estimates go
wrong in two well-known ways. It writes a workload file for it too:

```sh
lqo demo
lqo advise --allow-primary lqo-demo.sql
```

`--allow-primary` is there because a fresh container is a primary server, and LQO
normally only runs on a standby (more on that below). After a minute or two you get
a report like this one, from the recorded run in
[test/results/demo/report.txt](test/results/demo/report.txt):

```
Faster with a hint: 2 of 5 tested

  1. 891f0819  SELECT sum(l.qty) FROM lqo_demo.customers c JOIN lqo_demo.order…  (2 calls)
     1,061 ms → 684 ms (−36%) with "no nested loop"
     Plan: Nested Loop ×2, Bitmap Heap Scan on orders, Index Scan on order_lines → Hash Join ×2, Seq Scan on order_lines, Seq Scan on orders
       SET LOCAL enable_nestloop = off;

  2. 6c73a7a6  SELECT id, created_at FROM lqo_demo.customers WHERE attrs->>'ti…  (1 call)
     35.1 ms → 24.7 ms (−30%) with "no index scan"
     Plan: Index Scan on customers → Seq Scan on customers
       SET LOCAL enable_indexscan = off;
       SET LOCAL enable_indexonlyscan = off;
```

The second one is a borderline case: on some runs its timings overlap and LQO
keeps the default plan instead. That's the rule working as meant.

The demo's workload file explains each query and why LQO should or shouldn't touch
it. One of them is a query where switching off index scans would make one of its
values hundreds of times slower, and LQO leaves it alone.

## Use it on your database

### 1. Point it at a replica

LQO runs every query it tests several times, under each setting, and some of those
plans are slow on purpose. Run it against a streaming replica, a restored backup or
a staging copy with production-like data. By default `lqo advise` checks
`pg_is_in_recovery()` and refuses to run on a primary. If you're sure, for a copy or
a test server, add `--allow-primary`.

Every trial is read-only and rolled back (see
[What LQO will and won't run](#what-lqo-will-and-wont-run)), so LQO itself changes
nothing. The cost on a primary is load: the queries really run, and so does any
function they call. Use a copy if that worries you.

On a replica, long trials can be cancelled by replication conflicts. If that
happens often, `hot_standby_feedback = on` or a larger `max_standby_streaming_delay`
on the replica helps, or use a copy that isn't replicating.

### 2. Give it a workload

LQO reads either a SQL file or a PostgreSQL server log.

**A SQL file** is statements separated by semicolons, with the real values filled
in. Comments are fine.

**A server log** is better, because it says which queries are slow and how often
they run. Turn on one of these on the primary for a while, then copy the log:

```sql
-- Log every statement that takes more than 100 ms, with its duration:
ALTER SYSTEM SET log_min_duration_statement = '100ms';
SELECT pg_reload_conf();
```

or load `auto_explain` with `auto_explain.log_min_duration` set. LQO reads the
`stderr` log format, `csvlog` and `jsonlog` (PostgreSQL 15 and later), and works
out which from the file. It fills in the parameter values the server logs for
prepared statements (`DETAIL: parameters: $1 = '42'`). With `csvlog` and `jsonlog`
it keeps only statements from the database you connect to, unless you pass
`--all-databases`.

Statements without their values (pg_stat_statements' `$1`) can't be run, so LQO
skips them and says why.

### 3. Run it

```sh
lqo advise --dsn "postgres://lqo@replica.internal:5432/shop" postgresql.log
```

The connection string is a URL or `key=value` pairs, as for `psql`. It can also come
from `LQO_DSN` or the usual `PGHOST`, `PGUSER`, `PGPASSWORD` variables, and
`~/.pgpass` works. TLS follows `sslmode` (`prefer` by default; `require`,
`verify-ca` and `verify-full` with `sslrootcert`).

If your application sets things on its connections, such as `search_path`, give
them with `--set`: `--set search_path=app,public`.

LQO tests the 25 slowest shapes by default (the most frequent, for a SQL file),
three texts of each. `--top`, `--samples` and `--repeat` change that. A run's time
is roughly the time your queries take, times the number of distinct plans the
settings produce, times four.

## Reading the report

Each kind of query is a **shape**: statements that differ only in their values,
such as `WHERE id = 5` and `WHERE id = 7`, share one. LQO prints each shape's ID,
the first 8 characters of a fingerprint of its text with the values taken out.

Shapes land in one of three lists:

- **Faster with a hint.** A setting made every text of the shape faster, and saved
  at least 20% overall. You get the time before and after, the setting's name,
  what changed in the plan, and the `SET LOCAL` lines.
- **Keep the default plan.** No setting passed. The reason says which case it was:
  no setting changed the plan; another plan existed but wasn't clearly faster; or a
  setting helped one text and hurt another.
- **Not tested.** LQO won't run the statement (a write, a statement with no values),
  or the default plan failed or ran past `--max-time`.

The last lines estimate the time one pass of the tested statements takes now and
with the suggested settings. Times are planning plus execution as `EXPLAIN ANALYZE`
reports them, which leaves out sending the rows to a client.

`--report markdown` gives the same report for a pull request or a wiki page, and
`--report json` gives every run of every plan, for your own tooling. `lqo report`
turns a saved JSON report back into text or Markdown.

### Using a suggestion

A suggestion is for that shape only. Don't set it for the whole server: the same
setting that rescues one query can slow down a hundred others. Set it inside the
transaction that runs the query:

```sql
BEGIN;
SET LOCAL enable_nestloop = off;
SELECT sum(l.qty) FROM ... ;
COMMIT;
```

`SET LOCAL` ends with the transaction, so nothing else on the connection is
affected. Most drivers and ORMs can run a statement before a query in the same
transaction. If you'd rather not touch the code, a function with the setting in a
`SET` clause (`CREATE FUNCTION ... SET enable_nestloop = off`) does the same.

Then watch it. Data changes, and a plan that was right in March can be wrong in
June. Run LQO again after big data changes or a PostgreSQL upgrade.

### The state file

`lqo advise` also keeps the latest finding for each shape in `lqo-state.json`
(`--state` to move it, `--no-state` to skip it), so findings from several runs add
up. `lqo show` prints it. The file is replaced whole on each save, through a
temporary file and a rename, so a crash never leaves half a file. If it ever can't
be read, LQO moves it aside and starts a new one.

## How LQO decides

For each shape, and each of its sampled texts, LQO:

1. **Plans** the query with `EXPLAIN` under the default settings and under each
   hint. Hints that produce the same plan are grouped, so each distinct plan is run
   once per round, not once per hint. In the demo, the 13 hints gave between two
   and eight distinct plans per query.
2. **Warms up** each distinct plan with one untimed run. The default plan's warm-up
   sets the time limit for the others: three times the default plan's time (at
   least 250 ms). A plan that hits the limit is stopped and counts as slower.
3. **Times** each plan `--repeat` times (3 by default) with
   `EXPLAIN (ANALYZE, TIMING OFF)`, in an order shuffled each round so no plan
   always runs on a warmer cache.

A hint is suggested for the shape only when, for every sampled text:

- every run returned the same number of rows as every run of the default plan,
- it never hit the time limit,
- its median time was no more than 10% above the default plan's,

and on at least one text its slowest run beat the default plan's fastest run, and
over all the texts it saved at least `--min-gain` (20%) of the time. If several
hints pass, the fastest wins; when two are within 2% of each other, the one that
changes fewer settings wins, and if they change as many, the faster.

It's deliberately strict. A missed suggestion costs nothing; a wrong one would slow
down a query in production. The rule is the same for every shape, and the JSON
report has every run, so you can check any decision.

The settings it tries (`lqo hints` lists them):

| Hint | Settings |
|---|---|
| no nested loop | `enable_nestloop = off` |
| no hash join | `enable_hashjoin = off` |
| no merge join | `enable_mergejoin = off` |
| hash join only | `enable_nestloop = off`, `enable_mergejoin = off` |
| nested loop only | `enable_hashjoin = off`, `enable_mergejoin = off` |
| merge join only | `enable_nestloop = off`, `enable_hashjoin = off` |
| no sequential scan | `enable_seqscan = off` |
| no index scan | `enable_indexscan = off`, `enable_indexonlyscan = off` |
| no bitmap scan | `enable_bitmapscan = off` |
| no nested loop, no sequential scan | `enable_nestloop = off`, `enable_seqscan = off` |
| no memoize | `enable_memoize = off` |
| no parallel workers | `max_parallel_workers_per_gather = 0` |
| join order as written | `join_collapse_limit = 1`, `from_collapse_limit = 1` |

`--hints full` tries instead the 48 combinations of join and scan methods used in
the Bao research paper. It tries more combinations and takes longer.

## Learn from live traffic: lqo serve

`lqo advise` needs a workload you give it. `lqo serve` finds its own: it runs as a
proxy in front of your database, your application connects to it instead, and it
passes every message through unchanged. On the side it times every query, groups
them into shapes, keeps a few real texts of each with their values, and tries the
slowest shapes under the hints on a replica.

```sh
lqo serve --dsn "postgres://db1:5432/shop?sslmode=require" \
          --replica "postgres://lqo@replica1:5432/shop?sslmode=require"
```

Then point the application at LQO, on port 6433 by default, with its usual user
and password. LQO holds no passwords: logins go through to the server. `--dsn`
only needs the server's address and TLS settings; `--replica` needs a login, like
`lqo advise`.

The status page, on `http://127.0.0.1:6434/` by default, shows what it has seen and
learned, and `/status` has the same as JSON. `lqo show` reads it from the state
file afterwards.

```
lqo 0.2.0, shadow mode, up 2h13m
connections: 14 open, 2,210 in all
statements measured: 1,904,227 in 212 shapes
learning: 96 rounds, 288 hint trials, replica busy 31m, 3 proven

891f0819  shop  SELECT sum(l.qty) FROM customers c JOIN orders o ON …
  1,204 calls, 1,288,280 ms in all, median 1,061 ms, p95 1,640 ms
  proven: "no nested loop", about 36% faster on the replica
    SET LOCAL enable_nestloop = off;
```

**How it learns.** Trying all 13 hints on every text would keep the replica busy
for nothing, so LQO tries three at a time, picked by Thompson sampling: each hint's
results so far say how much faster it probably is and how sure that is, LQO draws
a guess from each, and tries the best guesses. Hints that keep winning get tried
more, hints that lose stop being tried, and untried hints still get a turn. A hint
is proven only by the same rule as `lqo advise`, on at least three texts: never
more than 10% slower on any, the same rows, no time-outs, faster than every run of
the default plan at least once, and saving 20% of the time over the texts tried,
with enough certainty.
A bad result on one text is tried again before it counts, because a replica busy
with other work can make one round look bad. A proven hint that fails a later
check loses its standing.

**What it costs.** The learner keeps the replica busy at most a quarter of the time
(`--budget`). The proxy adds a little time to every query, mostly the extra network
hop: [test/results/serve/overhead.txt](test/results/serve/overhead.txt) has the
measurements. Measuring never holds traffic up: if LQO falls behind, it drops
measurements, never queries.

**What it changes: nothing.** 0.2 runs in shadow mode only. Applying proven hints
to live queries is planned for 0.3.

**Connecting.** Plain and prepared queries, pipelining, `COPY`, cancel requests
and notifications pass through. With `--tls-cert` and
`--tls-key`, clients can connect with TLS; LQO ends that TLS and makes its own
connection to the server, with TLS as `--dsn` says. That's what lets it see the
queries, and it means SCRAM channel binding can't work through it: a client that
connects to LQO with TLS needs `channel_binding=disable`. The simplest setup runs
LQO next to the application, over a Unix socket (`--listen /var/run/lqo`) or
localhost, and keeps TLS between LQO and the database.

If the replica goes away, learning pauses and traffic carries on. LQO saves what it
has learned to the state file as it goes; after a crash or a restart it picks up
where it was.

## What LQO will and won't run

LQO runs a statement only if it is a read: it starts with `SELECT`, `WITH`,
`VALUES` or `TABLE`, contains no `INSERT`, `UPDATE`, `DELETE`, `MERGE`, `TRUNCATE`
or `COPY`, isn't `SELECT ... INTO` or `FOR UPDATE`/`FOR SHARE`, and calls none of the
functions that act outside a query, such as `pg_sleep`, `set_config`,
`pg_terminate_backend`, advisory locks, `nextval`, `dblink` or large objects.
Everything else is listed as skipped, with the reason.

That check reads the SQL text, so it can't see inside functions you wrote
yourself: a query that calls one runs it, and whatever the function does inside a
read-only transaction happens. LQO sends each trial's query as a single statement
that the server won't split, refuses a server whose `standard_conforming_strings`
is off, and after every shape checks that no setting and no advisory lock is left
behind. For the most care, connect as a role that can only read.

Then every trial runs like this, and is rolled back:

```sql
BEGIN TRANSACTION READ ONLY;
SET LOCAL statement_timeout = <limit>;
SET LOCAL lock_timeout = 2000;
SET LOCAL enable_nestloop = off;           -- the hint, if any
EXPLAIN (ANALYZE, TIMING OFF, SUMMARY ON, FORMAT JSON) <your query>;
ROLLBACK;
```

`READ ONLY` means that even a statement that got past the check can't write: the
server refuses. `SET LOCAL` means no setting outlives the trial. After each shape
LQO reads the settings back and stops if any differs from before, which the tests
check can't happen. The lock timeout keeps a trial from waiting behind your
application's locks.

## Commands

```
lqo advise [flags] WORKLOAD...   try a workload's queries with each hint and report
lqo serve [flags]                run as a proxy, measure live queries, learn on a replica
lqo show [flags]                 print what the state file holds
lqo report [flags] REPORT.json   print a saved JSON report as text or Markdown
lqo hints [--hints full]         list the hints lqo tries
lqo demo [flags]                 build the lqo_demo schema and its workload file
lqo version                      print the version
```

`lqo advise` flags:

| Flag | Default | What it does |
|---|---|---|
| `--dsn` | `LQO_DSN`, then `PG*` | connection string |
| `--allow-primary` | off | run on a primary, not only a standby |
| `--format` | `auto` | workload format: `auto`, `sql`, `stderr`, `csv`, `json` |
| `--hints` | `standard` | `standard` (13 hints) or `full` (48) |
| `--top` | 25 | how many shapes to test |
| `--samples` | 3 | distinct texts tested per shape |
| `--repeat` | 3 | timed runs per plan, after one warm-up |
| `--min-gain` | 20 | percent of the time a hint must save |
| `--timeout-factor` | 3 | stop a hinted run at this multiple of the default plan's time |
| `--max-time` | `60s` | stop the default plan's runs after this long |
| `--set name=value` | | session setting for every trial, may repeat |
| `--all-databases` | off | keep logged statements from every database |
| `--report` | `text` | `text`, `json` or `markdown` |
| `--out` | stdout | write the report to a file |
| `--state` | `lqo-state.json` | state file |
| `--no-state` | off | don't write the state file |
| `--quiet` | off | no progress lines |

`lqo serve` flags:

| Flag | Default | What it does |
|---|---|---|
| `--dsn` | `LQO_DSN`, then `PG*` | the database server: address and TLS settings |
| `--replica` | none | a replica to learn on, with a login; without it LQO only measures |
| `--allow-primary` | off | let `--replica` be a primary |
| `--listen` | `127.0.0.1` | address to listen on, or a directory for a Unix socket |
| `--port` | 6433 | port to listen on |
| `--tls-cert`, `--tls-key` | none | certificate for clients that connect with TLS |
| `--status` | `127.0.0.1:6434` | status page address, `""` for none |
| `--hints` | `standard` | `standard` (13 hints) or `full` (48) |
| `--min-gain` | 20 | percent of the time a hint must save |
| `--min-ms` | 5 | don't try shapes whose median time is under this |
| `--budget` | 25 | percent of the time the learner may keep the replica busy |
| `--max-time` | `30s` | stop a trial of the default plan after this long |
| `--set name=value` | | session setting for every trial, may repeat |
| `--state` | `lqo-state.json` | state file |
| `--no-state` | off | don't write the state file |
| `--save-every` | `30s` | how often to save while learning |
| `--grace` | `30s` | on shutdown, how long to wait for open connections |
| `--quiet` | off | log only errors |

Exit codes: 0 when the run finished, 1 on an error, 2 for a usage error or a
refusal, such as a primary without `--allow-primary`.

[docs/REFERENCE.md](docs/REFERENCE.md) has the details: workload formats, the
JSON report, the state file and the connection settings.

## What 0.2 doesn't do

- **It doesn't change anything.** You apply the suggestions. `lqo serve` applying
  proven hints to live queries, and backing them out when they stop winning, is
  planned for 0.3.
- **It doesn't learn across shapes yet.** Each shape is tested on its own. A new
  shape needs its own run.
- **Only planner switches.** No join-order hints beyond "as written", and no
  pg_hint_plan. Plans that need a different join order than any switch produces
  are out of reach.
- **PostgreSQL only**, 14 to 18. Earlier versions may work but aren't tested.

## Building and testing

```sh
go build -o lqo ./cmd/lqo
go test ./...
```

The tests that need a server read `LQO_TEST_DSN`, a superuser connection on a test
database, and skip without it. They create the schemas `lqo_test_adv`,
`lqo_test_learn` and `lqo_demo`. Run the packages one at a time, so the timing
tests don't share the server with each other:

```sh
LQO_TEST_DSN="postgres://postgres:secret@localhost/lqo_test" go test -p 1 ./...
```

Among them: writes are never run, even ones that get past the statement check;
settings never outlive a trial; a slower plan is never suggested and a far slower
one is stopped at the time limit; a query whose rows change between runs gets no
suggestion; an interrupted run stops within seconds; the state file stays whole
when the writing process is killed 40 times; and the demo benchmark, where LQO must
find the trap and leave the other shapes alone. For `lqo serve`: results through
the proxy match results straight from the server, for plain and prepared queries,
errors, transactions, `COPY`, notifications, cancel requests, md5 and SCRAM logins
and TLS on both sides; psql and pgbench, pipelining included, work through it;
the learner finds the demo trap from traffic sent through the proxy; losing the
replica pauses learning while queries carry on; and `lqo serve` killed with
SIGKILL 10 times while saving leaves its state file whole and keeps what it
learned.

`sh scripts/record-tests.sh` runs them all and writes the output to
[test/results/](test/results/). The Join Order Benchmark, 113 queries in 94 shapes
over the IMDB data set, runs in the [job workflow](.github/workflows/job.yml) on a
GitHub runner and records its reports in [test/results/job/](test/results/job/). In
the recorded run (PostgreSQL 16, 4 cores, the 13 standard hints), 35 of the 94
shapes got a suggestion and 59 stayed on the default plan. The suggestions saved
21% to 98% per shape, median 48%, and one pass over every tested query went from
172.6 s to 77.6 s (−55%). One query, JOB 26c, accounts for most of that, 59.2 s to
1.2 s without parallel workers; without it the pass goes from 113.4 s to 76.4 s
(−33%). The run used the code released as 0.1.0, built just before the version
number changed, so its report says `0.1.0-dev`.

CI runs every push on Linux against PostgreSQL 14 and 18, and on macOS and Windows.
Releases are automated: see [RELEASING.md](RELEASING.md).

## License

Apache License 2.0. See [LICENSE](LICENSE) and [NOTICE](NOTICE).

LQO's approach, steering PostgreSQL's planner with its own settings rather than
replacing it, follows published research, chiefly
[Bao](https://doi.org/10.1145/3448016.3452838) (Marcus et al., SIGMOD 2021). LQO
contains no code from Bao or any other learned optimizer.
