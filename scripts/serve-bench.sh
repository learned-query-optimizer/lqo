#!/bin/bash
# Copyright lqo.net 2026
# SPDX-License-Identifier: Apache-2.0
#
# Measures lqo serve against a PostgreSQL server and writes the results to
# test/results/serve/:
#
#   overhead.txt  pgbench through LQO against pgbench straight to the server
#   demo.txt      lqo serve learning the demo's trap from live traffic, then
#                 applying it in active mode
#   demo.json     the status page's JSON at the end of that run
#
# It needs the demo schema (lqo demo, or scripts/record-tests.sh, builds it),
# psql and pgbench, and a release build of lqo as its first argument.
#
#   LQO_TEST_DSN=postgres://postgres:secret@localhost/lqo_test bash scripts/serve-bench.sh ./lqo
set -euo pipefail
cd "$(dirname "$0")/.."
bin=$1
: "${LQO_TEST_DSN:?set LQO_TEST_DSN to a superuser connection on a test database}"
out=test/results/serve
mkdir -p "$out"
work=$(mktemp -d)
port=16433
status=127.0.0.1:16434
via=$(echo "$LQO_TEST_DSN" | sed -E "s#@[^/]+/#@127.0.0.1:$port/#")
server=$(psql "$LQO_TEST_DSN" -Atc 'SELECT version()')
duration=${BENCH_SECONDS:-20}
clients=${BENCH_CLIENTS:-8}

machine() {
	echo "# recorded $(date -u '+%Y-%m-%d %H:%M UTC'), lqo $("$bin" version | cut -d' ' -f2)"
	echo "# $(uname -sr), $(grep -m1 'model name' /proc/cpuinfo | cut -d: -f2- | sed 's/^ //'), $(nproc) CPUs"
	echo "# server: $server"
	echo "# LQO, pgbench and PostgreSQL share this machine, so every one of them"
	echo "# takes CPU from the others; on separate machines the cost is lower."
}

start_serve() {
	"$bin" serve --dsn "$LQO_TEST_DSN" --listen 127.0.0.1 --port $port --status $status \
		--state "$work/state.json" --grace 2s "$@" 2> "$work/serve.log" &
	serve_pid=$!
	for _ in $(seq 1 100); do
		grep -q "listening on" "$work/serve.log" 2>/dev/null && return
		sleep 0.1
	done
	cat "$work/serve.log" >&2
	exit 1
}

stop_serve() {
	kill -INT $serve_pid
	wait $serve_pid || true
}

pgb() {
	pgbench -n -S -c "$clients" -j 2 -T "$duration" -M "$1" "$2" 2>&1 |
		awk '/latency average/ {lat=$4} /^tps/ {tps=$3} END {printf "%s tps, %s ms average latency", tps, lat}'
}

if [ -z "${BENCH_SKIP_OVERHEAD:-}" ]; then
echo "== overhead"
pgbench -i -s 10 -q "$LQO_TEST_DSN" > /dev/null 2>&1
start_serve
{
	echo "# lqo serve overhead: pgbench -S (one indexed SELECT per transaction),"
	echo "# $clients clients for ${duration}s each, measuring only (no --replica)"
	machine
	echo
	for mode in simple extended prepared; do
		direct=$(pgb $mode "$LQO_TEST_DSN")
		proxied=$(pgb $mode "$via")
		echo "$mode protocol"
		echo "  straight to the server: $direct"
		echo "  through lqo serve:      $proxied"
		echo "$direct" > "$work/direct-$mode"
	done
	echo
	curl -s "http://$status/status" | grep -E '"(statements|dropped|connections_total)"' | sed 's/^ */  /'
} > "$out/overhead.txt"
# The shape pgbench ran, to give it a hint for the active-mode run.
curl -s "http://$status/status" | python3 -c '
import json, sys
d = json.load(sys.stdin)
for s in d["shapes"]:
    if "pgbench_accounts" in s["normalized"]:
        print(s["key"]); break' > "$work/pgbench-key"
stop_serve

# Active mode, with every one of pgbench's SELECTs hinted. The hint is
# "no memoize", which doesn't change this plan, so what's measured is what
# applying a hint costs. No held-out calls, so nothing is compared.
python3 - "$work/pgbench-key" "$work/state.json" <<'PY'
import json, sys
key = open(sys.argv[1]).read().strip()
db, sid = key.split("/", 1)
state = {"format": 1, "updated": "2026-01-01T00:00:00Z", "shapes": {}, "serve": {key: {
    "key": key, "id": sid, "database": db, "normalized": "", "calls": 0, "total_ms": 0,
    "p50_ms": 0, "p95_ms": 0, "last_seen": "2026-01-01T00:00:00Z", "status": "proven",
    "rounds": 0, "texts_tried": 0, "proven": "no memoize",
    "settings": [{"name": "enable_memoize", "value": "off"}]}}}
