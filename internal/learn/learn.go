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
	"strings"
	"sync"
	"time"

	"github.com/learned-query-optimizer/lqo/internal/advisor"
	"github.com/learned-query-optimizer/lqo/internal/hints"
	"github.com/learned-query-optimizer/lqo/internal/pgwire"
	"github.com/learned-query-optimizer/lqo/internal/sqltext"
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

	// Active applies proven hints to live queries; otherwise the learner
	// only reports (shadow mode).
	Active bool
	// Holdout is the share of a hinted shape's calls run without the hint,
	// to compare against.
	Holdout float64
	// GuardCalls is how many hinted calls the guard waits for before it
	// judges a hint live.
	GuardCalls int64
	// NoShare stops shapes from starting with their relatives' results.
	NoShare bool
	// JoinHints is passed to the advisor: auto, on or off.
	JoinHints string
	// ShareWeight is how many texts' worth of evidence the relatives' results
	// count for, at most, when a shape's hints are drawn.
	ShareWeight float64
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
	Holdout:       0.1,
	GuardCalls:    20,
	ShareWeight:   2,
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
	if o.Holdout < 0 || o.Holdout >= 1 {
		o.Holdout = d.Holdout
	}
	if o.GuardCalls <= 0 {
		o.GuardCalls = d.GuardCalls
	}
	if o.ShareWeight <= 0 {
		o.ShareWeight = d.ShareWeight
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

	saveFailed bool // the state file can't be written: apply nothing
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
	// In active mode: hints applied now, hints the guard took back, and
	// why nothing is applied, when nothing is.
	Applied  int    `json:"applied"`
	Reverted int    `json:"reverted"`
	Paused   string `json:"paused,omitempty"`
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

// Run tries shapes until ctx ends, then saves the state. In active mode it
// also watches the hints in use and takes back any that run slower live.
func (l *Learner) Run(ctx context.Context) error {
	defer l.closeAll()
	defer l.Save()
	defer l.col.SetActive(nil) // stopping: apply nothing
	l.Publish()
	stopGuard := make(chan struct{})
	defer close(stopGuard)
	if l.opt.Active {
		go func() {
			t := time.NewTicker(250 * time.Millisecond)
			defer t.Stop()
			for {
				select {
				case <-stopGuard:
					return
				case <-t.C:
					l.Guard()
				}
			}
		}()
	}
	lastSave := time.Now()
	backoff := time.Second
	for ctx.Err() == nil {
		start := time.Now()
		worked, err := l.round(ctx)
		busy := time.Since(start)
		l.Publish()
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
	l.publishLocked()
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
		JoinHints:     l.opt.JoinHints,
	}, rep)
	if err != nil {
		conn.Close()
		return nil, err
	}
	r = &replica{conn: conn, adv: adv}
	l.mu.Lock()
	l.conns[db] = r
	wasDown := l.stats.ReplicaDown
	l.stats.ReplicaDown = false
	if wasDown {
		l.publishLocked()
	}
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
		if e.Family == "" && e.Normalized != "" {
			e.Family = sqltext.Family(e.Normalized)
		}
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
	static := r.adv.Arms()

	l.mu.Lock()
	extras := joinArms(e)
	arms := append(append([]hints.Arm(nil), static...), extras...)
	text := sh.Samples[e.Rounds%len(sh.Samples)].Text
	if s := suspectText(e); s != "" {
		text = s
	}
	want, focus := l.choose(e, arms, text)
	l.mu.Unlock()
	if len(want) == 0 {
		l.mu.Lock()
		l.settle(e, arms)
		l.mu.Unlock()
		return true, nil
	}

	start := time.Now()
	timed := l.opt.ArmsPerRound
	if focus {
		// A relative proved this hint: try it alone first. If it holds on
		// three texts, that's proof at a third of the replica time; if not,
		// its head start fades and the shape is back to a few at a time.
		timed = 1
	}
	r.adv.SetExtra(extras)
	sr, err := r.adv.Trial(ctx, text, want, timed)
	busy := time.Since(start)
	if err != nil {
		return true, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(e.JoinHints) == 0 {
		// The trial built this shape's row-count hints from what the
		// default plan measured: they're its own hints from now on.
		for _, x := range r.adv.Extra() {
			e.JoinHints = append(e.JoinHints, state.JoinHint{Name: x.Name, Comment: x.Comment})
			if e.Arms[x.Name] == nil {
				e.Arms[x.Name] = &state.Arm{}
			}
		}
		arms = append(append([]hints.Arm(nil), static...), joinArms(e)...)
	}
	l.stats.Rounds++
	// A trial is a plan run, however many hints gave that plan.
	plans := map[string]bool{}
	for _, at := range sr.Arms {
		if !at.Same && at.Plan != "" && !plans[at.Plan] {
			plans[at.Plan] = true
			l.stats.Trials++
			e.Trials++
		}
	}
	ms := float64(busy.Microseconds()) / 1000
	l.stats.BusyMs += ms
	e.BusyMs += ms
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
//
// focus is true when the shape should time only the first: its relatives
// proved that hint, and it has been a clear win on every one of the shape's
// own texts so far.
func (l *Learner) choose(e *state.Learned, arms []hints.Arm, text string) (want []int, focus bool) {
	type cand struct {
		i    int
		draw float64
	}
	var cands []cand
	var out []int
	prior, fav := l.priors(e)
	if fav != "" && e.Status == Learning && suspectText(e) == "" {
		for i, a := range arms {
			st := e.Arms[a.Name]
			if a.Name != fav || (st != nil && (st.Out != "" || st.Suspect != "")) {
				continue
			}
			if st != nil && st.Wins < st.N-st.SamePlan {
				continue // not a clear win on every text so far
			}
			if st == nil {
				e.Arms[a.Name] = &state.Arm{}
			}
			out = append(out, i)
			focus = true
		}
	}
	for i, a := range arms {
		st := e.Arms[a.Name]
		if st == nil {
			st = &state.Arm{}
			e.Arms[a.Name] = st
		}
		if st.Out != "" || (focus && a.Name == fav) {
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
		cands = append(cands, cand{i, l.draw(st, prior[a.Name])})
	}
	sort.Slice(cands, func(x, y int) bool { return cands[x].draw < cands[y].draw })
	for _, c := range cands {
		out = append(out, c.i)
	}
	return out, focus
}

// joinArms are the row-count hints built for e, as arms.
func joinArms(e *state.Learned) []hints.Arm {
	out := make([]hints.Arm, 0, len(e.JoinHints))
	for _, j := range e.JoinHints {
		out = append(out, hints.Arm{Name: j.Name, Comment: j.Comment})
	}
	return out
}

// cost is how much an arm changes, to break ties between equally fast
// arms: one per setting, and one per row-count hint plus one, so a plain
// switch wins a tie with a comment.
func cost(a hints.Arm) int {
	if a.Comment != "" {
		return strings.Count(a.Comment, "Rows(") + 1
	}
	return len(a.Settings)
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
// "no change"; with evidence it narrows around what was seen. A prior from
// the shape's relatives counts as p.n texts' worth of evidence at p.mean
// before the shape has any of its own, and less with each of its own. It
// only orders the draws: proof takes the shape's own results.
func (l *Learner) draw(a *state.Arm, p prior) float64 {
	if a.N == 0 && p.n == 0 {
		return l.rng.NormFloat64() * 0.5
	}
	sd := 0.3
	if a.N >= 2 {
		sd = math.Sqrt(a.M2 / float64(a.N-1))
	}
	sd = max(sd, 0.05)
	// The head start fades as the shape's own texts come in.
	pn := p.n / float64(1+a.N)
	n := float64(a.N) + pn
	mean := (a.Mean*float64(a.N) + p.mean*pn) / n
	return mean + l.rng.NormFloat64()*sd/math.Sqrt(n)
}

// prior is what a shape's relatives say about one hint.
type prior struct {
	mean float64 // mean log ratio over the texts where the plan changed
	n    float64 // how many texts' worth it counts for
}

// slowerPrior stands for a hint a relative ruled out: about 20% slower.
var slowerPrior = math.Log(1.2)

// priors pools, hint by hint, the results of the shapes in e's family: other
// shapes on the same database that read the same tables. Texts where a hint
// didn't change the plan say nothing about how it does when it does, so
// they're left out. A hint a relative ruled out for being slower, timing out
// or changing the rows counts as one text 20% slower. All of it together
// counts for at most ShareWeight texts, so a shape's own results soon
// outweigh it.
//
// fav is a hint some relative proved, the one with the best pooled mean, if
// that mean saves at least MinGain: the hint worth trying alone first.
func (l *Learner) priors(e *state.Learned) (out map[string]prior, fav string) {
	e.HeadStart = 0
	if l.opt.NoShare || e.Family == "" {
		return nil, ""
	}
	sum := map[string]float64{}
	w := map[string]float64{}
	proven := map[string]bool{}
	for _, o := range l.file.Serve {
		if o.Key == e.Key || o.Family != e.Family || o.Database != e.Database {
			continue
		}
		if o.Status == Proven && o.Proven != "" {
			proven[o.Proven] = true
		}
		used := false
		for name, a := range o.Arms {
			switch {
			case a.Out != "" && a.Out != "it doesn't change the plan" && !strings.HasPrefix(a.Out, "error"):
				sum[name] += slowerPrior
				w[name]++
				used = true
			case a.N > a.SamePlan:
				k := float64(a.N - a.SamePlan)
				sum[name] += a.Mean * float64(a.N) // same-plan texts added 0
				w[name] += k
				used = true
			case a.SamePlan > 0:
				// It never changed a relative's plan: most likely it
				// won't change this one's much either.
				w[name] += float64(a.SamePlan)
				used = true
			}
		}
		if used {
			e.HeadStart++
		}
	}
	if len(w) == 0 {
		return nil, ""
	}
	total := 0.0
	for _, v := range w {
		total = max(total, v)
	}
	scale := min(1, l.opt.ShareWeight/total)
	out = make(map[string]prior, len(w))
	best := math.Log(1 - l.opt.MinGain)
	for name, v := range w {
		out[name] = prior{mean: sum[name] / v, n: v * scale}
		if proven[name] && out[name].mean <= best {
			best, fav = out[name].mean, name
		}
	}
	return out, fav
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
			st.SumMs += at.Ms
			st.SumBaseMs += sr.BaseMs
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
	// Saves MinGain of the time over the texts, added up as lqo advise
	// adds them, and is confidently faster on each text, on average.
	if st.SumBaseMs <= 0 || st.SumMs > st.SumBaseMs*(1-l.opt.MinGain) {
		return false
	}
	return st.Mean+z*se(st) <= math.Log(1-l.opt.MinGain/2) && st.MaxRatio <= 1+l.opt.Tolerance
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
			best, bestN = a.Name, cost(a)
			continue
		}
		b := e.Arms[best]
		// Clearly faster wins; within 2%, fewer settings.
		switch {
		case st.Mean < b.Mean+math.Log(0.98):
			best, bestN = a.Name, cost(a)
		case st.Mean <= b.Mean-math.Log(0.98) && cost(a) < bestN:
			best, bestN = a.Name, cost(a)
		}
	}
	if best != "" {
		if e.Status != Proven || e.Proven != best {
			for _, a := range arms {
				if a.Name == best {
					e.Settings, e.Comment = a.Settings, a.Comment
				}
			}
			if e.Status != Proven {
				l.stats.Proven++
			}
			if e.RoundsToProof == 0 {
				e.RoundsToProof, e.TrialsToProof, e.BusyToProofMs = e.Rounds, e.Trials, e.BusyMs
			}
			e.Status, e.Reason, e.Proven, e.ProvenAt = Proven, "", best, time.Now().UTC()
			l.opt.Logf("proven: %s %s with %q, %.0f%% faster", e.Database, e.ID[:8], best, (1-e.Arms[best].SumMs/e.Arms[best].SumBaseMs)*100)
		}
		e.Gain = 1 - e.Arms[best].SumMs/e.Arms[best].SumBaseMs
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
	e.Proven, e.Settings, e.Comment, e.Gain = "", nil, "", 0
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
		disk.ServeHintsOff = l.file.ServeHintsOff
		out = disk
	}
	if err := out.Save(l.opt.StatePath); err != nil {
		if !l.saveFailed {
			l.saveFailed = true
			l.publishLocked()
		}
		return err
	}
	if l.saveFailed {
		l.saveFailed = false
		l.publishLocked()
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

// Publish hands the collector the hints to apply now.
func (l *Learner) Publish() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.publishLocked()
}

// publishLocked works out the active set. Nothing is applied in shadow
// mode, with the master switch off, while the replica can't be reached, or
// while the state file can't be written: LQO then passes every query
// through as it came.
func (l *Learner) publishLocked() {
	set := &watch.ActiveSet{Hints: map[string]*watch.Hint{}, Holdout: l.opt.Holdout, Off: l.file.ServeHintsOff}
	paused := ""
	switch {
	case !l.opt.Active:
		paused = "shadow mode"
	case l.file.ServeHintsOff:
		paused = "switched off"
	case l.stats.ReplicaDown:
		paused = "the replica can't be reached"
	case l.saveFailed:
		paused = "the state file can't be written"
	}
	if paused == "" {
		for key, e := range l.file.Serve {
			if e.Status == Proven && !e.Disabled && e.Proven != "" {
				set.Hints[key] = watch.NewHint(e.Proven, e.Settings, e.Comment)
			}
		}
	}
	l.stats.Paused = paused
	l.stats.Applied = len(set.Hints)
	l.col.SetActive(set)
}

// Guard compares each applied hint's live times with the calls held out
// without it (or, until there are enough of those, with the shape's times
// before the hint), and takes back a hint whose median is more than
// Tolerance slower. The hint is ruled out for the shape, which goes back to
// learning.
func (l *Learner) Guard() {
	if !l.opt.Active {
		return
	}
	shapes := l.col.Snapshot()
	l.mu.Lock()
	defer l.mu.Unlock()
	changed := false
	for _, s := range shapes {
		if !s.Active || s.HintedCalls < l.opt.GuardCalls {
			continue
		}
		ref, against := s.BeforeP50Ms, "before the hint"
		if s.HoldoutCalls >= 5 {
			ref, against = s.HoldoutP50Ms, "without it"
		}
		if ref <= 0 || s.HintedP50Ms <= ref*(1+l.opt.Tolerance) {
			continue
		}
		e := l.file.Serve[s.Key]
		if e == nil || e.Status != Proven {
			continue
		}
		why := fmt.Sprintf("live median %s ms with %q against %s ms %s", watch.FormatMs(s.HintedP50Ms), e.Proven, watch.FormatMs(ref), against)
		if st := e.Arms[e.Proven]; st != nil {
			st.Out = "slower live: " + why
		}
		l.opt.Logf("guard: took back %q for %s %s: %s", e.Proven, e.Database, e.ID[:8], why)
		l.unprove(e)
		e.Status, e.Reason = Learning, "the guard took the hint back: "+why
		e.Reverted, e.RevertedAt = why, time.Now().UTC()
		l.stats.Reverted++
		l.dirty = true
		changed = true
	}
	if changed {
		l.publishLocked()
	}
}

// ErrNoShape means a switch named a shape LQO doesn't know, or more than one.
var ErrNoShape = errors.New("no single shape matches")

// SwitchAll turns every hint off, or back on. It's saved in the state file,
// so it holds across restarts.
func (l *Learner) SwitchAll(off bool) error {
	l.mu.Lock()
	l.file.ServeHintsOff = off
	l.dirty = true
	l.publishLocked()
	l.mu.Unlock()
	return l.Save()
}

// SwitchShape turns one shape's hint off, or back on. id is the shape ID, a
// prefix of it, or database/ID.
func (l *Learner) SwitchShape(id string, off bool) (string, error) {
	l.mu.Lock()
	var match *state.Learned
	for key, e := range l.file.Serve {
		if key == id || e.ID == id || (len(id) >= 4 && strings.HasPrefix(e.ID, id)) {
			if match != nil {
				l.mu.Unlock()
				return "", fmt.Errorf("%w: %q matches more than one; give more of the ID", ErrNoShape, id)
			}
			match = e
		}
	}
	if match == nil {
		l.mu.Unlock()
		return "", fmt.Errorf("%w: %q", ErrNoShape, id)
	}
	match.Disabled = off
	l.dirty = true
	l.publishLocked()
	key := match.Key
	l.mu.Unlock()
	return key, l.Save()
}
