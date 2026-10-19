// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

// Package advisor runs each query shape under the server's default plan and
// under each hint set, against a replica or a copy, and keeps a hint only
// when it is clearly and consistently faster.
//
// Every trial runs as EXPLAIN inside its own READ ONLY transaction, with the
// hint applied by SET LOCAL and a statement_timeout, and is rolled back. So
// nothing a trial does outlives it, and nothing it does can write.
package advisor

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"math"
	"math/rand/v2"
	"sort"
	"strings"
	"time"

	"github.com/learned-query-optimizer/lqo/internal/hints"
	"github.com/learned-query-optimizer/lqo/internal/pgwire"
	"github.com/learned-query-optimizer/lqo/internal/plan"
	"github.com/learned-query-optimizer/lqo/internal/sqltext"
	"github.com/learned-query-optimizer/lqo/internal/workload"
)

// Options control a run. Zero values get the defaults in Defaults.
type Options struct {
	HintSet       string
	Arms          []hints.Arm
	Repeat        int           // timed runs per plan, after one warm-up run
	Samples       int           // distinct texts tried per shape
	Top           int           // shapes tried, slowest or most frequent first
	MinGain       float64       // a hint must save at least this share of the time, such as 0.2
	Tolerance     float64       // and must not be slower than this on any sample, such as 0.1
	TimeoutFactor float64       // a hint's runs are stopped at this multiple of the default plan's time
	MinTimeout    time.Duration // but never sooner than this
	MaxTime       time.Duration // the default plan's runs are stopped after this
	AllowPrimary  bool          // run against a primary, not just a standby
	Session       []hints.Setting
	Progress      io.Writer
}

// Defaults are the settings lqo advise uses unless told otherwise.
var Defaults = Options{
	HintSet:       "standard",
	Repeat:        3,
	Samples:       3,
	Top:           25,
	MinGain:       0.2,
	Tolerance:     0.1,
	TimeoutFactor: 3,
	MinTimeout:    250 * time.Millisecond,
	MaxTime:       60 * time.Second,
}

func (o *Options) fill() error {
	d := Defaults
	if o.HintSet == "" {
		o.HintSet = d.HintSet
	}
	if o.Arms == nil {
		arms, err := hints.Set(o.HintSet)
		if err != nil {
			return err
		}
		o.Arms = arms
	}
	if o.Repeat <= 0 {
		o.Repeat = d.Repeat
	}
	if o.Samples <= 0 {
		o.Samples = d.Samples
	}
	if o.Top <= 0 {
		o.Top = d.Top
	}
	if o.MinGain <= 0 {
		o.MinGain = d.MinGain
	}
	if o.MinGain >= 1 {
		return errors.New("the minimum gain must be under 100%")
	}
	if o.Tolerance <= 0 {
		o.Tolerance = d.Tolerance
	}
	if o.TimeoutFactor <= 1 {
		o.TimeoutFactor = d.TimeoutFactor
	}
	if o.MinTimeout <= 0 {
		o.MinTimeout = d.MinTimeout
	}
	if o.MaxTime <= 0 {
		o.MaxTime = d.MaxTime
	}
	if o.Progress == nil {
		o.Progress = io.Discard
	}
	return nil
}

// ErrPrimary is returned when the server isn't a standby and the run
// wasn't allowed on a primary.
var ErrPrimary = errors.New("this server is a primary, not a standby. LQO runs every query several times, so point it at a replica or a copy. If this is a copy or a test database, add --allow-primary")

// Verdicts for a shape.
const (
	Faster  = "faster"   // a hint was clearly and consistently faster
	Keep    = "keep"     // no hint beat the default plan clearly
	Skipped = "skipped"  // LQO won't run this statement
	Failed  = "error"    // the default plan failed to run
	TooSlow = "too slow" // the default plan ran longer than the time limit
)

