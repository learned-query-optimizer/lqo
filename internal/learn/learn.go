// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

// Package learn decides, from live traffic, which hints make which query
// shapes faster. It takes the shapes the proxy has seen, slowest total time
// first, and tries a few hints at a time on a replica, with the texts the
// application actually ran. Which hints to try next is chosen by Thompson
// sampling: each hint's evidence so far is a distribution over how much
// faster it is, the learner draws from each and tries the most promising
// draws. Hints that look good get tried more; hints that look bad stop being
// tried; hints with little evidence still get their turn.
//
// A hint is proven only when it passes the same strict rule as lqo advise,
// over at least MinObs texts: never more than Tolerance slower on any of
// them, the same rows, no time-outs, clearly faster than every run of the
// default plan at least once, and confidently MinGain faster on average.
// In shadow mode, proven hints are reported and nothing else.
package learn

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"sort"
	"sync"
	"time"

	"github.com/learned-query-optimizer/lqo/internal/advisor"
	"github.com/learned-query-optimizer/lqo/internal/hints"
	"github.com/learned-query-optimizer/lqo/internal/pgwire"
	"github.com/learned-query-optimizer/lqo/internal/state"
	"github.com/learned-query-optimizer/lqo/internal/watch"
)

// Statuses of a learned shape.
const (
	Learning = "learning"
	Proven   = "proven"
	Keep     = "keep"
	Skipped  = "skipped"
	Failed   = "error"
)

// Options control the learner. Zero values get the defaults in Defaults.
type Options struct {
	Replica       *pgwire.Config  // where trials run; its database is replaced by each shape's
	AllowPrimary  bool            // let Replica be a primary
	HintSet       string          // standard or full
	Session       []hints.Setting // session settings for every trial, such as search_path
	ArmsPerRound  int             // hints tried per round
	Repeat        int             // timed runs per plan in a round
	MinObs        int             // texts a hint must be tried on before it can be proven
	MinGain       float64         // a proven hint saves at least this share
	Tolerance     float64         // and is never slower than this on any text
	MinMs         float64         // shapes faster than this live, at the median, aren't tried
	MinCalls      int64           // nor shapes seen fewer times than this
	MaxTime       time.Duration   // the default plan's runs stop after this
	Budget        float64         // share of the time the learner may keep the replica busy
	RetryKeep     time.Duration   // a shape with no better plan is tried again after this
	StatePath     string          // where findings are saved; empty for none
	SaveEvery     time.Duration
	Logf          func(format string, args ...any)
	TimeoutFactor float64
}

// Defaults are the settings lqo serve uses unless told otherwise.
var Defaults = Options{
	HintSet:       "standard",
	ArmsPerRound:  3,
	Repeat:        3,
	MinObs:        3,
	MinGain:       0.2,
	Tolerance:     0.1,
	MinMs:         5,
	MinCalls:      3,
	MaxTime:       30 * time.Second,
	Budget:        0.25,
	RetryKeep:     24 * time.Hour,
	SaveEvery:     30 * time.Second,
	TimeoutFactor: 3,
}

func (o *Options) fill() {
	d := Defaults
	if o.HintSet == "" {
		o.HintSet = d.HintSet
	}
	if o.ArmsPerRound <= 0 {
		o.ArmsPerRound = d.ArmsPerRound
	}
	if o.Repeat <= 0 {
		o.Repeat = d.Repeat
	}
	if o.MinObs <= 0 {
		o.MinObs = d.MinObs
	}
	if o.MinGain <= 0 || o.MinGain >= 1 {
		o.MinGain = d.MinGain
	}
	if o.Tolerance <= 0 {
		o.Tolerance = d.Tolerance
	}
	if o.MinMs < 0 {
		o.MinMs = 0
	}
	if o.MinCalls <= 0 {
		o.MinCalls = d.MinCalls
	}
	if o.MaxTime <= 0 {
		o.MaxTime = d.MaxTime
	}
	if o.Budget <= 0 || o.Budget > 1 {
		o.Budget = d.Budget
	}
	if o.RetryKeep <= 0 {
		o.RetryKeep = d.RetryKeep
	}
	if o.SaveEvery <= 0 {
		o.SaveEvery = d.SaveEvery
	}
	if o.TimeoutFactor <= 1 {
		o.TimeoutFactor = d.TimeoutFactor
	}
	if o.Logf == nil {
		o.Logf = func(string, ...any) {}
	}
}

