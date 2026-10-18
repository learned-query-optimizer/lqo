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

The code is Go with no dependencies: `internal/pgwire` is LQO's own small
PostgreSQL client (simple query protocol, trust/md5/SCRAM, TLS), because pgx and
golang.org/x modules can't be fetched from a cloud session. Keep it
dependency-free unless the owner decides otherwise. The approach follows the Bao
paper; no code from Bao (GPL/AGPL) may be copied in.

Read [README.md](README.md) (the manual) and [docs/REFERENCE.md](docs/REFERENCE.md)
before changing behaviour. The decision rule in the reference is the contract.

## Test results

`sh scripts/record-tests.sh` (with `LQO_TEST_DSN` set to a superuser on a test
database) runs every test, the race detector, the smoke check and the demo
benchmark, and writes them to `test/results/`. In a cloud session, Postgres 16 is
in `/usr/lib/postgresql/16/bin`; run it as the `postgres` user from a directory
that user can reach, such as `/srv`.

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
optimizer is, how LQO was tested, the name. Images are drawn into static/images.

## Next

Agreed with the owner in October 2026, in this order. Each starts only when the
owner says go:

1. **v0.2.0, the proxy in shadow mode (`lqo serve`).** LQO speaks the PostgreSQL
   wire protocol between the app and the database (simple and extended protocol,
   prepared statements, TLS passthrough), records latency per shape, samples slow
   shapes and re-runs them on a replica, and learns per shape with Thompson
   sampling over the hints. A hint is promoted only after a minimum number of
   samples and a clear margin. Shadow mode, the default, learns and reports but
   changes nothing.
2. **v0.3.0, active mode.** Proven hints applied to live queries with SET LOCAL; a
   regression guard per shape that reverts a hint whose live latency slips past
   the baseline; per-shape and global kill switches; fail-safe passthrough when
   the replica or the state file is unavailable.

Tests for both: results identical with LQO in the path, SIGKILL and restart with
the state intact, replica loss, SET LOCAL never leaking, timeouts, pgbench
overhead. Later ideas (sharing what's learned across similar shapes, pg_hint_plan
join-order hints) are decided with the owner before they go here.
