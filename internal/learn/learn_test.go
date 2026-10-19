// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

package learn

import (
	"context"
	"math/rand/v2"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/learned-query-optimizer/lqo/internal/advisor"
	"github.com/learned-query-optimizer/lqo/internal/demo"
	"github.com/learned-query-optimizer/lqo/internal/hints"
	"github.com/learned-query-optimizer/lqo/internal/pgwire"
	"github.com/learned-query-optimizer/lqo/internal/proxy"
	"github.com/learned-query-optimizer/lqo/internal/state"
	"github.com/learned-query-optimizer/lqo/internal/watch"
)

var testArms = []hints.Arm{
	{Name: "good", Settings: []hints.Setting{{Name: "enable_nestloop", Value: "off"}}},
	{Name: "same", Settings: []hints.Setting{{Name: "enable_memoize", Value: "off"}}},
	{Name: "bad", Settings: []hints.Setting{{Name: "enable_hashjoin", Value: "off"}}},
	{Name: "good too", Settings: []hints.Setting{{Name: "enable_nestloop", Value: "off"}, {Name: "enable_mergejoin", Value: "off"}}},
}

func newTestLearner() *Learner {
	opt := Options{}
	opt.fill()
	return &Learner{opt: opt, file: state.New(), rng: rand.New(rand.NewPCG(1, 2))}
}

func newShape(l *Learner) *state.Learned {
	e := &state.Learned{Key: "db/x", ID: "0123456789abcdef", Database: "db", Status: Learning, Arms: map[string]*state.Arm{}}
	for _, a := range testArms {
		e.Arms[a.Name] = &state.Arm{}
	}
	l.file.Serve[e.Key] = e
	return e
}

// sample builds a round's result: the default plan at base ms, and each
// named arm at base times its ratio. A ratio of 1 means the same plan.
func sample(base float64, ratios map[string]float64) advisor.SampleResult {
	sr := advisor.SampleResult{Text: "t", BaseMs: base, BaseRuns: []float64{base * 0.99, base, base * 1.01}, BaseRows: 1}
	for name, r := range ratios {
		at := advisor.ArmTiming{Name: name, Rows: 1}
		if r == 1 {
			at.Same = true
		} else {
			at.Ms = base * r
			at.Runs = []float64{base * r * 0.99, base * r, base * r * 1.01}
		}
		sr.Arms = append(sr.Arms, at)
	}
	return sr
}

func TestProvesAClearWinner(t *testing.T) {
	l := newTestLearner()
	e := newShape(l)
	for i := 0; i < 3; i++ {
		l.update(e, sample(100+float64(i)*10, map[string]float64{"good": 0.6, "same": 1, "bad": 1.4, "good too": 0.6}), testArms)
	}
	if e.Status != Proven || e.Proven != "good" {
		t.Fatalf("status %s, proven %q, reason %q", e.Status, e.Proven, e.Reason)
	}
	if e.Gain < 0.39 || e.Gain > 0.41 {
		t.Errorf("gain %.2f", e.Gain)
	}
	if e.Arms["bad"].Out == "" {
		t.Error("the slower hint wasn't ruled out")
	}
	if e.Arms["same"].Out == "" {
		t.Error("a hint that never changed the plan wasn't set aside")
	}
	if l.stats.Proven != 1 {
		t.Errorf("proven count %d", l.stats.Proven)
	}
}

func TestNeedsEnoughTexts(t *testing.T) {
	l := newTestLearner()
	e := newShape(l)
	for i := 0; i < 2; i++ {
		l.update(e, sample(100, map[string]float64{"good": 0.5}), testArms)
	}
	if e.Status == Proven {
		t.Error("proven after two texts")
	}
}

func TestNoisyHintIsNotProven(t *testing.T) {
	l := newTestLearner()
	e := newShape(l)
	for _, r := range []float64{0.45, 1.05, 0.5, 1.08} {
		l.update(e, sample(100, map[string]float64{"good": r}), testArms)
	}
	if e.Status == Proven {
		t.Errorf("a hint that helped on half the texts was proven: %+v", e.Arms["good"])
	}
}

func TestSlowerTwiceOnATextIsOut(t *testing.T) {
	l := newTestLearner()
	e := newShape(l)
	for _, r := range []float64{0.5, 0.5, 1.2, 1.3, 0.5} {
		l.update(e, sample(100, map[string]float64{"good": r}), testArms)
	}
	if e.Status == Proven || e.Arms["good"].Out == "" {
		t.Errorf("status %s, arm %+v", e.Status, e.Arms["good"])
	}
}