json.dump(state, open(sys.argv[2], "w"))
PY
start_serve --replica "$LQO_TEST_DSN" --allow-primary --mode active --holdout 0 --min-ms 1000000000
sleep 2
{
	echo
	echo "# Active mode: the same, with a hint applied to every one of pgbench's"
	echo "# SELECTs (no memoize, which leaves this plan as it is), so the extra is"
	echo "# what applying a hint costs: one more statement in the same round trip"
	echo
	for mode in simple extended prepared; do
		echo "$mode protocol"
		echo "  straight to the server: $(cat "$work/direct-$mode")"
		echo "  through lqo serve, hinted: $(pgb $mode "$via")"
	done
	echo
	curl -s "http://$status/status" | grep -E '"(hinted_calls|applied)"' | sed 's/^ */  /'
} >> "$out/overhead.txt"
stop_serve
cat "$out/overhead.txt"
fi

if [ -n "${BENCH_SKIP_DEMO:-}" ]; then rm -rf "$work"; exit 0; fi
echo "== learning the demo trap, then applying it"
rm -f "$work/state.json"
start_serve --replica "$LQO_TEST_DSN" --allow-primary --save-every 5s --mode active --holdout 25
trap1="SELECT sum(l.qty * p.price) FROM lqo_demo.customers c JOIN lqo_demo.orders o ON o.customer_id = c.id JOIN lqo_demo.order_lines l ON l.order_id = o.id JOIN lqo_demo.products p ON p.id = l.product_id WHERE c.attrs->>'tier' = 'gold' AND c.attrs->>'channel' = 'partner'"
trap2="SELECT sum(l.qty * p.price) FROM lqo_demo.customers c JOIN lqo_demo.orders o ON o.customer_id = c.id JOIN lqo_demo.order_lines l ON l.order_id = o.id JOIN lqo_demo.products p ON p.id = l.product_id WHERE c.attrs->>'tier' = 'basic' AND c.attrs->>'channel' = 'web'"
key="SELECT c.id, count(*), sum(l.qty) FROM lqo_demo.customers c JOIN lqo_demo.orders o ON o.customer_id = c.id JOIN lqo_demo.order_lines l ON l.order_id = o.id WHERE c.id = 4242 GROUP BY c.id"
minutes=${BENCH_LEARN_MINUTES:-15}
start=$(date +%s)
proven=""
while [ $(( $(date +%s) - start )) -lt $(( minutes * 60 )) ]; do
	# Traffic for the first two minutes only: here the "replica" is this
	# same machine, and a replica busy with the very queries being timed
	# makes every round noisy. Afterwards the learner works from what it
	# saw, as it would on a replica of its own.
	if [ $(( $(date +%s) - start )) -lt 120 ]; then
		psql -X -q "$via" -c "$trap1" -c "$trap2" -c "$key" > /dev/null
	fi
	if [ -z "$proven" ] && curl -s "http://$status/status" | grep -q '"status": "proven"'; then
		proven=$(( $(date +%s) - start ))
		curl -s "http://$status/" > "$work/first.txt"
		break
	fi
	sleep 5
done
# Once a hint is proven, live traffic again for three minutes: now the
# trap's calls run with the hint, and a quarter are held out without it.
if [ -n "$proven" ]; then
	live=$(date +%s)
	while [ $(( $(date +%s) - live )) -lt 180 ]; do
		psql -X -q "$via" -c "$trap1" -c "$trap2" -c "$key" > /dev/null
		sleep 5
	done
fi
curl -s "http://$status/status" > "$out/demo.json"
{
	echo "# lqo serve --mode active learning from live traffic: the demo's trap"
	echo "# query (two texts) and a key lookup, sent through LQO by psql every 5"
	echo "# seconds for the first two minutes; the same server stands in for the"
	echo "# replica (--allow-primary), with the default 25% budget. Once a hint is"
	echo "# proven, three more minutes of the same traffic, with a quarter of the"
	echo "# calls held out (--holdout 25) to compare."
	machine
	echo "# first hint proven after: ${proven:-not within $minutes minutes} s"
	echo
	if [ -n "$proven" ]; then
		echo "## When the first hint was proven"
		echo
		cat "$work/first.txt"
		echo
	fi
	echo "## After three more minutes of live traffic"
	echo
	curl -s "http://$status/"
} > "$out/demo.txt"
stop_serve
cat "$out/demo.txt"
rm -rf "$work"