// Report is the outcome of a run.
type Report struct {
	LQOVersion     string        `json:"lqo_version"`
	Started        time.Time     `json:"started"`
	Finished       time.Time     `json:"finished"`
	Interrupted    bool          `json:"interrupted,omitempty"`
	Server         string        `json:"server"`
	ServerNum      int           `json:"server_version_num"`
	Database       string        `json:"database"`
	Target         string        `json:"target"`
	Standby        bool          `json:"standby"`
	HintSet        string        `json:"hint_set"`
	Arms           int           `json:"arms"`
	DroppedArms    []string      `json:"dropped_arms,omitempty"`
	Repeat         int           `json:"repeat"`
	SamplesPer     int           `json:"samples_per_shape"`
	MinGain        float64       `json:"min_gain"`
	TimeoutFactor  float64       `json:"timeout_factor"`
	MaxTimeMs      float64       `json:"max_time_ms"`
	Workload       []string      `json:"workload"`
	Statements     int           `json:"statements"`
	OtherDatabases int           `json:"statements_in_other_databases,omitempty"`
	ShapesTotal    int           `json:"shapes_total"`
	Shapes         []ShapeResult `json:"shapes"`
}

// ShapeResult is what LQO found for one shape.
type ShapeResult struct {
	ID         string         `json:"id"`
	QueryID    string         `json:"query_id,omitempty"`
	Normalized string         `json:"normalized"`
	Calls      int            `json:"calls"`
	LoggedMs   float64        `json:"logged_ms,omitempty"`
	Example    string         `json:"example"`
	Verdict    string         `json:"verdict"`
	Reason     string         `json:"reason,omitempty"`
	BaselineMs float64        `json:"baseline_ms,omitempty"`
	Best       *ArmResult     `json:"best,omitempty"`
	Plans      int            `json:"distinct_plans,omitempty"`
	Notes      []string       `json:"notes,omitempty"`
	Samples    []SampleResult `json:"samples,omitempty"`
}

// ArmResult is the hint LQO suggests for a shape.
type ArmResult struct {
	Name     string          `json:"name"`
	Settings []hints.Setting `json:"settings"`
	Lines    []string        `json:"set_local"`
	Ms       float64         `json:"ms"`
	Gain     float64         `json:"gain"`
	Change   string          `json:"plan_change"`
	SamePlan []string        `json:"same_plan_as,omitempty"`
}

// SampleResult holds the runs for one distinct text of a shape.
type SampleResult struct {
	Text      string      `json:"text"`
	BaseMs    float64     `json:"baseline_ms"`
	BaseRuns  []float64   `json:"baseline_runs"`
	BaseRows  float64     `json:"rows"`
	BaseSteps []string    `json:"baseline_plan"`
	Arms      []ArmTiming `json:"arms"`
	Error     string      `json:"error,omitempty"`

	BaseRowsVary bool `json:"-"`
}

// ArmTiming is one arm on one sample.
type ArmTiming struct {
	Name     string    `json:"name"`
	Plan     string    `json:"plan"`
	Same     bool      `json:"same_plan_as_default,omitempty"`
	Ms       float64   `json:"ms,omitempty"`
	Runs     []float64 `json:"runs,omitempty"`
	TimedOut bool      `json:"timed_out,omitempty"`
	Rows     float64   `json:"rows,omitempty"`
	RowsVary bool      `json:"-"`
	Error    string    `json:"error,omitempty"`
}

// Advisor holds one connection and the options for a run.
type Advisor struct {
	conn *pgwire.Conn
	opt  Options
	snap map[string]string
}

