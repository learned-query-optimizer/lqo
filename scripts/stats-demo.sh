#!/bin/sh
# Copyright lqo.net 2026
# SPDX-License-Identifier: Apache-2.0
#
# The demo workload through pg_stat_statements: runs the demo's queries three
# times so the view counts them, then lqo advise --stats with no file, and
# compares its suggestions with the file run in test/results/demo. Needs the
# demo built (record-tests.sh builds it first) and pg_stat_statements loaded.
#
#   sh scripts/stats-demo.sh LQO_BINARY WORKLOAD_FILE
set -eu
cd "$(dirname "$0")/.."
bin=$1
wl=$2
: "${LQO_TEST_DSN:?set LQO_TEST_DSN}"
out=test/results/stats
mkdir -p "$out"

if ! psql "$LQO_TEST_DSN" -qc "CREATE EXTENSION IF NOT EXISTS pg_stat_statements" -c "SELECT pg_stat_statements_reset()" >/dev/null 2>&1; then
	echo "pg_stat_statements isn't loaded on this server: no stats run recorded" | tee "$out/skipped.txt"
	exit 0
fi
rm -f "$out/skipped.txt"
for i in 1 2 3; do
	psql "$LQO_TEST_DSN" -q -f "$wl" >/dev/null 2>&1 || true
done

"$bin" advise --dsn "$LQO_TEST_DSN" --allow-primary --stats --no-state --report json --out "$out/report.json" 2>"$out/progress.txt"
"$bin" report --report text "$out/report.json" > "$out/report.txt"
"$bin" report --report markdown "$out/report.json" > "$out/report.md"
go run ./scripts/compare test/results/demo/report.json "$out/report.json" > "$out/compare.md"
{
	echo "# Demo workload through pg_stat_statements"
	echo "# recorded $(date -u '+%Y-%m-%d %H:%M UTC'), $("$bin" version)"
	echo "# server: $(psql "$LQO_TEST_DSN" -Atc 'SELECT version()')"
	echo "# pg_stat_statements $(psql "$LQO_TEST_DSN" -Atc "SELECT extversion FROM pg_extension WHERE extname = 'pg_stat_statements'")"
	echo "# the demo workload run 3 times with psql, then:"
	echo "# command: lqo advise --allow-primary --stats (no workload file)"
} > "$out/environment.txt"
tail -n 2 "$out/compare.md"
