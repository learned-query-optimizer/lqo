# Notes for Claude

This file says where the next session starts and what comes next. What comes
next is decided with the owner before it goes here, and work starts only when
the owner says go.

## Releases

Releases are automated. To release, raise `Version` in `lqo.go` and push to
`main`; never ask the owner to publish one. [RELEASING.md](RELEASING.md) has the
procedure and the checks to run afterwards.

## Where things stand

v0.1.0 is the advisor (October 2026): `lqo advise` reads a SQL file or a
PostgreSQL log, groups statements into shapes, runs each shape under the default
plan and under 13 planner-setting hints (48 with `--hints full`), on a standby or,
with `--allow-primary`, a copy, and reports the hints that are clearly and
consistently faster. It changes nothing on the server: every trial is EXPLAIN
ANALYZE in a READ ONLY transaction with SET LOCAL, rolled back.

v0.2.0 is `lqo serve` in shadow mode (October 2026): a PostgreSQL proxy
(`internal/proxy`, port 6433, status page on 6434) that relays the wire protocol
unchanged, measures every statement per shape (`internal/watch`), and with
`--replica` learns per shape on the replica with Thompson sampling over the hints
(`internal/learn`), proving a hint only under the advise rule. TLS from clients is
ended at the proxy (it must see the queries), so SCRAM channel binding can't pass
through; the proxy strips -PLUS and explains `channel_binding=disable`.

v0.3.0 adds active mode (October 2026): `lqo serve --mode active` applies proven
hints to live queries with `set_config(..., true)` (SET LOCAL) plus
`plan_cache_mode = force_custom_plan`, only to a statement that is its own implicit
transaction (session idle, nothing in flight, the whole batch before Sync), and
takes LQO's own replies out of the stream (`internal/watch/session.go`). A guard
compares hinted calls with held-out ones (`--holdout`, 10%) and takes a hint back
when it runs over 10% slower live; `lqo switch` and the status page's POST routes
turn hints off and on; nothing is applied while the replica or the state file is
unavailable. Shadow mode stays the default.

v0.4.0 adds shared learning (October 2026): shapes with the same tables in their
FROM and JOIN clauses (`sqltext.Family`) are relatives; a shape's Thompson draws
start from a prior pooled from its relatives (at most 2 texts' worth, fading with
its own texts), and when a relative proved a hint, the shape times that hint
alone each round while it's a clear win on every own text (focus). Proof stays
per shape. `--share=false` turns it off. `test/results/serve/shared.txt`
compares three new relatives with and without it.

v0.5.0 adds row-count hints with pg_hint_plan (October 2026), optional
(`--join-hints auto|on|off`): from the default plan's EXPLAIN ANALYZE, joins whose
estimate was over 2x off get `Rows(aliases *factor)` hints (`plan.Corrections`),
tried as the arms "row counts" and "row counts, second pass", built from a
shape's first text and kept for its others. Active mode puts a proven comment in
front of a simple Query or into an unnamed one-batch Parse, and checks per
session that pg_hint_plan is loaded. The local test server preloads pg_hint_plan
(`shared_preload_libraries`, built from source into Postgres 16, which needs
postgresql-server-dev-16); the `pg-hint-plan` CI job and the JOB workflow
install it from apt. The JOB run with it writes to `test/results/job-join-hints/`.

The code is Go with no dependencies: `internal/pgwire` is LQO's own small
PostgreSQL client (simple query protocol, trust/md5/SCRAM, TLS), because pgx and
golang.org/x modules can't be fetched from a cloud session. Keep it
dependency-free unless the owner decides otherwise. The approach follows the Bao
paper; no code from Bao (GPL/AGPL) may be copied in.

Read [README.md](README.md) (the manual) and [docs/REFERENCE.md](docs/REFERENCE.md)
before changing behaviour. The decision rule in the reference is the contract.

## Test results

`sh scripts/record-tests.sh` (with `LQO_TEST_DSN` set to a superuser on a test
database, and psql, pgbench and curl on the PATH) runs every test, the race
detector, the smoke check, the demo benchmark and `scripts/serve-bench.sh` (proxy
overhead with pgbench, and lqo serve learning the demo trap), and writes them to
`test/results/`. Test packages run one at a time (`go test -p 1`), so timing
tests don't share the server. Don't edit the code while it records. In a cloud
session, Postgres 16 is in `/usr/lib/postgresql/16/bin`; run it as the `postgres`
user from a directory that user can reach, such as `/srv`.

