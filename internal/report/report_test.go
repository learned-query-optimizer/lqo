// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

package report

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/learned-query-optimizer/lqo/internal/advisor"
	"github.com/learned-query-optimizer/lqo/internal/hints"
)

func sample() *advisor.Report {
	return &advisor.Report{
		Server: "PostgreSQL 16.4 on x86_64-pc-linux-gnu", Database: "shop", Standby: true,
		HintSet: "standard", Arms: 13, Repeat: 3, SamplesPer: 3, MinGain: 0.2,
		Workload: []string{"slow.log (stderr)"}, Statements: 40, ShapesTotal: 4,
		Shapes: []advisor.ShapeResult{
			{
				ID: "891f0819ce6bf4ec", Example: "SELECT sum(qty) FROM a JOIN b ON b.a_id = a.id", Calls: 10, Verdict: advisor.Faster,
				BaselineMs: 2000, Samples: make([]advisor.SampleResult, 2),
				Best: &advisor.ArmResult{Name: "no nested loop", Settings: []hints.Setting{{Name: "enable_nestloop", Value: "off"}},
					Lines: []string{"SET LOCAL enable_nestloop = off;"}, Ms: 1000, Gain: 0.5, Change: "Nested Loop ×2 → Hash Join ×2"},
			},
			{ID: "24eb83c21651051b", Example: "SELECT id FROM t WHERE x = 'a|b'", Calls: 5, Verdict: advisor.Keep,
				BaselineMs: 30, Samples: make([]advisor.SampleResult, 3), Reason: "a hint helped one sample but was slower on another"},
			{ID: "48529e11aaaaaaaa", Example: "UPDATE t SET x = 1", Calls: 1, Verdict: advisor.Skipped, Reason: "only reads are tested"},
			{ID: "5fcb12e3bbbbbbbb", Example: "SELECT * FROM gone", Calls: 1, Verdict: advisor.Failed, Reason: "relation \"gone\" does not exist"},
		},
	}
}

func TestTotals(t *testing.T) {
	before, after := Totals(sample())
	// faster: 2000/2 per call × 10 calls = 10000, then 1000/2 × 10 = 5000
	// keep: 30/3 × 5 = 50 either way; skipped and failed don't count
	if math.Abs(before-10050) > 1e-9 || math.Abs(after-5050) > 1e-9 {
		t.Errorf("before %v, after %v", before, after)
	}
}

func TestText(t *testing.T) {
	var b strings.Builder
	if err := Write(&b, sample(), "text"); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{
		"PostgreSQL 16.4, database shop (standby)",
		"Faster with a hint: 1 of 2 tested",
		"1,000 ms → 500 ms (−50%) with \"no nested loop\"",
		"SET LOCAL enable_nestloop = off;",
		"Nested Loop ×2 → Hash Join ×2",
		"Keep the default plan: 1",
		"Not tested: 2",
		"skipped: only reads are tested",
		"10,050 ms now, 5,050 ms with these hints (−50%)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("text report lacks %q\n%s", want, out)
		}
	}
}

func TestMarkdown(t *testing.T) {
	var b strings.Builder
	if err := Write(&b, sample(), "markdown"); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{"# LQO advisor report", "## Faster with a hint (1 of 2 tested)", "```sql\nSET LOCAL enable_nestloop = off;\n```", "x = 'a\\|b'", "## Not tested (2)"} {
		if !strings.Contains(out, want) {
			t.Errorf("markdown lacks %q\n%s", want, out)
		}
	}
}

func TestJSON(t *testing.T) {
	var b strings.Builder
	if err := Write(&b, sample(), "json"); err != nil {
		t.Fatal(err)
	}
	var back advisor.Report
	if err := json.Unmarshal([]byte(b.String()), &back); err != nil {
		t.Fatal(err)
	}
	if len(back.Shapes) != 4 || back.Shapes[0].Best.Lines[0] != "SET LOCAL enable_nestloop = off;" {
		t.Errorf("round trip lost data: %+v", back.Shapes[0])
	}
	if err := Write(&b, sample(), "yaml"); err == nil {
		t.Error("unknown format accepted")
	}
}

func TestPrimaryLineAndInterrupt(t *testing.T) {
	r := sample()
	r.Standby = false
	r.Interrupted = true
	var b strings.Builder
	Write(&b, r, "text")
	if !strings.Contains(b.String(), "primary, allowed with --allow-primary") || !strings.Contains(b.String(), "interrupted") {
		t.Error(b.String())
	}
}
