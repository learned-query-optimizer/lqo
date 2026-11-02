// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

package watch

import (
	"context"
	"os"
	"testing"

	"github.com/learned-query-optimizer/lqo/internal/pgwire"
)

// The fresh-plan SQL forces custom plans, keeps an application's own
// force_generic_plan, and in a transaction sets everything back.
func TestFreshPlanSQLOnTheServer(t *testing.T) {
	dsn := os.Getenv("LQO_TEST_DSN")
	if dsn == "" {
		t.Skip("LQO_TEST_DSN not set")
	}
	ctx := context.Background()
	cfg, _ := pgwire.ParseConfig(dsn)
	c, err := pgwire.Connect(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	val := func(q string) string {
		t.Helper()
		v, err := c.QueryValue(ctx, q)
		if err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		return v
	}
	exec := func(q string) {
		t.Helper()
		if _, err := c.Exec(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	for _, tc := range []struct{ app, want string }{{"auto", "force_custom_plan"}, {"force_custom_plan", "force_custom_plan"}, {"force_generic_plan", "force_generic_plan"}} {
		exec("BEGIN")
		exec("SET LOCAL plan_cache_mode = " + tc.app)
		val(plainHint.Save)
		if got := val("SHOW plan_cache_mode"); got != tc.want {
			t.Errorf("app %s: %s under the fresh plan, want %s", tc.app, got, tc.want)
		}
		val(plainHint.Restore)
		if got := val("SHOW plan_cache_mode"); got != tc.app {
			t.Errorf("app %s: %s after Restore", tc.app, got)
		}
		exec("COMMIT")
	}
}
