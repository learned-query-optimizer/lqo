# LQO reference

The [README](../README.md) is the manual. This page has the details it leaves out.

1. [Connection strings](#connection-strings)
2. [Workload formats](#workload-formats)
3. [Shapes and fingerprints](#shapes-and-fingerprints)
4. [The decision rule, exactly](#the-decision-rule-exactly)
5. [The JSON report](#the-json-report)
6. [The state file](#the-state-file)
7. [lqo serve](#lqo-serve)
8. [Permissions](#permissions)
9. [Exit codes and signals](#exit-codes-and-signals)

## Connection strings

LQO has its own small PostgreSQL client, so it needs no libpq. It accepts:

- **URLs:** `postgres://user:password@host:5432/dbname?sslmode=require`. Characters
  such as `@` in a password are percent-encoded (`%40`). The query string takes
  `sslmode`, `sslrootcert`, `sslcert`, `sslkey`, `application_name`,
  `connect_timeout`, `options`, `passfile`, `host`, `port`, `user`, `password` and
  `dbname`. A socket directory can be the host, percent-encoded:
  `postgres://app@%2Fvar%2Frun%2Fpostgresql/shop`.
- **Keywords:** `host=replica port=5432 user=lqo dbname=shop`. Values with spaces
  go in single quotes; `\'` and `\\` escape inside them. `hostaddr`, `gssencmode`,
  `target_session_attrs` and `client_encoding` are accepted and ignored.

Anything the string leaves out comes from `PGHOST`, `PGPORT`, `PGUSER`,
`PGPASSWORD`, `PGDATABASE`, `PGSSLMODE`, `PGSSLROOTCERT`, `PGSSLCERT`, `PGSSLKEY`,
`PGAPPNAME`, `PGCONNECT_TIMEOUT`, `PGOPTIONS` and `PGPASSFILE`, then from defaults:
the Unix socket in `/var/run/postgresql` or `/tmp` if one exists, otherwise
`localhost`; port 5432; your operating system user name; a database named after
the user. A host that starts with `/` is a socket directory.

Passwords come from the string, `PGPASSWORD`, or the password file (`~/.pgpass`,
or `passfile`), which is read only when no one but its owner can read it, as libpq
does.

Login methods: trust, password, md5 and SCRAM-SHA-256 (without channel binding).
SCRAM passwords are sent as written, without the SASLprep normalisation libpq
applies, which only matters for passwords with unusual Unicode characters.
GSSAPI, SSPI and certificate-only logins aren't supported in 0.1; a client
certificate can still be sent with `sslcert` and `sslkey`.

TLS follows `sslmode`:

| sslmode | What LQO does |
|---|---|
| `disable` | plain connection |
| `allow` | plain connection (LQO doesn't fall back to TLS) |
| `prefer` | TLS if the server offers it, without checking the certificate |
| `require` | TLS or fail, without checking the certificate; with `sslrootcert` set, as `verify-ca` |
| `verify-ca` | TLS, certificate chain checked against `sslrootcert` (or `~/.postgresql/root.crt`, or `system`) |
| `verify-full` | as `verify-ca`, and the host name must match the certificate |

One difference from libpq: Go checks host names against a certificate's subject
alternative names only, not its common name. Certificates from managed services
have SANs; a home-made one may need `-addext "subjectAltName=DNS:yourhost"`.

The connection reports itself as `application_name=lqo`, so it is easy to spot in
`pg_stat_activity`.

## Workload formats

`--format auto` picks the format from the file name (`.sql`, `.csv`, `.json`,
`.jsonl`), then from the first 64 KB of the file.

**sql.** Statements separated by semicolons outside strings, quoted identifiers,
dollar quotes and comments. Comments above a statement are dropped from its
example text.

**stderr.** The server's plain log. LQO finds where each message starts by its
severity (`LOG:  `, `DETAIL:  ` and so on), whatever `log_line_prefix` puts in
front, and joins the tab-indented lines that continue a message. It reads:

| Message | From |
|---|---|
| `duration: 12.3 ms  statement: SELECT ...` | `log_min_duration_statement` |
| `duration: 12.3 ms  execute <name>: SELECT ... $1` and its `DETAIL: parameters: $1 = '...'` | the same, for prepared statements |
| `statement: SELECT ...` | `log_statement = 'all'` (no durations) |
| `duration: 12.3 ms  plan:` with `Query Text:`, text or JSON | `auto_explain` |

The `parse` and `bind` steps of a prepared statement aren't counted, so each
execution is counted once. Statements that failed aren't read. A stderr log
doesn't say which database a statement ran in, so all of them are kept.

**csv.** The server's `csvlog`. Same messages, plus the database name (column 3)
and, from PostgreSQL 14, the query ID (column 26).

**json.** The server's `jsonlog` (PostgreSQL 15 and later). Same messages, with
`dbname` and `query_id`.

## Shapes and fingerprints

A shape's fingerprint is the first 16 hex digits of the SHA-256 of its normalised
text: comments and spacing removed, keywords lower-cased, every literal and `$n`
replaced by `?`, and lists of values in `IN (...)`, `ANY(...)` and `ARRAY[...]`
reduced to one `?...`. Quoted identifiers keep their case. The ID in reports is
the first 8 digits.

On PostgreSQL 14 and later LQO also records the server's own query ID for each
tested shape when it can switch `compute_query_id` on (that takes a superuser), so
you can match shapes with `pg_stat_statements`.

Within a shape, LQO tests up to `--samples` distinct texts: the slowest first when
the log has durations, then the most frequent.

## The decision rule, exactly

For one shape, with texts *s₁ … sₙ* and hint *h*:

- Each distinct plan, the default included, gets one warm-up run, then `--repeat`
  timed runs. A run's time is `Planning Time + Execution Time` from
  `EXPLAIN (ANALYZE, TIMING OFF, SUMMARY ON)`.
- The default plan's runs stop at `--max-time`. If that happens on the first text,
  the shape is reported as *too slow*; on a later text, no hint is suggested.
- A hinted plan's runs stop at `--timeout-factor` × the default plan's warm-up
  time, but no sooner than 250 ms and no later than `--max-time`. A hinted plan that
  hits the limit is out, for that shape.
- The plan of every timed run must match the plan grouped at the planning step. If
  statistics changed in between and it doesn't, that plan is out.

*h* is suggested when all of these hold:

1. On every text, *h*'s plan ran, every one of its runs returned the same number
   of rows as every run of the default plan, and its median time was at most 110%
   of the default plan's median. If the default plan's own runs disagree on the
   rows, no hint is suggested for the shape.
2. On at least one text, *h*'s plan differs from the default, its slowest run was
   faster than the default plan's fastest, and its median saved at least
   `--min-gain`.
3. Summed over the texts, *h*'s medians are at most (1 − `--min-gain`) × the
   default plan's.

Among hints that pass, the one with the lowest summed median wins, unless another
is within 2% of it and changes fewer settings; then that one wins. Between hints
that change as many settings, the lower sum wins. A text that fails to run after
the first one rules out every hint for the shape, and isn't counted in its times.
Hints that ran exactly the same plans as the winner are listed in the JSON report
as `same_plan_as`.

## The JSON report

`--report json` writes one object. The main fields:

| Field | What it holds |
|---|---|
| `lqo_version`, `started`, `finished`, `interrupted` | the run |
| `server`, `server_version_num`, `database`, `target`, `standby` | where it ran |
| `hint_set`, `arms`, `dropped_arms`, `repeat`, `samples_per_shape`, `min_gain`, `timeout_factor`, `max_time_ms` | the settings |
| `workload`, `statements`, `statements_in_other_databases`, `shapes_total` | what it read |
| `shapes[]` | one entry per shape, in the order tested |

Each shape:

| Field | What it holds |
|---|---|
| `id`, `query_id`, `normalized`, `example` | which shape |
| `calls`, `logged_ms` | how often it ran in the workload, and its total logged time |
| `verdict` | `faster`, `keep`, `skipped`, `error` or `too slow` |
| `reason` | why, for anything but `faster` |
| `baseline_ms` | the default plan's medians, summed over the texts tested |
| `best` | for `faster`: `name`, `settings`, `set_local` lines, `ms` (summed medians), `gain`, `plan_change` |
| `distinct_plans` | the most distinct plans any one text had |
| `notes` | anything else, such as a hint ruled out for different rows |
| `samples[]` | per text: `baseline_runs`, `rows`, `baseline_plan` (its joins and scans), and `arms[]` with each hint's `plan`, `runs`, `ms`, `timed_out` and `error` |

Times are in milliseconds.

## The state file

`lqo-state.json` holds the latest finding per shape, keyed by fingerprint:

```json
{
  "format": 1,
  "updated": "2026-10-18T21:30:00Z",
  "shapes": {
    "891f0819ce6bf4ec": {
      "id": "891f0819ce6bf4ec",
      "normalized": "select sum ( l . qty ) from ...",
      "example": "SELECT sum(l.qty) FROM ...",
      "database": "shop",
      "server_version_num": 160015,
      "tested": "2026-10-18T21:30:00Z",
      "verdict": "faster",
      "baseline_ms": 1054.2,
      "hint": "no nested loop",
      "settings": [{"name": "enable_nestloop", "value": "off"}],
      "hint_ms": 554.1,
      "gain": 0.47,
      "plan_change": "Nested Loop ×2 → Hash Join ×2"
    }
  }
}
```

Only `faster` and `keep` findings are stored. Times are per text, averaged over the
texts tested. A save writes a temporary file in the same folder, flushes it to
disk, renames it over the old file and flushes the folder, so a crash leaves the
old file or the new one, whole. Temporary files a killed save left behind are
deleted by the next save once they are ten minutes old. A file that can't be read
is renamed to `lqo-state.json.damaged-<time>` and a new one is started.

## lqo serve

### What passes through

LQO relays the PostgreSQL wire protocol one whole message at a time and changes
nothing in it, with two exceptions during login, both described below. It handles:

- the startup message, `SSLRequest` (TLS ended at LQO when it has a certificate,
  refused with `N` when it hasn't), direct TLS as libpq 17's
  `sslnegotiation=direct` sends it (ALPN `postgresql` required), and
  `GSSENCRequest` (always refused, so the client carries on without it);
- logins: trust, password, md5 and SCRAM-SHA-256 pass through to the server;
- simple and extended queries, pipelined or not, `COPY` both ways, notifications,
  notices, and anything else after login;
- cancel requests, forwarded to the server: the key in them is the one the server
  gave the client, which LQO passed on unchanged.

The two changes: LQO removes `SCRAM-SHA-256-PLUS` from the server's list of login
methods, because channel binding ties a login to one TLS connection and LQO makes
two. And when a client that connected to LQO with TLS says it supports channel
binding (`y` in its first SCRAM message) while LQO's own connection to the server
uses TLS, LQO ends the login with a message saying to use
`channel_binding=disable`, because the server would take that for a downgrade
attack and refuse it with a less helpful error.

A startup packet over 10,000 bytes or a message over about 1 GB ends the
connection. A client gets 60 seconds to log in.

### What is measured

For each statement LQO notes when the request went to the server and when its
reply finished: `CommandComplete`, `EmptyQueryResponse`, `PortalSuspended` or an
error for an extended-protocol `Execute`; `ReadyForQuery` for a simple `Query`,
which counts once however many statements it holds. A pipelined request is timed
from when the one before it finished, so it isn't charged for waiting in line.

Statements are grouped into shapes by the same fingerprint as `lqo advise`, per
database. For each shape LQO keeps the call count, total time, errors, a
histogram for the median and 95th percentile, and up to 8 complete texts, the
slowest seen. A prepared statement's text gets its parameters' values written
in as literals: text-format values quoted, with a cast when the type is known;
binary values for the common types (booleans, integers, floats, text, bytea,
uuid, date, timestamp, json, jsonb) decoded; a binary value of any other type
means that call gives no text. A shape that isn't a plain read is measured and
never tried, for the same reasons as in `lqo advise`.

Measuring never makes traffic wait. Sessions hand their reports to the collector
over a buffer of 8,192; when it's full, reports are dropped and counted
(`watch.dropped` in the status JSON). LQO tracks at most 5,000 shapes, dropping
the tenth with the least total time when it needs room.

### How learning works

Each round the learner picks one shape: the most total live time, discounted by
how often it has been tried. Shapes under `--min-ms` at the median or seen fewer
than 3 times are left alone. It takes one of the shape's texts in turn and runs
it on the replica under the default plan and under three hints, exactly as
`lqo advise` runs one sample: plan each, a warm-up run, then three timed rounds in
shuffled order, each run in its own `READ ONLY` transaction that is rolled back,
and a check afterwards that nothing was left behind.

The three hints are chosen by Thompson sampling. For each hint, the evidence is
the log of its median time over the default plan's, one value per text. LQO draws
a plausible average from each hint's evidence (a normal distribution around the
mean, with the standard error as its width, never narrower than 0.05 over the
square root of the count; a wide guess around no change for a hint never tried)
and tries the three lowest draws.

What a result does to a hint:

| Result | Effect |
|---|---|
| an error | ruled out |
| a different number of rows, or rows that vary between runs | ruled out |
| more than 10% slower, or the time limit hit | tried again on the same text next round; ruled out if it happens again |
| the same plan as the default | counted as no change; ruled out after three texts of only that |
| otherwise | its ratio is added to the evidence |

A hint is proven when it isn't ruled out or waiting for a second look, has at
least 3 results, beat every run of the default plan with its slowest run on at
least one text, has an average ratio at most 1 − `--min-gain`, has no result over
1 + tolerance, and the upper end of its 95% one-sided interval is at most
1 − half of `--min-gain`. Between proven hints the faster wins; within 2%, the
one with fewer settings. A shape whose hints are all ruled out, or all
confidently save less than `--min-gain`, is marked `keep` and tried again a day
later. A proven hint keeps being checked on new texts now and then, and loses its
standing if a check rules it out.

After each round the learner waits so the replica is busy at most `--budget` of
the time. If the replica can't be reached, learning pauses, retries after 1
second, then 2, up to a minute, and traffic is untouched.

### The status page

`GET /` is a plain-text summary; `GET /status` is JSON:

| Field | What it holds |
|---|---|
| `lqo_version`, `mode`, `started` | the running server; `mode` is `shadow` in 0.2 |
| `connections_open`, `connections_total` | client connections |
| `watch` | `statements` measured, `dropped` measurements, `shapes` |
| `learning` | `rounds`, `trials`, `busy_ms`, `proven`, and `replica_down` with `last_error` while the replica can't be reached; absent without `--replica` |
| `shapes[]` | each shape as in the state file below |

### In the state file

`lqo serve` keeps what it learns under `serve` in the same state file, keyed by
database and shape ID, and replaces only that part when it saves, so findings
from `lqo advise` in the same file stay:

```json
"serve": {
  "shop/891f0819ce6bf4ec": {
    "key": "shop/891f0819ce6bf4ec",
    "id": "891f0819ce6bf4ec",
    "database": "shop",
    "normalized": "select sum ( l . qty ) from ...",
    "example": "SELECT sum(l.qty) FROM ... WHERE c.attrs->>'tier' = 'gold' ...",
    "calls": 1204, "total_ms": 1288280, "p50_ms": 1061, "p95_ms": 1640,
    "status": "proven", "rounds": 7, "texts_tried": 7,
    "proven": "no nested loop",
    "settings": [{"name": "enable_nestloop", "value": "off"}],
    "gain": 0.36,
    "arms": {"no nested loop": {"n": 4, "mean_log_ratio": -0.45, "strict_wins": 3, ...}}
  }
}
```

`status` is `learning`, `proven`, `keep`, `skipped` (not a plain read) or `error`
(the default plan failed on three texts in a row). `example` holds a real text
with its values, as the application sent it; keep the state file where you'd keep
your logs. It's saved every `--save-every` and on shutdown, the same atomic way as
always.

### Signals

On Ctrl-C or SIGTERM, `lqo serve` stops accepting connections, waits up to
`--grace` for open ones to end, closes the rest, stops learning, saves the state
and exits.

## Permissions

LQO needs to connect, read `pg_settings`, and run `EXPLAIN ANALYZE` on the
workload's queries, so the role needs `SELECT` on the tables they read. A
read-only role is enough, and is the better choice: LQO checks the SQL it runs,
but a role that can't write is a second guard that doesn't depend on that check.
Switching `compute_query_id` on needs a superuser; without one, shapes have no
query ID and nothing else changes.

`lqo demo` needs to create a schema and tables.

## Exit codes and signals

| Code | When |
|---|---|
| 0 | the run finished (whatever it found) |
| 1 | an error: a file it couldn't read, a connection that failed, a lost connection |
| 2 | a usage error, or a refusal such as a primary without `--allow-primary` |

`lqo serve` exits with 0 after a clean shutdown, and with 1 when it can't listen,
or when learning had stopped because a trial didn't stay inside its transaction
(which should never happen; the proxy keeps serving in that case, and only
learning stops).

On Ctrl-C or SIGTERM, `lqo advise` cancels the running trial on the server, rolls
it back, saves the state, prints the report for the shapes it finished, marked as
interrupted, and exits with code 1.