// Learner runs trials on the replica and keeps what they show.
type Learner struct {
	opt Options
	col *watch.Collector
	rng *rand.Rand

	mu    sync.Mutex
	file  *state.File
	conns map[string]*replica // by database
	stats Stats
	dirty bool
}

type replica struct {
	conn *pgwire.Conn
	adv  *advisor.Advisor
}

// Stats are the learner's own counts.
type Stats struct {
	Rounds      int       `json:"rounds"`
	Trials      int       `json:"trials"`
	BusyMs      float64   `json:"busy_ms"`
	ReplicaDown bool      `json:"replica_down,omitempty"`
	LastError   string    `json:"last_error,omitempty"`
	LastErrorAt time.Time `json:"last_error_at,omitempty"`
	Proven      int       `json:"proven"`
}

// New returns a learner fed by col. It loads the state file if there is one;
// a damaged file is moved aside, as lqo advise does.
func New(col *watch.Collector, opt Options) (*Learner, string, error) {
	opt.fill()
	l := &Learner{opt: opt, col: col, conns: map[string]*replica{}, rng: rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 7))}
	note := ""
	if opt.StatePath != "" {
		f, n, err := state.LoadOrSetAside(opt.StatePath)
		if err != nil {
			return nil, "", err
		}
		l.file, note = f, n
	} else {
		l.file = state.New()
	}
	for _, s := range l.file.Serve {
		if s.Status == Proven {
			l.stats.Proven++
		}
	}
	return l, note, nil
}

