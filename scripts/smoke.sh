#!/bin/sh
# Copyright lqo.net 2026
# SPDX-License-Identifier: Apache-2.0
#
# A quick check that a built lqo binary works. Without a connection string
# it checks the commands that don't need a server. With one, it also builds
# a small demo and runs the advisor on it, read-only.
#
#   sh scripts/smoke.sh ./lqo
#   sh scripts/smoke.sh ./lqo postgres://postgres:secret@localhost/test
set -eu
bin=$(cd "$(dirname "$1")" && pwd)/$(basename "$1")
dsn=${2:-}
dir=$(mktemp -d)
cd "$dir"

"$bin" version
"$bin" hints | grep -q 'SET LOCAL enable_nestloop = off;'
test "$("$bin" hints --hints full | wc -l | tr -d ' ')" = 48
"$bin" demo --workload-only --workload demo.sql >/dev/null
grep -q lqo_demo demo.sql
"$bin" show --state none.json | grep -q 'holds nothing yet'
if "$bin" advise >/dev/null 2>&1; then
	echo "smoke: advise with no workload should fail"
	exit 1
fi

if [ -n "$dsn" ]; then
	"$bin" demo --dsn "$dsn" --scale 0.05 --workload demo.sql 2>/dev/null
	"$bin" advise --dsn "$dsn" --allow-primary --quiet --repeat 1 --report json --out report.json demo.sql
	grep -q '"verdict": "skipped"' report.json
	"$bin" show --state lqo-state.json | grep -q 'lqo-state.json, updated'
fi

cd /
rm -rf "$dir"
echo "smoke test passed"