// A replica busy with other work can make one round look bad. The hint is
// tried on that text again, and only a second bad result rules it out.
func TestOneNoisyRoundIsForgiven(t *testing.T) {
	l := newTestLearner()
	e := newShape(l)
	l.update(e, sample(100, map[string]float64{"good": 2.0}), testArms)
	if e.Arms["good"].Out != "" || e.Arms["good"].Suspect != "t" {
		t.Fatalf("after one bad round: %+v", e.Arms["good"])
	}
	if s := suspectText(e); s != "t" {
		t.Errorf("the next round should retry the text, got %q", s)
	}
	if got := l.choose(e, testArms, "t"); len(got) == 0 || got[0] != 0 {
		t.Errorf("the suspect hint should be tried first: %v", got)
	}
	for i := 0; i < 3; i++ {
		l.update(e, sample(100, map[string]float64{"good": 0.5}), testArms)
	}
	if e.Status != Proven || e.Arms["good"].MaxRatio > 1 {
		t.Errorf("status %s, arm %+v", e.Status, e.Arms["good"])
	}
}

func TestProvenHintIsDemoted(t *testing.T) {
	l := newTestLearner()
	e := newShape(l)
	for i := 0; i < 3; i++ {
		l.update(e, sample(100, map[string]float64{"good": 0.5}), testArms)
	}
	if e.Status != Proven {
		t.Fatal("not proven")
	}
	l.update(e, sample(100, map[string]float64{"good": 1.5}), testArms)
	if e.Status != Proven {
		t.Error("demoted on one bad round")
	}
	l.update(e, sample(100, map[string]float64{"good": 1.5}), testArms)
	if e.Status == Proven || e.Proven != "" || l.stats.Proven != 0 {
		t.Errorf("status %s proven %q count %d", e.Status, e.Proven, l.stats.Proven)
	}
}

func TestDifferentRowsAndTimeoutsRuleOut(t *testing.T) {
	l := newTestLearner()
	e := newShape(l)
	sr := sample(100, map[string]float64{"good": 0.5, "bad": 0.5})
	for i := range sr.Arms {
		if sr.Arms[i].Name == "good" {
			sr.Arms[i].Rows = 2
		} else {
			sr.Arms[i].TimedOut = true
		}
	}
	l.update(e, sr, testArms)
	if e.Arms["bad"].Out != "" {
		t.Error("one time-out ruled the hint out without a second look")
	}
	l.update(e, sr, testArms)
	if !strings.Contains(e.Arms["good"].Out, "rows") || !strings.Contains(e.Arms["bad"].Out, "time limit") {
		t.Errorf("good %q bad %q", e.Arms["good"].Out, e.Arms["bad"].Out)
	}
}

func TestKeepWhenNothingHelps(t *testing.T) {
	l := newTestLearner()
	e := newShape(l)
	for i := 0; i < 5; i++ {
		l.update(e, sample(100, map[string]float64{"good": 0.97, "same": 1, "bad": 1.02, "good too": 0.99}), testArms)
	}
	if e.Status != Keep {
		t.Errorf("status %s: %+v", e.Status, e.Arms)
	}
}

func TestBaseFailingThreeTimesIsAnError(t *testing.T) {
	l := newTestLearner()
	e := newShape(l)
	for i := 0; i < 3; i++ {
		l.update(e, advisor.SampleResult{Error: "relation does not exist"}, testArms)
	}
	if e.Status != Failed {
		t.Errorf("status %s", e.Status)
	}
}

func TestThompsonFavoursTheEvidence(t *testing.T) {
	l := newTestLearner()
	l.opt.ArmsPerRound = 1
	e := newShape(l)
	for i := 0; i < 2; i++ {
		l.update(e, sample(100, map[string]float64{"good": 0.6, "same": 1, "bad": 1.05, "good too": 0.98}), testArms)
	}
	counts := map[int]int{}
	for i := 0; i < 1000; i++ {
		counts[l.choose(e, testArms, "t")[0]]++
	}
	if counts[0] < 900 {
		t.Errorf("the hint with the best evidence was picked %d times in 1000: %v", counts[0], counts)
	}
	if counts[2] != 0 {
		t.Error("a ruled-out hint was picked")
	}
}