// New checks the server and prepares a run. It refuses a primary unless
// AllowPrimary is set.
func New(ctx context.Context, conn *pgwire.Conn, opt Options, rep *Report) (*Advisor, error) {
	if err := opt.fill(); err != nil {
		return nil, err
	}
	a := &Advisor{conn: conn, opt: opt}
	ver, err := conn.QueryValue(ctx, "SELECT version()")
	if err != nil {
		return nil, err
	}
	rec, err := conn.QueryValue(ctx, "SELECT pg_is_in_recovery()")
	if err != nil {
		return nil, err
	}
	db, err := conn.QueryValue(ctx, "SELECT current_database()")
	if err != nil {
		return nil, err
	}
	rep.Server = ver
	rep.ServerNum = conn.ServerVersionNum()
	rep.Database = db
	rep.Standby = rec == "t"
	rep.HintSet = opt.HintSet
	rep.Repeat = opt.Repeat
	rep.SamplesPer = opt.Samples
	rep.MinGain = opt.MinGain
	rep.TimeoutFactor = opt.TimeoutFactor
	rep.MaxTimeMs = float64(opt.MaxTime.Milliseconds())
	if !rep.Standby && !opt.AllowPrimary {
		return nil, ErrPrimary
	}
	if conn.TxStatus() != 'I' {
		return nil, errors.New("the connection is inside a transaction")
	}

	// Keep only the arms whose settings this server has.
	names := hints.SettingNames(opt.Arms)
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = "'" + n + "'"
	}
	res, err := conn.Exec(ctx, "SELECT name FROM pg_settings WHERE name IN ("+strings.Join(quoted, ",")+")")
	if err != nil {
		return nil, err
	}
	known := map[string]bool{}
	for _, r := range res {
		for _, row := range r.Rows {
			if row[0] != nil {
				known[*row[0]] = true
			}
		}
	}
	a.opt.Arms, rep.DroppedArms = hints.Supported(opt.Arms, known)
	rep.Arms = len(a.opt.Arms)
	if len(a.opt.Arms) == 0 {
		return nil, errors.New("this server has none of the planner settings LQO tries")
	}

	// Session settings the user asked for, such as search_path.
	for _, s := range opt.Session {
		if _, err := conn.Exec(ctx, fmt.Sprintf("SELECT set_config(%s, %s, false)", quote(s.Name), quote(s.Value))); err != nil {
			return nil, fmt.Errorf("setting %s: %w", s.Name, err)
		}
	}
	// With standard_conforming_strings off, a backslash escapes a quote
	// inside '...', and LQO's reading of statements would differ from the
	// server's. Every supported version has it on by default.
	if v, err := conn.QueryValue(ctx, "SHOW standard_conforming_strings"); err != nil {
		return nil, err
	} else if v != "on" {
		return nil, errors.New("standard_conforming_strings is off on this connection; LQO runs only with it on, the default since PostgreSQL 9.1")
	}
	// Query IDs, where the server allows it. Ignore a refusal.
	if rep.ServerNum >= 140000 {
		conn.Exec(ctx, "SET compute_query_id = on")
	}

	a.snap, err = a.snapshot(ctx)
	if err != nil {
		return nil, err
	}
	return a, nil
}

// snapshot reads the session's current value of every setting LQO changes.
func (a *Advisor) snapshot(ctx context.Context) (map[string]string, error) {
	names := hints.SettingNames(a.opt.Arms)
	exprs := make([]string, len(names))
	for i, n := range names {
		exprs[i] = fmt.Sprintf("current_setting(%s)", quote(n))
	}
	res, err := a.conn.Exec(ctx, "SELECT "+strings.Join(exprs, ", "))
	if err != nil {
		return nil, err
	}
	if len(res) == 0 || len(res[0].Rows) == 0 {
		return nil, errors.New("couldn't read the session's settings")
	}
	out := map[string]string{}
	for i, n := range names {
		if v := res[0].Rows[0][i]; v != nil {
			out[n] = *v
		}
	}
	return out, nil
}

// checkNoLeak confirms no setting LQO changed is still changed. SET LOCAL
// ends with its transaction, so this should never fail; LQO stops if it does.
func (a *Advisor) checkNoLeak(ctx context.Context) error {
	now, err := a.snapshot(ctx)
	if err != nil {
		return err
	}
	for k, v := range a.snap {
		if now[k] != v {
			return fmt.Errorf("the setting %s is %q in the session, but was %q before the trials; stopping", k, now[k], v)
		}
	}
	if a.conn.TxStatus() != 'I' {
		return errors.New("a trial transaction was left open; stopping")
	}
	// A session-level advisory lock outlives a rolled-back transaction. The
	// statement check blocks the functions that take one, but a function in
	// the workload could still call them; if it did, stop, and closing the
	// connection releases the lock.
	n, err := a.conn.QueryValue(ctx, "SELECT count(*) FROM pg_locks WHERE pid = pg_backend_pid() AND locktype = 'advisory'")
	if err != nil {
		return err
	}
	if n != "0" {
		return errors.New("a trial left an advisory lock held; stopping, which releases it")
	}
	return nil
}

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

var errTimeout = errors.New("timed out")