// Run tries shapes until ctx ends, then saves the state.
func (l *Learner) Run(ctx context.Context) error {
	defer l.closeAll()
	defer l.Save()
	lastSave := time.Now()
	backoff := time.Second
	for ctx.Err() == nil {
		start := time.Now()
		worked, err := l.round(ctx)
		busy := time.Since(start)
		if time.Since(lastSave) > l.opt.SaveEvery {
			if err := l.Save(); err != nil {
				l.opt.Logf("saving the state: %v", err)
			}
			lastSave = time.Now()
		}
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			l.fail(err)
			if errors.Is(err, advisor.ErrUnsafe) {
				return err
			}
			// Most likely the replica is unreachable. Traffic isn't
			// affected; learning waits and tries again.
			sleep(ctx, backoff)
			if backoff < time.Minute {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second
		if !worked && l.Stats().ReplicaDown {
			// Nothing to try right now, but say as soon as the replica is
			// back rather than on the next round.
			if _, err := l.replicaFor(ctx, l.opt.Replica.Database); err != nil {
				l.fail(err)
			}
		}
		if !worked {
			sleep(ctx, time.Second)
			continue
		}
		// Keep the replica busy at most Budget of the time.
		idle := time.Duration(float64(busy) * (1/l.opt.Budget - 1))
		sleep(ctx, max(idle, 100*time.Millisecond))
	}
	return nil
}

func sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

func (l *Learner) fail(err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.stats.ReplicaDown = true
	l.stats.LastError = err.Error()
	l.stats.LastErrorAt = time.Now().UTC()
	for db, r := range l.conns {
		if r.conn.Broken() {
			r.conn.Close()
			delete(l.conns, db)
		}
	}
	l.opt.Logf("learning paused: %v", err)
}

func (l *Learner) closeAll() {
	l.mu.Lock()
	defer l.mu.Unlock()
	for db, r := range l.conns {
		r.conn.Close()
		delete(l.conns, db)
	}
}

// replicaFor connects to the replica's copy of db, once.
func (l *Learner) replicaFor(ctx context.Context, db string) (*replica, error) {
	l.mu.Lock()
	r := l.conns[db]
	l.mu.Unlock()
	if r != nil && !r.conn.Broken() {
		return r, nil
	}
	cfg := *l.opt.Replica
	cfg.Database = db
	if cfg.ApplicationName == "" {
		cfg.ApplicationName = "lqo serve"
	}
	conn, err := pgwire.Connect(ctx, &cfg)
	if err != nil {
		return nil, err
	}
	rep := &advisor.Report{}
	adv, err := advisor.New(ctx, conn, advisor.Options{
		HintSet:       l.opt.HintSet,
		Repeat:        l.opt.Repeat,
		MinGain:       l.opt.MinGain,
		Tolerance:     l.opt.Tolerance,
		TimeoutFactor: l.opt.TimeoutFactor,
		MaxTime:       l.opt.MaxTime,
		AllowPrimary:  l.opt.AllowPrimary,
		Session:       l.opt.Session,
	}, rep)
	if err != nil {
		conn.Close()
		return nil, err
	}
	r = &replica{conn: conn, adv: adv}
	l.mu.Lock()
	l.conns[db] = r
	l.stats.ReplicaDown = false
	l.mu.Unlock()
	return r, nil
}

// sync copies the collector's live numbers into the learned shapes.
func (l *Learner) sync(shapes []watch.Shape) {
	for _, s := range shapes {
		e := l.file.Serve[s.Key]
		if e == nil {
			e = &state.Learned{Key: s.Key, ID: s.ID, Database: s.Database, Normalized: s.Normalized, Status: Learning, Arms: map[string]*state.Arm{}}
			if s.Skip != "" {
				e.Status, e.Reason = Skipped, s.Skip
			}
			l.file.Serve[s.Key] = e
		}
		e.Calls, e.TotalMs, e.P50Ms, e.P95Ms, e.LastSeen = s.Calls, s.TotalMs, s.P50Ms, s.P95Ms, s.LastSeen
		if len(s.Samples) > 0 {
			e.Example = s.Samples[0].Text
		}
		if e.Arms == nil {
			e.Arms = map[string]*state.Arm{}
		}
	}
}

// pick chooses the shape to try next: the most live time, discounted by how
// much it has been tried already. Proven shapes are checked again now and
// then; shapes kept on the default plan only after RetryKeep.
func (l *Learner) pick(shapes []watch.Shape) (*state.Learned, watch.Shape, bool) {
	var best *state.Learned
	var bestShape watch.Shape
	bestScore := 0.0
	now := time.Now()
	for _, s := range shapes {
		e := l.file.Serve[s.Key]
		if e == nil || len(s.Samples) == 0 || s.Calls < l.opt.MinCalls || s.P50Ms < l.opt.MinMs || e.Disabled {
			continue
		}
		var w float64
		switch e.Status {
		case Learning:
			w = 1 / (1 + float64(e.Rounds)/4)
		case Proven:
			w = 0.1 / (1 + float64(e.Rounds)/4)
		case Keep:
			if now.Sub(e.LastTried) < l.opt.RetryKeep {
				continue
			}
			w = 0.05
		default:
			continue
		}
		if score := s.TotalMs * w; score > bestScore {
			best, bestShape, bestScore = e, s, score
		}
	}
	return best, bestShape, best != nil
}

// round tries one shape on one text with a few hints.
func (l *Learner) round(ctx context.Context) (bool, error) {
	shapes := l.col.Snapshot()
	l.mu.Lock()
	l.sync(shapes)
	e, sh, ok := l.pick(shapes)
	l.mu.Unlock()
	if !ok {
		return false, nil
	}
	r, err := l.replicaFor(ctx, sh.Database)
	if err != nil {
		return false, err
	}
	arms := r.adv.Arms()

	l.mu.Lock()
	text := sh.Samples[e.Rounds%len(sh.Samples)].Text
	if s := suspectText(e); s != "" {
		text = s
	}
	want := l.choose(e, arms, text)
	l.mu.Unlock()
	if len(want) == 0 {
		l.mu.Lock()
		l.settle(e, arms)
		l.mu.Unlock()
		return true, nil
	}

	start := time.Now()
	sr, err := r.adv.Trial(ctx, text, want, l.opt.ArmsPerRound)
	busy := time.Since(start)
	if err != nil {
		return true, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.stats.Rounds++
	for _, at := range sr.Arms {
		if !at.Same {
			l.stats.Trials++
		}
	}
	l.stats.BusyMs += float64(busy.Microseconds()) / 1000
	l.dirty = true
	e.Rounds++
	e.LastTried = time.Now().UTC()
	l.update(e, sr, arms)
	return true, nil
}

// choose draws from each eligible hint's evidence and returns every
// eligible hint, the most promising draws first: hints due a second look on
// this text, and a proven shape's hint, lead. The trial plans them all and
// runs the first ArmsPerRound distinct plans; hints whose plan is the
// default's are noted without a run.
func (l *Learner) choose(e *state.Learned, arms []hints.Arm, text string) []int {
	type cand struct {
		i    int
		draw float64
	}
	var cands []cand
	var out []int
	for i, a := range arms {
		st := e.Arms[a.Name]
		if st == nil {
			st = &state.Arm{}
			e.Arms[a.Name] = st
		}
		if st.Out != "" {
			continue
		}
		// Hints waiting for a second look on this text, and the proven
		// hint, go first.
		if (st.Suspect != "" && st.Suspect == text) || (e.Status == Proven && a.Name == e.Proven) {
			out = append(out, i)
			continue
		}
		if st.Suspect != "" {
			continue // its second look is on another text
		}
		cands = append(cands, cand{i, l.draw(st)})
	}
	sort.Slice(cands, func(x, y int) bool { return cands[x].draw < cands[y].draw })
	for _, c := range cands {
		out = append(out, c.i)
	}
	return out
}

// suspectText is a text some hint must be tried on again, if any.
func suspectText(e *state.Learned) string {
	names := make([]string, 0, len(e.Arms))
	for n := range e.Arms {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if st := e.Arms[n]; st.Out == "" && st.Suspect != "" {
			return st.Suspect
		}
	}
	return ""
}

// draw samples a plausible mean log ratio for a hint: hint time over default
// time, so below zero is faster. With no evidence it's a wide guess around
// "no change"; with evidence it narrows around what was seen.
func (l *Learner) draw(a *state.Arm) float64 {
	if a.N == 0 {
		return l.rng.NormFloat64() * 0.5
	}
	sd := 0.3
	if a.N >= 2 {
		sd = math.Sqrt(a.M2 / float64(a.N-1))
	}
	sd = max(sd, 0.05)
	return a.Mean + l.rng.NormFloat64()*sd/math.Sqrt(float64(a.N))
}

// update records one round's results and decides what they mean.
func (l *Learner) update(e *state.Learned, sr advisor.SampleResult, arms []hints.Arm) {
	if sr.Error != "" {
		e.Failures++
		if e.Failures >= 3 {
			l.unprove(e)
			e.Status, e.Reason = Failed, sr.Error
		}
		return
	}
	e.Failures = 0
	e.Texts++
	e.BaselineMs = sr.BaseMs
	if sr.BaseRowsVary {
		l.unprove(e)
		e.Status, e.Reason = Keep, "the query returned a different number of rows on different runs, so no plan could be compared"
		return
	}
	baseMin := math.Inf(1)
	for _, v := range sr.BaseRuns {
		baseMin = min(baseMin, v)
	}
	for _, at := range sr.Arms {
		st := e.Arms[at.Name]
		if st == nil || st.Out != "" {
			continue
		}
		st.Texts++
		// A bad result is confirmed on the same text before it counts.
		confirm := func(why string) {
			if st.Suspect == sr.Text {
				st.Out = why
				st.Suspect, st.SuspectWhy = "", ""
				return
			}
			st.Suspect, st.SuspectWhy = sr.Text, why
		}
		retest := st.Suspect == sr.Text
		switch {
		case at.Error != "":
			st.Out = "error: " + at.Error
		case at.TimedOut:
			confirm("it hit the time limit twice on the same text")
		case at.Same:
			st.SamePlan++
			observe(st, 0, 1)
			if st.SamePlan >= l.opt.MinObs && st.SamePlan == st.N {
				st.Out = "it doesn't change the plan"
			}
		case at.RowsVary || at.Rows != sr.BaseRows:
			st.Out = "it returned a different number of rows"
		case sr.BaseMs <= 0 || at.Ms <= 0:
			st.Out = "no usable time"
		default:
			ratio := at.Ms / sr.BaseMs
			if ratio > 1+l.opt.Tolerance {
				confirm(fmt.Sprintf("it was %.0f%% slower on a text, twice", (ratio-1)*100))
				break
			}
			if retest {
				// The second look was fine: the first was noise.
				st.Suspect, st.SuspectWhy = "", ""
			}
			observe(st, math.Log(ratio), ratio)
			slowest := 0.0
			for _, v := range at.Runs {
				slowest = max(slowest, v)
			}
			if slowest < baseMin && ratio <= 1-l.opt.MinGain {
				st.Wins++
			}
		}
		if st.Out != "" && e.Status == Proven && e.Proven == at.Name {
			l.unprove(e)
			e.Status, e.Reason = Learning, "the proven hint failed a later check ("+st.Out+"); trying the others again"
		}
	}
	l.settle(e, arms)
}

func observe(st *state.Arm, x, ratio float64) {
	st.N++
	d := x - st.Mean
	st.Mean += d / float64(st.N)
	st.M2 += d * (x - st.Mean)
	st.MaxRatio = max(st.MaxRatio, ratio)
}

// se is the standard error of a hint's mean log ratio, never under a floor
// so a few lucky identical runs can't make it look certain.
func se(st *state.Arm) float64 {
	sd := 0.3
	if st.N >= 2 {
		sd = math.Sqrt(st.M2 / float64(st.N-1))
	}
	return max(sd, 0.05) / math.Sqrt(float64(st.N))
}

// z is the one-sided 95% normal quantile.
const z = 1.645

// provable reports whether a hint meets the rule.
func (l *Learner) provable(st *state.Arm) bool {
	if st.Out != "" || st.Suspect != "" || st.N < l.opt.MinObs || st.Wins < 1 || st.N == st.SamePlan {
		return false
	}
	target := math.Log(1 - l.opt.MinGain)
	return st.Mean <= target && st.Mean+z*se(st) <= math.Log(1-l.opt.MinGain/2) && st.MaxRatio <= 1+l.opt.Tolerance
}

// settle proves the best provable hint, or decides the shape has none, or
// leaves it learning.
func (l *Learner) settle(e *state.Learned, arms []hints.Arm) {
	best := ""
	bestN := 0
	for _, a := range arms {
		st := e.Arms[a.Name]
		if st == nil || !l.provable(st) {
			continue
		}
		if best == "" {
			best, bestN = a.Name, len(a.Settings)
			continue
		}
		b := e.Arms[best]
		// Clearly faster wins; within 2%, fewer settings.
		switch {
		case st.Mean < b.Mean+math.Log(0.98):
			best, bestN = a.Name, len(a.Settings)
		case st.Mean <= b.Mean-math.Log(0.98) && len(a.Settings) < bestN:
			best, bestN = a.Name, len(a.Settings)
		}
	}
	if best != "" {
		if e.Status != Proven || e.Proven != best {
			for _, a := range arms {
				if a.Name == best {
					e.Settings = a.Settings
				}
			}
			if e.Status != Proven {
				l.stats.Proven++
			}
			e.Status, e.Reason, e.Proven, e.ProvenAt = Proven, "", best, time.Now().UTC()
			l.opt.Logf("proven: %s %s with %q, %.0f%% faster", e.Database, e.ID[:8], best, (1-math.Exp(e.Arms[best].Mean))*100)
		}
		e.Gain = 1 - math.Exp(e.Arms[best].Mean)
		return
	}
	if e.Status == Proven {
		return
	}
	// No better plan: every hint is ruled out, or confidently saves less
	// than MinGain.
	open := 0
	for _, a := range arms {
		st := e.Arms[a.Name]
		if st == nil || st.Out != "" {
			continue
		}
		if st.N >= l.opt.MinObs && (st.N == st.SamePlan || st.Mean-z*se(st) > math.Log(1-l.opt.MinGain)) {
			continue
		}
		open++
	}
	if open == 0 && e.Status == Learning {
		e.Status = Keep
		e.Reason = "no hint was clearly faster"
	}
}

func (l *Learner) unprove(e *state.Learned) {
	if e.Status == Proven {
		l.stats.Proven--
	}
	e.Proven, e.Settings, e.Gain = "", nil, 0
}

// Save writes the state file, if there is one.
func (l *Learner) Save() error {
	if l.opt.StatePath == "" {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.dirty {
		return nil
	}
	// lqo advise may have saved its own findings to the same file since;
	// keep those and replace only what the proxy learned.
	out := l.file
	if disk, err := state.Load(l.opt.StatePath); err == nil {
		disk.Serve = l.file.Serve
		out = disk
	}
	if err := out.Save(l.opt.StatePath); err != nil {
		return err
	}
	l.dirty = false
	return nil
}

// Shapes copies the learned shapes, proven first, then by live time, with
// the live counts brought up to date.
func (l *Learner) Shapes() []state.Learned {
	live := l.col.Snapshot()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sync(live)
	out := make([]state.Learned, 0, len(l.file.Serve))
	for _, e := range l.file.Serve {
		cp := *e
		cp.Arms = make(map[string]*state.Arm, len(e.Arms))
		for k, v := range e.Arms {
			a := *v
			cp.Arms[k] = &a
		}
		out = append(out, cp)
	}
	sort.Slice(out, func(i, j int) bool {
		if (out[i].Status == Proven) != (out[j].Status == Proven) {
			return out[i].Status == Proven
		}
		if out[i].TotalMs != out[j].TotalMs {
			return out[i].TotalMs > out[j].TotalMs
		}
		return out[i].Key < out[j].Key
	})
	return out
}

// Stats reports the learner's counts.
func (l *Learner) Stats() Stats {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.stats
}
