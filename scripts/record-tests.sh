#!/bin/sh
# Copyright lqo.net 2026
# SPDX-License-Identifier: Apache-2.0
#
# Runs every test, the race detector, the smoke check, the demo benchmark
# and the lqo serve benchmark against a PostgreSQL server, and writes the output to
# test/results/ with a note of the machine it ran on. Every number on
# lqo.net and in the README comes from these files.
#
#   LQO_TEST_DSN=postgres://postgres:secret@localhost/lqo_test sh scripts/record-tests.sh
#
# The tests create the schemas lqo_test_adv and lqo_demo in that database.
# The Join Order Benchmark has its own script, scripts/job.sh.
set -eu
cd "$(dirname "$0")/.."
: "${LQO_TEST_DSN:?set LQO_TEST_DSN to a superuser connection on a test database}"
out=test/results
mkdir -p "$out/demo"
server=$(psql "$LQO_TEST_DSN" -Atc 'SELECT version()' 2>/dev/null || echo "unknown, psql not found")
preload=$(psql "$LQO_TEST_DSN" -Atc "SELECT coalesce(nullif(current_setting('shared_preload_libraries'), ''), 'nothing')" 2>/dev/null || echo "unknown")

header() {
	echo "# $1"
	echo "# recorded $(date -u '+%Y-%m-%d %H:%M UTC') with $(go version | cut -d' ' -f3-)"
	echo "# $(uname -sr), $(grep -m1 'model name' /proc/cpuinfo 2>/dev/null | cut -d: -f2- | sed 's/^ //'), $(nproc 2>/dev/null || echo '?') CPUs"
	echo "# server: $server"
	echo "# shared_preload_libraries: $preload"
	echo "# command: $2"
	echo
}

test -z "$(gofmt -l .)"
go vet ./...

cmd="go test -p 1 -count=1 -v ./..."
{ header "Every test, verbose, against a real server" "$cmd"; $cmd 2>&1; } > "$out/tests.txt"

cmd="go test -p 1 -race -short -count=1 ./..."
{ header "Every test with the race detector (short: no demo benchmark, fewer kills)" "$cmd"; $cmd 2>&1; } > "$out/race.txt"

bin=$(mktemp -d)/lqo
CGO_ENABLED=0 go build -trimpath -ldflags '-s -w' -o "$bin" ./cmd/lqo
cmd="sh scripts/smoke.sh lqo \$LQO_TEST_DSN"
{ header "Smoke check of a release build" "$cmd"; sh scripts/smoke.sh "$bin" "$LQO_TEST_DSN" 2>&1; } > "$out/smoke.txt"

# The demo benchmark: build the demo at scale 1 and run its workload.
wl=$(mktemp -d)/lqo-demo.sql
{ header "Demo database build, scale 1" "lqo demo --scale 1"; "$bin" demo --dsn "$LQO_TEST_DSN" --workload "$wl" 2>&1; } > "$out/demo/build.txt"
"$bin" advise --dsn "$LQO_TEST_DSN" --allow-primary --quiet --no-state --report json --out "$out/demo/report.json" "$wl"
"$bin" report --report text "$out/demo/report.json" > "$out/demo/report.txt"
"$bin" report --report markdown "$out/demo/report.json" > "$out/demo/report.md"
header "Demo benchmark: lqo advise on the demo workload" "lqo advise --allow-primary lqo-demo.sql" > "$out/demo/environment.txt"

# lqo serve: proxy overhead and learning the demo trap from live traffic.
bash scripts/serve-bench.sh "$bin"

grep -E '^(ok|FAIL|---)' "$out/tests.txt" "$out/race.txt" | grep -v -- '--- PASS' || true
tail -n 1 "$out/smoke.txt"
echo "results written to $out/"