// ErrUnsafe means a trial didn't stay inside its own transaction. LQO stops
// the whole run when it sees this; it should never happen.
var ErrUnsafe = errors.New("a trial left its READ ONLY transaction; LQO stopped to be safe")

// trial runs one EXPLAIN in a READ ONLY transaction with the arm's settings
// and rolls it back. A nil arm means the server's default plan.
//
// The BEGIN and the SET LOCAL lines are LQO's own text and go through the
// simple query protocol. The EXPLAIN with the workload's statement goes
// through the extended protocol, which runs one statement and no more, so
// nothing in the statement's text can end the transaction or run a second
// statement.
func (a *Advisor) trial(ctx context.Context, sql string, arm *hints.Arm, mode string, timeout time.Duration) (*plan.Explain, error) {
	var b strings.Builder
	b.WriteString("BEGIN TRANSACTION READ ONLY;")
	fmt.Fprintf(&b, " SET LOCAL statement_timeout = %d;", max(1, timeout.Milliseconds()))
	b.WriteString(" SET LOCAL lock_timeout = 2000;")
	if arm != nil {
		for _, l := range arm.SetLocal() {
			b.WriteString(" " + l)
		}
	}
	if _, err := a.conn.Exec(ctx, b.String()); err != nil {
		a.rollback()
		return nil, err
	}
	defer a.rollback()
	if a.conn.TxStatus() != 'T' {
		return nil, ErrUnsafe
	}
	var explain string
	switch mode {
	case "analyze":
		explain = "EXPLAIN (ANALYZE, TIMING OFF, SUMMARY ON, FORMAT JSON) "
	case "verbose":
		explain = "EXPLAIN (VERBOSE, FORMAT JSON) "
	default:
		explain = "EXPLAIN (FORMAT JSON) "
	}
	// A client-side deadline as a backstop to statement_timeout.
	tctx, cancel := context.WithTimeout(ctx, timeout+10*time.Second)
	defer cancel()
	res, err := a.conn.ExecOne(tctx, explain+sql)
	// Whatever happened, the transaction must still be open: in progress
	// after success, failed after an error. Idle would mean it ended.
	if st := a.conn.TxStatus(); !a.conn.Broken() && st != 'T' && st != 'E' {
		return nil, ErrUnsafe
	}
	if err != nil {
		if pgwire.IsCode(err, pgwire.CodeQueryCanceled) {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, errTimeout
		}
		return nil, err
	}
	if len(res) != 1 {
		return nil, ErrUnsafe
	}
	if len(res[0].Rows) > 0 && res[0].Rows[0][0] != nil {
		return plan.Parse(*res[0].Rows[0][0])
	}
	return nil, errors.New("EXPLAIN returned no rows")
}

