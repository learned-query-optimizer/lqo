#!/bin/sh
# Copyright lqo.net 2026
# SPDX-License-Identifier: Apache-2.0
#
# Turns the failures in a go test log into one GitHub Actions error
# annotation, so they can be read through the API as well as in the log.
#
#   sh scripts/ci-annotate.sh test-output.txt
set -eu
msg=$(grep -n -E -B 3 -A 30 -- '--- FAIL|^FAIL|panic:|WARNING: DATA RACE' "$1" | head -c 30000 || true)
[ -n "$msg" ] || msg=$(tail -n 60 "$1")
msg=$(printf '%s' "$msg" | sed 's/%/%25/g; s/\r/%0D/g' | awk 'BEGIN{ORS="%0A"} {print}')
echo "::error title=go test failures::$msg"