func TestUntriedHintsGetTheirTurn(t *testing.T) {
	l := newTestLearner()
	l.opt.ArmsPerRound = 2
	e := newShape(l)
	seen := map[int]bool{}
	for i := 0; i < 50; i++ {
		for _, a := range l.choose(e, testArms, "t")[:2] {
			seen[a] = true
		}
	}
	if len(seen) != len(testArms) {
		t.Errorf("only %v were tried", seen)
	}
}

// The whole loop on a real server: the demo's trap, at a quarter of its
// size, in its own schema, driven through the proxy, learned on the same
// server standing in for a replica.

func TestLearnsTheDemoTrapThroughTheProxy(t *testing.T) {
	dsn := os.Getenv("LQO_TEST_DSN")
	if dsn == "" {
		t.Skip("LQO_TEST_DSN not set")
	}
	if testing.Short() {
		t.Skip("short")
	}
	ctx := context.Background()
	cfg, _ := pgwire.ParseConfig(dsn)
	conn, err := pgwire.Connect(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	const schema = "lqo_test_learn"
	for _, s := range demo.SetupSQL(0.25) {
		if _, err := conn.Exec(ctx, strings.ReplaceAll(s, demo.Schema, schema)); err != nil {
			t.Fatal(err)
		}
	}

	col := watch.NewCollector()
	defer col.Close()
	up := *cfg
	up.SSLMode = "disable"
	ln, err := proxy.Listen("127.0.0.1:0", 0)
	if err != nil {
		t.Fatal(err)
	}
	srv := proxy.New(proxy.Config{Upstream: &up, Watch: col})
	sctx, stop := context.WithCancel(ctx)
	defer stop()
	go srv.Serve(sctx, ln)
	defer srv.Shutdown(time.Second)
	u, _ := url.Parse(dsn)
	u.Host = net.JoinHostPort("127.0.0.1", strconv.Itoa(ln.Addr().(*net.TCPAddr).Port))
	pcfg, _ := pgwire.ParseConfig(u.String())
	app, err := pgwire.Connect(ctx, pcfg)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	trap := func(tier, channel string) string {
		return "SELECT sum(l.qty * p.price) FROM " + schema + ".customers c JOIN " + schema + ".orders o ON o.customer_id = c.id JOIN " + schema + ".order_lines l ON l.order_id = o.id JOIN " + schema + ".products p ON p.id = l.product_id WHERE c.attrs->>'tier' = '" + tier + "' AND c.attrs->>'channel' = '" + channel + "'"
	}
	key := "SELECT count(*) FROM " + schema + ".customers c JOIN " + schema + ".orders o ON o.customer_id = c.id WHERE c.id = "
	for i := 0; i < 3; i++ {
		for _, q := range []string{trap("gold", "partner"), trap("basic", "web"), key + strconv.Itoa(100+i)} {
			if _, err := app.ExecOne(ctx, q); err != nil {
				t.Fatal(err)
			}
		}
	}
	col.Flush()

	statePath := filepath.Join(t.TempDir(), "lqo-state.json")
	l, _, err := New(col, Options{Replica: cfg, AllowPrimary: true, MinMs: 0, MinCalls: 1, StatePath: statePath, Repeat: 2})
	if err != nil {
		t.Fatal(err)
	}
	var trapShape *state.Learned
	for round := 0; round < 40; round++ {
		if _, err := l.round(ctx); err != nil {
			t.Fatal(err)
		}
		for _, s := range l.Shapes() {
			if strings.Contains(s.Normalized, "sum ( l . qty * p . price )") {
				s := s
				trapShape = &s
			}
		}
		if trapShape != nil && trapShape.Status == Proven {
			break
		}
	}
	if trapShape == nil || trapShape.Status != Proven {
		t.Fatalf("the trap wasn't proven: %+v", trapShape)
	}
	off := false
	for _, s := range trapShape.Settings {
		if s.Name == "enable_nestloop" && s.Value == "off" {
			off = true
		}
	}
	if !off {
		t.Errorf("trap proven with %q, %v", trapShape.Proven, trapShape.Settings)
	}
	t.Logf("trap proven after %d rounds with %q, %.0f%% faster", trapShape.Rounds, trapShape.Proven, trapShape.Gain*100)
	if err := l.Save(); err != nil {
		t.Fatal(err)
	}
	f, err := state.Load(statePath)
	if err != nil || f.Serve[trapShape.Key] == nil || f.Serve[trapShape.Key].Status != Proven {
		t.Errorf("state file: %v", err)
	}
	for _, s := range l.Shapes() {
		if strings.Contains(s.Normalized, "c . id = ?") && s.Status == Proven {
			t.Errorf("the key lookup got a hint: %+v", s)
		}
	}
	conn.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
}

// Losing the replica pauses learning and nothing else: the application's
// traffic keeps flowing, and learning picks up again when the replica is
// back.
func TestReplicaLossPausesLearningOnly(t *testing.T) {
	dsn := os.Getenv("LQO_TEST_DSN")
	if dsn == "" {
		t.Skip("LQO_TEST_DSN not set")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg, _ := pgwire.ParseConfig(dsn)
	up := *cfg
	up.SSLMode = "disable"

	// The application's proxy.
	col := watch.NewCollector()
	defer col.Close()
	appLn, _ := proxy.Listen("127.0.0.1:0", 0)
	app := proxy.New(proxy.Config{Upstream: &up, Watch: col})
	go app.Serve(ctx, appLn)
	defer app.Shutdown(time.Second)

	// The "replica": another proxy in front of the same server, which the
	// test can take away and bring back on the same port.
	repLn, _ := proxy.Listen("127.0.0.1:0", 0)
	repAddr := repLn.Addr().String()
	startReplica := func(ln net.Listener) (*proxy.Server, context.CancelFunc) {
		rctx, rcancel := context.WithCancel(ctx)
		s := proxy.New(proxy.Config{Upstream: &up})
		go s.Serve(rctx, ln)
		return s, rcancel
	}
	rep, stopRep := startReplica(repLn)

	repCfg := *cfg
	host, port, _ := net.SplitHostPort(repAddr)
	repCfg.Host = host
	repCfg.Port, _ = strconv.Atoi(port)
	repCfg.SSLMode = "disable"

	u, _ := url.Parse(dsn)
	u.Host = appLn.Addr().String()
	appCfg, _ := pgwire.ParseConfig(u.String())
	conn, err := pgwire.Connect(ctx, appCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	q := func(i int) error {
		_, err := conn.ExecOne(ctx, "SELECT count(*) FROM pg_class c JOIN pg_attribute a ON a.attrelid = c.oid WHERE a.attnum > "+strconv.Itoa(i%3))
		return err
	}
	for i := 0; i < 5; i++ {
		if err := q(i); err != nil {
			t.Fatal(err)
		}
	}
	col.Flush()

	l, _, err := New(col, Options{Replica: &repCfg, AllowPrimary: true, MinMs: 0, MinCalls: 1, Budget: 1, Repeat: 1})
	if err != nil {
		t.Fatal(err)
	}
	ldone := make(chan error, 1)
	go func() { ldone <- l.Run(ctx) }()
	waitFor := func(what string, ok func(Stats) bool) {
		t.Helper()
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			if ok(l.Stats()) {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for %s: %+v", what, l.Stats())
	}
	waitFor("a first round", func(s Stats) bool { return s.Rounds > 0 })

	// Take the replica away, connections and all.
	stopRep()
	rep.Shutdown(0)
	waitFor("learning to notice", func(s Stats) bool { return s.ReplicaDown })
	for i := 0; i < 20; i++ {
		if err := q(i); err != nil {
			t.Fatalf("application query failed while the replica was gone: %v", err)
		}
	}

	// Bring it back on the same port.
	ln, err := net.Listen("tcp", repAddr)
	if err != nil {
		t.Skipf("couldn't take the port back: %v", err)
	}
	_, stopRep2 := startReplica(ln)
	defer stopRep2()
	waitFor("learning to resume", func(s Stats) bool { return !s.ReplicaDown })
	cancel()
	if err := <-ldone; err != nil {
		t.Errorf("Run: %v", err)
	}
}

// The gain is judged over the texts' times added up, as lqo advise does:
// a hint that halves a quick text and barely touches the long ones doesn't
// save 20% of the time.
func TestGainIsOverSummedTimes(t *testing.T) {
	l := newTestLearner()
	e := newShape(l)
	for _, c := range []struct{ base, r float64 }{{100, 0.79}, {1000, 0.82}, {1000, 0.82}, {1000, 0.82}} {
		l.update(e, sample(c.base, map[string]float64{"good": c.r}), testArms)
	}
	if e.Status == Proven {
		t.Errorf("proven at %.0f%% of the summed time", 100*e.Arms["good"].SumMs/e.Arms["good"].SumBaseMs)
	}
}