func (a *Advisor) rollback() {
	if a.conn.Broken() || a.conn.TxStatus() == 'I' {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a.conn.Exec(ctx, "ROLLBACK")
}

// Run tries every shape, up to Top of them that can be run.
func (a *Advisor) Run(ctx context.Context, shapes []*workload.Shape, rep *Report) error {
	rep.ShapesTotal = len(shapes)
	tried := 0
	for i, sh := range shapes {
		if ctx.Err() != nil {
			rep.Interrupted = true
			break
		}
		r := ShapeResult{
			ID:         sh.ID,
			Normalized: sh.Normalized,
			Calls:      sh.Calls,
			LoggedMs:   sh.LoggedMs,
			Example:    sh.Samples[0].Text,
		}
		if len(sh.QueryIDs) == 1 {
			r.QueryID = sh.QueryIDs[0]
		}
		if sh.Skip != "" {
			r.Verdict, r.Reason = Skipped, sh.Skip
			rep.Shapes = append(rep.Shapes, r)
			continue
		}
		if tried >= a.opt.Top {
			r.Verdict, r.Reason = Skipped, fmt.Sprintf("only the top %d shapes are tried (--top)", a.opt.Top)
			rep.Shapes = append(rep.Shapes, r)
			continue
		}
		tried++
		fmt.Fprintf(a.opt.Progress, "[%d/%d] %s %s\n", i+1, len(shapes), sh.ID[:8], sqltext.OneLine(sh.Samples[0].Text, 70))
		err := a.shape(ctx, sh, &r)
		if err != nil {
			if ctx.Err() != nil {
				rep.Interrupted = true
				r.Verdict, r.Reason = Failed, "interrupted"
				rep.Shapes = append(rep.Shapes, r)
				break
			}
			return err
		}
		fmt.Fprintf(a.opt.Progress, "      %s\n", summary(&r))
		rep.Shapes = append(rep.Shapes, r)
		if a.conn.Broken() {
			return errors.New("lost the connection to the server")
		}
		if err := a.checkNoLeak(ctx); err != nil {
			return err
		}
	}
	return nil
}

func summary(r *ShapeResult) string {
	switch r.Verdict {
	case Faster:
		return fmt.Sprintf("faster with %q: %s → %s (%s)", r.Best.Name, ms(r.BaselineMs), ms(r.Best.Ms), pct(r.Best.Gain))
	case Keep:
		if r.Reason != "" {
			return fmt.Sprintf("keep the default plan (%s ms): %s", fmtMs(r.BaselineMs), r.Reason)
		}
		return fmt.Sprintf("keep the default plan (%s)", ms(r.BaselineMs))
	default:
		return fmt.Sprintf("%s: %s", r.Verdict, r.Reason)
	}
}

func ms(v float64) string { return fmtMs(v) + " ms" }

func fmtMs(v float64) string {
	switch {
	case v >= 100:
		return fmt.Sprintf("%.0f", v)
	case v >= 10:
		return fmt.Sprintf("%.1f", v)
	default:
		return fmt.Sprintf("%.2f", v)
	}
}

func pct(g float64) string { return fmt.Sprintf("−%.0f%%", g*100) }

// planGroup is one distinct plan for a sample, and the arms that produce it.
type planGroup struct {
	fp       string
	root     *plan.Node
	arms     []int // indexes into opt.Arms; -1 is the default plan
	runs     []float64
	rows     []float64
	timedOut bool
	err      string
}

// shape tries one shape on up to Samples of its texts.
func (a *Advisor) shape(ctx context.Context, sh *workload.Shape, r *ShapeResult) error {
	var samples []string
	for _, s := range sh.Samples {
		if len(samples) == a.opt.Samples {
			break
		}
		if sqltext.Classify(s.Text) == "" {
			samples = append(samples, s.Text)
		}
	}
	n := len(a.opt.Arms)
	// sumBase and sumArm add up medians across samples; ok[i] stays true
	// while arm i is never slower beyond tolerance, never times out, and
	// returns the same rows.
	sumBase := 0.0
	sumArm := make([]float64, n)
	ok := make([]bool, n)
	strictWin := make([]bool, n)
	helped := make([]bool, n)
	slower := make([]bool, n)
	laterFailed := ""
	rowsVary := false
	for i := range ok {
		ok[i] = true
	}
	var firstBase *plan.Node
	armRoot := make([]*plan.Node, n)
	maxPlans := 0
	for si, text := range samples {
		sr, groups, err := a.sample(ctx, text, si == 0 && r.QueryID == "", r)
		if err != nil {
			return err
		}
		if sr.Error != "" {
			if si == 0 {
				r.Verdict, r.Reason = Failed, sr.Error
				if errors.Is(errTimeoutLike(sr.Error), errTimeout) {
					r.Verdict = TooSlow
				}
				return nil
			}
			// A later text failed: don't trust any hint for this shape. The
			// failed text isn't counted in the shape's times.
			for i := range ok {
				ok[i] = false
			}
			if laterFailed == "" {
				laterFailed = sr.Error
			}
			r.Notes = append(r.Notes, "a text failed to run: "+sr.Error)
			continue
		}
		r.Samples = append(r.Samples, sr)
		distinct := 0
		for _, g := range groups {
			if g.err == "" {
				distinct++
			}
		}
		if distinct > maxPlans {
			maxPlans = distinct
		}
		base := groups[0]
		if firstBase == nil {
			firstBase = base.root
		}
		bMed := median(base.runs)
		bMin := minOf(base.runs)
		sumBase += bMed
		if !allEqual(base.rows) {
			// The default plan's own answer changed between runs, so no
			// plan can be compared with it.
			rowsVary = true
			for i := range ok {
				ok[i] = false
			}
		}
		byArm := make([]*planGroup, n)
		for _, g := range groups {
			for _, ai := range g.arms {
				if ai >= 0 {
					byArm[ai] = g
				}
			}
		}
		for i := 0; i < n; i++ {
			g := byArm[i]
			if g == nil || g.err != "" || g.timedOut || len(g.runs) == 0 {
				ok[i] = false
				if g != nil && g.timedOut {
					slower[i] = true
				}
				continue
			}
			if armRoot[i] == nil {
				armRoot[i] = g.root
			}
			m := median(g.runs)
			sumArm[i] += m
			if g.timedOut {
				slower[i] = true
			}
			if !rowsVary && !sameRows(g.rows, base.rows) {
				ok[i] = false
				r.Notes = appendOnce(r.Notes, fmt.Sprintf("%q returned a different number of rows; it was ruled out", a.opt.Arms[i].Name))
				continue
			}
			if m > bMed*(1+a.opt.Tolerance) {
				ok[i] = false
				slower[i] = true
			}
			if g != base && maxOf(g.runs) < bMin && m <= bMed*(1-a.opt.MinGain) {
				strictWin[i] = true
				helped[i] = true
			}
		}
	}
	r.Plans = maxPlans
	r.BaselineMs = sumBase
	best := -1
	for i := 0; i < n; i++ {
		if !ok[i] || !strictWin[i] {
			continue
		}
		if sumArm[i] > sumBase*(1-a.opt.MinGain) {
			continue
		}
		if best < 0 || a.better(i, best, sumArm) {
			best = i
		}
	}
	if best < 0 {
		r.Verdict = Keep
		mixed := false
		for i := 0; i < n; i++ {
			if helped[i] && slower[i] {
				mixed = true
			}
		}
		switch {
		case rowsVary:
			r.Reason = "the query returned a different number of rows on different runs, so no plan could be compared"
		case laterFailed != "":
			r.Reason = "a later text failed to run: " + laterFailed
		case mixed:
			r.Reason = "a hint helped one sample but was slower on another"
		case maxPlans <= 1:
			r.Reason = "no hint changed the plan"
		default:
			r.Reason = "no other plan was clearly faster"
		}
		return nil
	}
	arm := a.opt.Arms[best]
	r.Verdict = Faster
	r.Best = &ArmResult{
		Name:     arm.Name,
		Settings: arm.Settings,
		Lines:    arm.SetLocal(),
		Ms:       sumArm[best],
		Gain:     1 - sumArm[best]/sumBase,
		Change:   plan.Change(firstBase, armRoot[best]),
	}
	// Other arms that ran the very same plans on every sample.
	for i := 0; i < n; i++ {
		if i != best && ok[i] && math.Abs(sumArm[i]-sumArm[best]) < 1e-9 && strictWin[i] {
			r.Best.SamePlan = append(r.Best.SamePlan, a.opt.Arms[i].Name)
		}
	}
	return nil
}

func errTimeoutLike(s string) error {
	if strings.HasPrefix(s, "the default plan ran longer than") {
		return errTimeout
	}
	return nil
}

// sample plans the text under every arm, then times each distinct plan.
// groups[0] is always the default plan.
func (a *Advisor) sample(ctx context.Context, text string, wantQueryID bool, r *ShapeResult) (SampleResult, []*planGroup, error) {
	return a.sampleArms(ctx, text, wantQueryID, r, nil)
}

// Arms lists the hint sets this advisor tries, after dropping those the
// server lacks settings for. Trial takes indexes into it.
func (a *Advisor) Arms() []hints.Arm { return a.opt.Arms }

// Trial runs one text under the default plan and under the arms whose
// indexes are in want, as lqo advise does for each sample of a shape: plan
// each, then one warm-up and Repeat timed runs of each distinct plan, every
// one in its own READ ONLY transaction that is rolled back. It checks
// afterwards that no setting or advisory lock was left behind. The proxy's
// learner uses it to try a few hints at a time.
func (a *Advisor) Trial(ctx context.Context, text string, want []int) (SampleResult, error) {
	set := map[int]bool{}
	for _, i := range want {
		set[i] = true
	}
	sr, groups, err := a.sampleArms(ctx, text, false, &ShapeResult{}, set)
	if err != nil {
		return sr, err
	}
	if len(groups) > 0 {
		sr.BaseRowsVary = !allEqual(groups[0].rows)
		for i := range sr.Arms {
			for _, g := range groups {
				for _, ai := range g.arms {
					if ai >= 0 && a.opt.Arms[ai].Name == sr.Arms[i].Name {
						sr.Arms[i].RowsVary = len(g.rows) > 0 && !allEqual(g.rows)
					}
				}
			}
		}
	}
	if a.conn.Broken() {
		return sr, errors.New("lost the connection to the server")
	}
	return sr, a.checkNoLeak(ctx)
}

func (a *Advisor) sampleArms(ctx context.Context, text string, wantQueryID bool, r *ShapeResult, want map[int]bool) (SampleResult, []*planGroup, error) {
	sr := SampleResult{Text: text}
	// Plan only, no run: cheap, and tells which arms change the plan.
	planTimeout := a.opt.MaxTime
	baseExp, err := a.trial(ctx, text, nil, "plan", planTimeout)
	if err != nil {
		if ctx.Err() != nil || errors.Is(err, ErrUnsafe) {
			return sr, nil, err
		}
		sr.Error = errText(err)
		return sr, nil, nil
	}
	if wantQueryID {
		if v, err := a.trial(ctx, text, nil, "verbose", planTimeout); err == nil {
			if q := v.QueryID.String(); q != "" && q != "0" {
				r.QueryID = q
			}
		} else if ctx.Err() != nil || errors.Is(err, ErrUnsafe) {
			return sr, nil, err
		}
	}
	base := &planGroup{fp: plan.Fingerprint(baseExp.Plan), root: baseExp.Plan, arms: []int{-1}}
	groups := []*planGroup{base}
	byFP := map[string]*planGroup{base.fp: base}
	for i := range a.opt.Arms {
		if want != nil && !want[i] {
			continue
		}
		e, err := a.trial(ctx, text, &a.opt.Arms[i], "plan", planTimeout)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, ErrUnsafe) {
				return sr, nil, err
			}
			g := &planGroup{fp: "error", arms: []int{i}, err: errText(err)}
			groups = append(groups, g)
			continue
		}
		fp := plan.Fingerprint(e.Plan)
		if g, ok := byFP[fp]; ok {
			g.arms = append(g.arms, i)
			continue
		}
		g := &planGroup{fp: fp, root: e.Plan, arms: []int{i}}
		byFP[fp] = g
		groups = append(groups, g)
	}

	// Warm-up: one run of each plan, not counted. It also sets the time
	// limit for the hinted plans.
	warm, err := a.run(ctx, text, base, a.opt.MaxTime)
	if err != nil {
		return sr, nil, err
	}
	if base.timedOut {
		sr.Error = fmt.Sprintf("the default plan ran longer than %s", a.opt.MaxTime)
		return sr, nil, nil
	}
	if base.err != "" {
		sr.Error = base.err
		return sr, nil, nil
	}
	base.runs, base.rows = nil, nil
	limit := time.Duration(float64(time.Millisecond) * warm * a.opt.TimeoutFactor)
	if limit < a.opt.MinTimeout {
		limit = a.opt.MinTimeout
	}
	if limit > a.opt.MaxTime {
		limit = a.opt.MaxTime
	}
	for _, g := range groups[1:] {
		if g.err != "" {
			continue
		}
		if _, err := a.run(ctx, text, g, limit); err != nil {
			return sr, nil, err
		}
		g.runs, g.rows = nil, nil
	}

	// Timed rounds. Each round runs every plan once, in a shuffled order
	// that is the same for the same text every time.
	h := fnv.New64a()
	h.Write([]byte(text))
	rng := rand.New(rand.NewPCG(h.Sum64(), 42))
	for round := 0; round < a.opt.Repeat; round++ {
		order := make([]*planGroup, len(groups))
		copy(order, groups)
		rng.Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })
		for _, g := range order {
			if g.err != "" || g.timedOut {
				continue
			}
			lim := limit
			if g == base {
				lim = a.opt.MaxTime
			}
			if _, err := a.run(ctx, text, g, lim); err != nil {
				return sr, nil, err
			}
		}
		if base.timedOut {
			sr.Error = fmt.Sprintf("the default plan ran longer than %s", a.opt.MaxTime)
			return sr, nil, nil
		}
		if base.err != "" {
			sr.Error = base.err
			return sr, nil, nil
		}
	}

	sr.BaseRuns = base.runs
	sr.BaseMs = median(base.runs)
	if len(base.rows) > 0 {
		sr.BaseRows = base.rows[0]
	}
	sr.BaseSteps = plan.Steps(base.root)
	for _, g := range groups {
		for _, ai := range g.arms {
			if ai < 0 {
				continue
			}
			at := ArmTiming{Name: a.opt.Arms[ai].Name, Plan: g.fp, Same: g == base, TimedOut: g.timedOut, Error: g.err}
			if len(g.runs) > 0 && !g.timedOut {
				at.Runs = g.runs
				at.Ms = median(g.runs)
				at.Rows = g.rows[0]
			}
			sr.Arms = append(sr.Arms, at)
		}
	}
	sort.SliceStable(sr.Arms, func(i, j int) bool { return sr.Arms[i].Name < sr.Arms[j].Name })
	return sr, groups, nil
}