The Join Order Benchmark needs the IMDB files from event.cwi.nl, which a cloud
session can't reach, so it runs in `.github/workflows/job.yml` on a GitHub runner
(on changes to `scripts/job.sh` or the workflow, or by hand) and commits its
reports to `test/results/job/`.

Every number on the site and in the README comes from those files; record again
after any change that could move them, and update the site too.

## Site

lqo.net is in `photozeitgeist/lqo.net`: the Hugo Launch template copied unchanged
from ukue.com, content and config values only. The old personal blog was removed
from the repo and its history at the owner's request (October 2026). Pages: about,
contact, quick start, download, reference. Posts: the launch, what a learned query
optimizer is, how LQO was tested, the name, the Join Order Benchmark results, lqo
serve in shadow mode (0.2) and active mode (0.3), four case studies tagged "case
studies", built from the recorded runs with no customers, shared learning (0.4)
and row-count hints (0.5) (October 2026). Posts about a past release keep that
release's numbers and link to its tag's results (0.2, 0.3 with the case
studies, 0.4); the pages and the other posts follow the latest recording. Images are drawn into static/images.

## In progress: 0.7.0 Phase B, pg_stat_statements as a workload source

Approved by the owner on 2026-11-02 ("Go"), item 5 Phase B of softdev-plan.md
in photozeitgeist/jollyroger.biz.

What and why: `lqo advise` needs a SQL file or a server log, and on managed
Postgres the log is often hard to get or doesn't carry durations. Most of those
servers have pg_stat_statements, which already ranks every query shape by total
time. Reading it is the easiest way in. Its texts are normalised, with $n in
place of every value, so LQO has to find values to run them with.

Design decisions:
- `lqo advise --stats` reads pg_stat_statements on the --dsn server;
  `--stats-dsn URL` reads it on another (the primary, when trials run on a
  standby: a standby's view only has the standby's own queries). Workload
  files become optional with --stats; given both, they're merged.
- The read: the connected database only, top-level statements (1.9+, PG14),
  total_exec_time (total_time before 1.8), the 500 entries with the most total
  time. Kept: what ClassifyTemplate accepts; the rest is counted in the
  workload line (not reads, hidden by <insufficient privilege>, LQO's own).
  The extension missing gives one clear error (CREATE EXTENSION, preload).
- Ranking: a shape's calls and time come from pg_stat_statements when it's
  there (complete totals), else from the logs, never summed across both.
- Values for $n, per parameter, from the text around it:
  - compared with an expression over one table (`a.col = $1`, `$1 < a.col`,
    `a.attrs->>'tier' = $1`, `IN ($1, $2)`, `= ANY($1)`, LIKE/ILIKE,
    BETWEEN): rows sampled from that table (TABLESAMPLE SYSTEM sized for
    about 2000 rows, plain LIMIT for views or when the sample is empty), in a
    READ ONLY transaction that's rolled back, with a statement timeout.
    Parameters on the same table come from the same rows, so the values go
    together as they do in the data; the most frequent combinations first,
    up to --samples distinct texts. Range comparisons take quantiles of the
    sample (BETWEEN the 25th and 75th percentile, one-sided the median).
    Arrays get an untyped '{...}' literal.
  - LIMIT and FETCH get 100, OFFSET 0.
  - Anything else (a parameter in the select list, a function argument
    alone, interval arithmetic): the shape is skipped with the reason.
  Values are written as untyped quoted literals, so the column's type
  decides. Texts from a log with the same shape come first; filled ones are
  marked as such in the report's source.
- Version 0.7.0. No change to lqo serve.
- Done when: a recorded run (test/results/stats/) sends the demo workload
  with the extended protocol, then `lqo advise --stats` with no file proves
  the same hint for the trap as the SQL file does; and the JOB workflow,
  with pg_stat_statements loaded, runs the 113 queries once and advises
  from the view alone, recorded next to the file run of the same job.

Steps:
1. [ ] workload: pg_stat_statements reader (version-aware), Group merging
       stats totals; unit tests
2. [ ] parameter values: comparison finder, aliases, row sampling,
       quantiles, LIMIT/OFFSET; unit and real-server tests; CI preloads
       pg_stat_statements
3. [ ] CLI `--stats`, `--stats-dsn`; README, docs/REFERENCE.md
4. [ ] recorded demo run; JOB workflow stats run
5. [ ] independent review, fixes, record-tests.sh
6. [ ] release 0.7.0; lqo.net post; softdevstudio note; softdev-plan.md

## Done: 0.6.0 Phase A, hints inside app transactions (released 2026-11-02)

Approved by the owner on 2026-11-02 ("Go"), item 5 Phase A of softdev-plan.md
in photozeitgeist/jollyroger.biz. Phase B (`pg_stat_statements` as a workload
source) waits for his go.

What and why: active mode applies a hint only to a statement that is its own
implicit transaction, so it skips everything inside an app's explicit
transaction, which is most ORM traffic. A SET LOCAL there would last to the
end of the app's transaction and reach its later statements.

Design decisions:
- Inside the app's transaction (ReadyForQuery said 'T'), the hint is set for
  one statement and taken back right after it. Before the statement, one
  SELECT of LQO's saves each setting it changes (plan_cache_mode included)
  into a placeholder `lqo.saved_<name>` with set_config(..., true), then sets
  the hint; after the statement, one SELECT sets each setting back from its
  placeholder. Everything stays server-side, so no reply has to arrive before
  LQO can send the restore, and it's SET LOCAL throughout: whatever happens,
  the transaction's end undoes it too.
- Same conditions as outside, plus: nothing in flight (so the transaction
  status is current), the statement is the whole of what the client sent up
  to its Sync (extended) or the only statement in its Query (simple). A
  failed transaction ('E') gets nothing.
- Simple protocol: `<comment><save+set>; <text>\n; <restore>` in one Query;
  the replies to LQO's two SELECTs are taken out. Extended: the save+set
  statement goes before the Bind (as now), the restore goes in before the
  client's Sync.
- If the client's statement fails, the server skips the restore along with
  the rest of the batch; the transaction is aborted, and ROLLBACK or ROLLBACK
  TO SAVEPOINT undoes LQO's settings with the rest (the savepoint is always
  older than LQO's set). Tested.
- Pipelined statements (sent while others are in flight) still get no hint;
  tests check that pipelining through a hinted session stays correct.
- Known side effect, documented: after a hinted statement in a transaction,
  `current_setting('lqo.saved_<name>', true)` is '' instead of NULL in that
  session (a placeholder exists once set).
- Review (independent agent, two rounds): DISCARD ALL / DEALLOCATE ALL left
  LQO's prepared statement missing (now closed and prepared again on every
  call, named lqo_ + a random part per process, portal closed too, so an
  application's lqo_hint cursor can't clash); hinted calls' plan costs
  tilted a prepared statement's later unhinted calls to the generic plan
  (now held-out calls, and calls of a statement LQO hinted while hints are
  live, get force_custom_plan alone, keeping an app's force_generic_plan;
  pipelined calls are a documented limit). Tests: review_060_test.go,
  review_060b_test.go, watch/fresh_test.go.
