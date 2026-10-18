#!/bin/bash
# Copyright lqo.net 2026
# SPDX-License-Identifier: Apache-2.0
#
# Runs the Join Order Benchmark (JOB) through lqo advise and writes the
# reports to test/results/job/. JOB is 113 queries over the IMDB data set
# from "How Good Are Query Optimizers, Really?" (Leis et al., VLDB 2015),
# the standard test for query optimizers.
#
# It needs a PostgreSQL server it may load about 4 GB into, psql, curl and
# about 15 GB of disk. The job workflow runs it on a GitHub runner, because
# the IMDB files are downloaded from CWI's server.
#
#   LQO_DSN=postgres://postgres:secret@localhost/imdb sh scripts/job.sh
set -euo pipefail
cd "$(dirname "$0")/.."
: "${LQO_DSN:?set LQO_DSN to the database to load IMDB into}"
work=${JOB_WORK:-$(mktemp -d)}
out=test/results/job
mkdir -p "$out"

echo "== Join Order Benchmark queries"
if [ ! -d "$work/job" ]; then
	git clone --depth 1 -q https://github.com/gregrahn/join-order-benchmark "$work/job"
fi
git -C "$work/job" rev-parse HEAD > "$work/job-commit.txt"

echo "== IMDB data (May 2013, as in the paper)"
if [ ! -f "$work/imdb/title.csv" ]; then
	mkdir -p "$work/imdb"
	ok=""
	for url in https://event.cwi.nl/da/job/imdb.tgz http://event.cwi.nl/da/job/imdb.tgz; do
		if curl -fL --retry 3 -o "$work/imdb.tgz" "$url"; then ok=$url; break; fi
	done
	[ -n "$ok" ] || { echo "couldn't download imdb.tgz" >&2; exit 1; }
	echo "$ok" > "$work/imdb-url.txt"
	tar -xzf "$work/imdb.tgz" -C "$work/imdb"
	rm "$work/imdb.tgz"
fi

echo "== Load"
psql "$LQO_DSN" -q -v ON_ERROR_STOP=1 -f "$work/job/schema.sql"
for f in "$work"/imdb/*.csv; do
	t=$(basename "$f" .csv)
	psql "$LQO_DSN" -q -v ON_ERROR_STOP=1 -c "\\copy $t FROM '$f' WITH (FORMAT csv, ESCAPE '\\')"
done
psql "$LQO_DSN" -q -v ON_ERROR_STOP=1 -f "$work/job/fkindexes.sql"
psql "$LQO_DSN" -q -v ON_ERROR_STOP=1 -c "VACUUM ANALYZE"

echo "== Workload"
: > "$work/job.sql"
for q in $(ls "$work"/job/[0-9]*.sql | sort -V); do
	echo "-- $(basename "$q")" >> "$work/job.sql"
	cat "$q" >> "$work/job.sql"
	echo >> "$work/job.sql"
done

echo "== lqo advise"
go build -o "$work/lqo" ./cmd/lqo
start=$(date +%s)
"$work/lqo" advise --dsn "$LQO_DSN" --allow-primary --top 113 --repeat 3 --samples 10 \
	--max-time "${JOB_MAX_TIME:-120s}" --state "$work/state.json" \
	--report json --out "$out/job.json" "$work/job.sql"
end=$(date +%s)
"$work/lqo" report --report text "$out/job.json" > "$out/job.txt"
"$work/lqo" report --report markdown "$out/job.json" > "$out/job.md"

{
	echo "# Join Order Benchmark run"
	echo "recorded $(date -u '+%Y-%m-%d %H:%M UTC'), lqo advise took $(( (end - start) / 60 )) minutes"
	echo "machine: $(uname -sr), $(grep -m1 'model name' /proc/cpuinfo | cut -d: -f2- | sed 's/^ //'), $(nproc) CPUs, $(free -g | awk '/Mem:/ {print $2}') GB memory"
	echo "server: $(psql "$LQO_DSN" -Atc 'SELECT version()')"
	echo "settings: $(psql "$LQO_DSN" -Atc "SELECT string_agg(name || '=' || setting || coalesce(unit, ''), ', ' ORDER BY name) FROM pg_settings WHERE name IN ('shared_buffers', 'work_mem', 'effective_cache_size', 'max_parallel_workers_per_gather', 'random_page_cost', 'jit')")"
	echo "queries: github.com/gregrahn/join-order-benchmark at $(cat "$work/job-commit.txt")"
	echo "data: $(cat "$work/imdb-url.txt" 2>/dev/null || echo 'already downloaded')"
	echo "command: lqo advise --allow-primary --top 113 --repeat 3 --samples 10 --max-time ${JOB_MAX_TIME:-120s}"
} > "$out/environment.txt"
echo "results written to $out/"