// run times one plan once and adds the result to its group. It returns the
// run's time. A run that times out marks the group as timed out.
func (a *Advisor) run(ctx context.Context, text string, g *planGroup, limit time.Duration) (float64, error) {
	var arm *hints.Arm
	if g.arms[0] >= 0 {
		arm = &a.opt.Arms[g.arms[0]]
	}
	e, err := a.trial(ctx, text, arm, "analyze", limit)
	if err != nil {
		if ctx.Err() != nil || errors.Is(err, ErrUnsafe) {
			return 0, err
		}
		if errors.Is(err, errTimeout) {
			g.timedOut = true
			return 0, nil
		}
		if a.conn.Broken() {
			return 0, err
		}
		g.err = errText(err)
		return 0, nil
	}
	// The analyzed plan must be the plan that was grouped. With the same
	// settings and statistics it is; if the statistics changed between
	// the two, don't trust this group.
	if fp := plan.Fingerprint(e.Plan); fp != g.fp {
		g.err = "the plan changed between planning and running; statistics may have been updated"
		return 0, nil
	}
	t := e.TotalMs()
	g.runs = append(g.runs, t)
	g.rows = append(g.rows, e.RootRows())
	return t, nil
}

func errText(err error) string {
	var pe *pgwire.PgError
	if errors.As(err, &pe) {
		return pe.Message
	}
	return err.Error()
}

