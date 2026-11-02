// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

// Second review of 0.6.0 Phase A: the fixes in 08f35c4 and 8e93c04.

package learn

import (
	"context"
	"strings"
	"testing"

	"github.com/learned-query-optimizer/lqo/internal/hints"
	"github.com/learned-query-optimizer/lqo/internal/sqltext"
	"github.com/learned-query-optimizer/lqo/internal/state"
)

const skewQuery = "SELECT count(*) FROM lqo_review_skew2 WHERE k = $1"

func skewRig(t *testing.T) *activeRig {
	t.Helper()
	r := newActiveRig(t, "", func(f *state.File, db string) {
		key := db + "/" + sqltext.Fingerprint(skewQuery)
		f.Serve[key] = &state.Learned{Key: key, ID: sqltext.Fingerprint(skewQuery), Database: db, Status: Proven,
			Proven: "no index scans", Settings: []hints.Setting{
				{Name: "enable_indexscan", Value: "off"}, {Name: "enable_bitmapscan", Value: "off"}, {Name: "enable_indexonlyscan", Value: "off"}},
			Arms: map[string]*state.Arm{}}
	})
	ctx := context.Background()
	for _, s := range []string{
		"DROP TABLE IF EXISTS lqo_review_skew2",
		"CREATE TABLE lqo_review_skew2 (id int, k int)",
		"INSERT INTO lqo_review_skew2 SELECT g, CASE WHEN g <= 20 THEN g ELSE 0 END FROM generate_series(1, 200000) g",
		"CREATE INDEX ON lqo_review_skew2 (k)",
		"ANALYZE lqo_review_skew2",
	} {
		if _, err := r.direct.Exec(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { r.direct.Exec(context.Background(), "DROP TABLE IF EXISTS lqo_review_skew2") })
	return r
}

func bindSkew(stmt string) raw {
	params := []byte{0, 0, 0, 1, 0, 0, 0, 1, '1', 0, 0}
	return raw(nil).msg('B', cs(""), cs(stmt), params).msg('E', cs(""), []byte{0, 0, 0, 0}).msg('S')
}

// The fresh-plan fix covers only calls LQO could hint: a fresh, whole
// batch. The same prepared statement run in a pipeline (or with Flush, or
// several Executes before one Sync) gets nothing of LQO's, so the hinted
// calls' costs can still move those calls onto the generic plan. The review
// found the docs claiming more; they now list this as a limit, and this
// test checks what is promised: the pipelined calls may go generic, the
// whole-batch calls after them don't.
func TestReviewSkewThroughPipelinedCalls(t *testing.T) {
	r := skewRig(t)
	stmt := "s_pipe"
	if err := r.app.SendRaw(raw(nil).msg('P', cs(stmt), cs(skewQuery), []byte{0, 0}).msg('S')); err != nil {
		t.Fatal(err)
	}
	r.replies(1)
	for i := 0; i < 5; i++ { // hinted, one batch at a time
		r.app.SendRaw(bindSkew(stmt))
		if types, _ := r.replies(1); strings.Contains(types, "E") {
			t.Fatalf("call %d: %q", i, types)
		}
	}
	if _, err := r.l.SwitchShape(sqltext.Fingerprint(skewQuery), true); err != nil {
		t.Fatal(err)
	}
	// Three calls sent together: the first is a fresh batch, the other two
	// are in flight behind it.
	var b raw
	for i := 0; i < 3; i++ {
		b = append(b, bindSkew(stmt)...)
	}
	r.app.SendRaw(b)
	if types, _ := r.replies(3); strings.Contains(types, "E") {
		t.Fatalf("pipeline: %q", types)
	}
	generic := func() string {
		return r.simple("SELECT generic_plans FROM pg_prepared_statements WHERE name = '" + stmt + "'")
	}
	before := generic()
	t.Logf("generic plans after the pipeline: %s (a documented limit)", before)
	for i := 0; i < 3; i++ { // one batch at a time again
		r.app.SendRaw(bindSkew(stmt))
		if types, _ := r.replies(1); strings.Contains(types, "E") {
			t.Fatalf("call %d: %q", i, types)
		}
	}
	if got := generic(); got != before {
		t.Errorf("whole-batch calls of a statement LQO hinted before went generic: %s, was %s", got, before)
	}
}
