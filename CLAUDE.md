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
serve in shadow mode (0.2) and active mode (0.3) (October 2026). The 0.2 post's
results link points at the v0.2.0 tag, since the demo changed in 0.3. Images are drawn into static/images.

## Next

Agreed with the owner in October 2026: v0.2.0 (shadow mode) and v0.3.0 (active
mode) are both done. Nothing further is agreed; what comes next is decided with
the owner before it goes here.

Ideas mentioned but not agreed: sharing what's learned across similar shapes,
pg_hint_plan join-order hints.