func median(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	m := len(s) / 2
	if len(s)%2 == 1 {
		return s[m]
	}
	return (s[m-1] + s[m]) / 2
}

func minOf(v []float64) float64 {
	m := math.Inf(1)
	for _, x := range v {
		m = math.Min(m, x)
	}
	return m
}

func maxOf(v []float64) float64 {
	m := math.Inf(-1)
	for _, x := range v {
		m = math.Max(m, x)
	}
	return m
}

// sameRows reports whether every run of both plans returned the same
// number of rows.
func sameRows(a, b []float64) bool {
	if len(a) == 0 || len(b) == 0 || !allEqual(a) || !allEqual(b) {
		return false
	}
	return a[0] == b[0]
}

func allEqual(v []float64) bool {
	for _, x := range v {
		if x != v[0] {
			return false
		}
	}
	return true
}

// better reports whether arm i should replace arm j as the suggestion.
// Clearly faster wins (by more than 2%); within 2%, the arm that changes
// fewer settings wins, and with the same number, the faster one.
func (a *Advisor) better(i, j int, sum []float64) bool {
	if sum[i] < sum[j]*0.98 {
		return true
	}
	if sum[j] < sum[i]*0.98 {
		return false
	}
	ni, nj := len(a.opt.Arms[i].Settings), len(a.opt.Arms[j].Settings)
	if ni != nj {
		return ni < nj
	}
	return sum[i] < sum[j]
}

func appendOnce(list []string, s string) []string {
	for _, x := range list {
		if x == s {
			return list
		}
	}
	return append(list, s)
}
