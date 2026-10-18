# LQO reference

The [README](../README.md) is the manual. This page has the details it leaves out.

1. [Connection strings](#connection-strings)
2. [Workload formats](#workload-formats)
3. [Shapes and fingerprints](#shapes-and-fingerprints)
4. [The decision rule, exactly](#the-decision-rule-exactly)
5. [The JSON report](#the-json-report)
6. [The state file](#the-state-file)
7. [Permissions](#permissions)
8. [Exit codes and signals](#exit-codes-and-signals)

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

On Ctrl-C or SIGTERM, `lqo advise` cancels the running trial on the server, rolls
it back, saves the state, prints the report for the shapes it finished, marked as
interrupted, and exits with code 1.