- Done when: a JOB replay through an ORM-style transaction wrapper (BEGIN,
  a statement or two, COMMIT, extended protocol) shows applied hints and the
  speedup, recorded in test/results.
- Upkeep: README contents link (#what-03-doesnt-do), headline JOB numbers
  from the newest run, workflows on stable Go 1.26 (go.mod stays 1.24: the
  sandbox has only Go 1.24.7).

Steps:
1. [x] upkeep: README link, workflows (JOB numbers with step 4)
2. [x] session.go: in-transaction hints with save and restore; unit tests
3. [x] real-server tests: simple and extended in a transaction, savepoints,
       errors and rollback, pipelining, nothing leaks
4. [x] JOB replay in transactions (scripts/jobtx, job.sh JOB_TX, workflow), recorded
       (run 41386316126 on 0.6.0 code: advise proved 44 of 94 shapes,
       152.3 s -> 62.5 s; replay 153 of 153 calls hinted, 0 leaks, a pass
       152.8 s off -> 63.7 s on, proven shapes 117.6 s -> 28.9 s. Advise and
       replay must share one data load: ANALYZE samples differ per load)
5. [x] README, docs/REFERENCE.md, independent review (two rounds), fixes
5b. [x] record-tests.sh (2026-11-02: 131 tests passed; this sandbox has no
       pg_hint_plan, so the demo report ran without row-count hints; CI's
       pg-hint-plan job covers them)
6. [x] release 0.6.0 (tag on 6fcb35d; assets and Linux binary checked);
       lqo.net post; softdevstudio note; softdev-plan.md

## Next

Agreed with the owner in October 2026: v0.2.0 (shadow mode) and v0.3.0 (active
mode) are done. On October 19 the owner said go for both of these, in this order:

1. **v0.4.0, shared learning.** Done, October 2026.
2. **v0.5.0, pg_hint_plan.** Done, October 2026.

Both done. 0.6.0 (item 5 Phase A of softdev-plan.md) is done too. Phase B,
`pg_stat_statements` as a workload source, is in progress (0.7.0, above).

Later ideas are decided with the owner before they go here.
