// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

// Package watch measures the traffic the proxy relays. Each session follows
// its client's requests and the server's replies, and reports every finished
// statement, with its time, to a Collector. The Collector groups statements
// into shapes, as lqo advise does, and keeps per-shape counts, times and a
// few complete texts, values included, for the learner to try on a replica.
//
// Nothing here may slow the traffic down. Sessions hand their reports over a
// buffered channel and never wait: when the Collector falls behind, reports
// are dropped and counted, and the relay carries on.
package watch

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/learned-query-optimizer/lqo/internal/hints"
	"github.com/learned-query-optimizer/lqo/internal/proxy"
	"github.com/learned-query-optimizer/lqo/internal/sqltext"
)

// MaxSamples is the number of complete texts kept per shape.
const MaxSamples = 8

// maxCache bounds the cache of statement texts already grouped.
const maxCache = 20000

// maxShapes bounds the shapes tracked; the least used go first.
const maxShapes = 5000

type event struct {
	db, user string
	text     string
	params   []string
	noText   bool
	ms       float64
	err      bool
	at       time.Time
	hinted   bool
	holdout  bool
}

// Sample is one complete text of a shape and the time it took live.
type Sample struct {
	Text string  `json:"text"`
	Ms   float64 `json:"ms"`
}

// Shape is what the proxy has seen of one query shape in one database.
type Shape struct {
	Key        string    `json:"key"` // database and shape ID
	ID         string    `json:"id"`
	Database   string    `json:"database"`
	Normalized string    `json:"normalized"`
	Skip       string    `json:"skip,omitempty"` // why LQO won't try it, if it won't
	Calls      int64     `json:"calls"`
	Errors     int64     `json:"errors,omitempty"`
	TotalMs    float64   `json:"total_ms"`
	P50Ms      float64   `json:"p50_ms"`
	P95Ms      float64   `json:"p95_ms"`
	FirstSeen  time.Time `json:"first_seen"`
	LastSeen   time.Time `json:"last_seen"`
	Samples    []Sample  `json:"samples,omitempty"`

	// In active mode, since the shape's hint was switched on: calls run
	// with the hint, calls held out without it, and the median before.
	Active       bool    `json:"active,omitempty"`
	HintedCalls  int64   `json:"hinted_calls,omitempty"`
	HintedP50Ms  float64 `json:"hinted_p50_ms,omitempty"`
	HoldoutCalls int64   `json:"holdout_calls,omitempty"`
	HoldoutP50Ms float64 `json:"holdout_p50_ms,omitempty"`
	BeforeP50Ms  float64 `json:"before_p50_ms,omitempty"`

	hist [histBuckets]int64
	// The latest times, exact, for the guard's comparisons.
	recent, hintedRecent, holdoutRecent ring
}

// ringSize is how many of the latest times a shape keeps exactly.
const ringSize = 200

type ring struct {
	v    []float64
	next int
}

func (r *ring) add(ms float64) {
	if len(r.v) < ringSize {
		r.v = append(r.v, ms)
		return
	}
	r.v[r.next] = ms
	r.next = (r.next + 1) % ringSize
}

func (r *ring) median() float64 {
	if len(r.v) == 0 {
		return 0
	}
	c := append([]float64(nil), r.v...)
	sort.Float64s(c)
	if len(c)%2 == 1 {
		return c[len(c)/2]
	}
	return (c[len(c)/2-1] + c[len(c)/2]) / 2
}

func quantileOf(h *[histBuckets]int64, n int64, q float64) float64 {
	if n == 0 {
		return 0
	}
	target := int64(math.Ceil(q * float64(n)))
	var c int64
	for b, v := range h {
		c += v
		if c >= target {
			return bucketMs(b)
		}
	}
	return bucketMs(histBuckets - 1)
}

// histBuckets times, in quarter-powers of two from 10 µs to about 3 hours.
const histBuckets = 140

func bucket(ms float64) int {
	if ms <= 0.01 {
		return 0
	}
	b := int(math.Log2(ms/0.01) * 4)
	if b >= histBuckets {
		b = histBuckets - 1
	}
	return b
}

func bucketMs(b int) float64 { return 0.01 * math.Pow(2, (float64(b)+0.5)/4) }

func (s *Shape) quantile(q float64) float64 { return quantileOf(&s.hist, s.Calls, q) }

type grouped struct {
	id, normalized, skip string
}

// Collector gathers the reports of every session.
type Collector struct {
	ch      chan event
	dropped atomic.Int64
	// noHintPlan counts calls a hint comment was put in front of, in a
	// session without pg_hint_plan: they're counted as unhinted.
	noHintPlan atomic.Int64
	seen       atomic.Int64
	inflight   atomic.Int64

	mu     sync.Mutex
	cache  map[string]grouped // statement text to its shape
	shapes map[string]*Shape
	done   chan struct{}

	// active is the hints sessions apply now; nil or empty in shadow mode.
	active atomic.Pointer[ActiveSet]
	// fps caches statement texts' shape IDs for the relay's own lookups.
	fps  sync.Map
	fpsN atomic.Int64
}

// Hint is a proven hint, ready to apply.
type Hint struct {
	Name     string
	Settings []hints.Setting
	// Comment is pg_hint_plan hints, for a hint LQO built from measured
	// row counts; they go in a /*+ ... */ comment at the start of the
	// query.
	Comment string
	// SQL sets the hint's settings for the current transaction only, and
	// makes the planner build a fresh plan for prepared statements, so a
	// cached plan made without the hint isn't reused, and none made with
	// it is kept. With a Comment, its last column also says whether
	// pg_hint_plan is loaded in the session.
	SQL string
}

// NewHint prepares a hint to apply.
func NewHint(name string, settings []hints.Setting, comment string) *Hint {
	parts := make([]string, 0, len(settings)+2)
	for _, st := range settings {
		parts = append(parts, "set_config("+quoted(st.Name, 0)+", "+quoted(st.Value, 0)+", true)")
	}
	parts = append(parts, "set_config('plan_cache_mode', 'force_custom_plan', true)")
	if comment != "" {
		parts = append(parts, "coalesce(current_setting('pg_hint_plan.enable_hint', true), 'off') = 'on'")
	}
	return &Hint{Name: name, Settings: settings, Comment: comment, SQL: "SELECT " + strings.Join(parts, ", ")}
}

// prefix is what goes in front of the query's own text for a hint with a
// comment.
func (h *Hint) prefix() string {
	if h.Comment == "" {
		return ""
	}
	return "/*+ " + h.Comment + " */ "
}

// same reports whether two hints apply the very same thing.
func (h *Hint) same(o *Hint) bool { return h.SQL == o.SQL && h.Comment == o.Comment }

// ActiveSet is what sessions apply: hints by database and shape ID, the
// share of calls held out to compare against, and the master switch.
type ActiveSet struct {
	Hints   map[string]*Hint
	Holdout float64
	Off     bool
}

// SetActive replaces the hints sessions apply. A shape whose hint is new
// starts its with-and-without counts afresh.
func (c *Collector) SetActive(set *ActiveSet) {
	old := c.active.Load()
	c.mu.Lock()
	for key, s := range c.shapes {
		on := set != nil && !set.Off && set.Hints[key] != nil
		was := old != nil && !old.Off && old.Hints[key] != nil && (set == nil || set.Hints[key] == nil || old.Hints[key].same(set.Hints[key]))
		if on && !was {
			s.BeforeP50Ms = s.recent.median()
			s.HintedCalls, s.HoldoutCalls = 0, 0
			s.hintedRecent, s.holdoutRecent = ring{}, ring{}
		}
		s.Active = on
	}
	c.mu.Unlock()
	c.active.Store(set)
}

// fingerprint is sqltext.Fingerprint with a cache, for the relay.
func (c *Collector) fingerprint(text string) string {
	if v, ok := c.fps.Load(text); ok {
		return v.(string)
	}
	fp := sqltext.Fingerprint(text)
	if c.fpsN.Add(1) > maxCache {
		c.fps.Clear()
		c.fpsN.Store(0)
	}
	c.fps.Store(text, fp)
	return fp
}

// NewCollector starts a collector. Close stops it.
func NewCollector() *Collector {
	c := &Collector{
		ch:     make(chan event, 8192),
		cache:  map[string]grouped{},
		shapes: map[string]*Shape{},
		done:   make(chan struct{}),
	}
	go c.loop()
	return c
}

var _ proxy.Watcher = (*Collector)(nil)

// Open starts watching a session; the proxy calls it.
func (c *Collector) Open(params map[string]string) proxy.Session {
	db := params["database"]
	if db == "" {
		db = params["user"]
	}
	// The session opens right after the server said it was ready, idle.
	return &session{c: c, db: db, user: params["user"], stmts: map[string]*statement{}, portals: map[string]*pending{}, txStatus: 'I'}
}

func (c *Collector) record(e event) {
	c.inflight.Add(1)
	select {
	case c.ch <- e:
	default:
		c.inflight.Add(-1)
		c.dropped.Add(1)
	}
}

// Close stops the collector once the reports already handed over are in.
func (c *Collector) Close() {
	close(c.ch)
	<-c.done
}

// Flush waits until every report handed over so far is counted. Tests use it.
func (c *Collector) Flush() {
	for c.inflight.Load() > 0 {
		time.Sleep(time.Millisecond)
	}
}

// Stats are the collector's own counts.
type Stats struct {
	Statements int64 `json:"statements"`
	Dropped    int64 `json:"dropped"`
	Shapes     int   `json:"shapes"`
	// NoHintPlan counts calls that would have had a join hint, in a
	// session where pg_hint_plan wasn't loaded.
	NoHintPlan int64 `json:"no_pg_hint_plan,omitempty"`
}

// Stats reports how much the collector has seen and dropped.
func (c *Collector) Stats() Stats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return Stats{Statements: c.seen.Load(), Dropped: c.dropped.Load(), Shapes: len(c.shapes), NoHintPlan: c.noHintPlan.Load()}
}

func (c *Collector) loop() {
	defer close(c.done)
	for e := range c.ch {
		c.add(e)
		c.inflight.Add(-1)
	}
}

func (c *Collector) add(e event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seen.Add(1)
	g, ok := c.cache[e.text]
	if !ok {
		if len(c.cache) >= maxCache {
			c.cache = map[string]grouped{}
		}
		g = grouped{
			id:         sqltext.Fingerprint(e.text),
			normalized: sqltext.Normalize(e.text),
			skip:       sqltext.ClassifyTemplate(e.text),
		}
		c.cache[e.text] = g
	}
	key := e.db + "/" + g.id
	s := c.shapes[key]
	if s == nil {
		if len(c.shapes) >= maxShapes {
			c.evict()
		}
		s = &Shape{Key: key, ID: g.id, Database: e.db, Normalized: g.normalized, Skip: g.skip, FirstSeen: e.at}
		if set := c.active.Load(); set != nil && !set.Off && set.Hints[key] != nil {
			s.Active = true // hinted from its first call: no "before"
		}
		c.shapes[key] = s
	}
	s.Calls++
	s.TotalMs += e.ms
	s.LastSeen = e.at
	s.hist[bucket(e.ms)]++
	switch {
	case e.hinted:
		s.HintedCalls++
		s.hintedRecent.add(e.ms)
	case e.holdout:
		s.HoldoutCalls++
		s.holdoutRecent.add(e.ms)
	default:
		s.recent.add(e.ms)
	}
	if e.err {
		s.Errors++
		return
	}
	if s.Skip != "" || e.noText {
		return
	}
	text := e.text
	if len(e.params) > 0 {
		m := make(map[int]string, len(e.params))
		for i, p := range e.params {
			m[i+1] = p
		}
		text = sqltext.Substitute(e.text, m)
	}
	keepSample(s, Sample{Text: sqltext.TrimEnd(text), Ms: e.ms})
}

// keepSample keeps up to MaxSamples distinct texts, the slowest ones seen,
// since slow calls are where a better plan matters most.
func keepSample(s *Shape, smp Sample) {
	for i := range s.Samples {
		if s.Samples[i].Text == smp.Text {
			if smp.Ms > s.Samples[i].Ms {
				s.Samples[i].Ms = smp.Ms
			}
			return
		}
	}
	if len(s.Samples) < MaxSamples {
		s.Samples = append(s.Samples, smp)
		return
	}
	low := 0
	for i := range s.Samples {
		if s.Samples[i].Ms < s.Samples[low].Ms {
			low = i
		}
	}
	if smp.Ms > s.Samples[low].Ms {
		s.Samples[low] = smp
	}
}

// evict drops the tenth of shapes with the least total time.
func (c *Collector) evict() {
	all := make([]*Shape, 0, len(c.shapes))
	for _, s := range c.shapes {
		all = append(all, s)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].TotalMs < all[j].TotalMs })
	for _, s := range all[:len(all)/10+1] {
		delete(c.shapes, s.Key)
	}
}

// Snapshot copies every shape, the most total time first.
func (c *Collector) Snapshot() []Shape {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Shape, 0, len(c.shapes))
	for _, s := range c.shapes {
		cp := *s
		cp.P50Ms = s.quantile(0.5)
		cp.P95Ms = s.quantile(0.95)
		cp.HintedP50Ms = s.hintedRecent.median()
		cp.HoldoutP50Ms = s.holdoutRecent.median()
		cp.recent, cp.hintedRecent, cp.holdoutRecent = ring{}, ring{}, ring{}
		cp.Samples = append([]Sample(nil), s.Samples...)
		sort.Slice(cp.Samples, func(i, j int) bool { return cp.Samples[i].Ms > cp.Samples[j].Ms })
		out = append(out, cp)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TotalMs != out[j].TotalMs {
			return out[i].TotalMs > out[j].TotalMs
		}
		return out[i].Key < out[j].Key
	})
	return out
}

// Get copies one shape.
func (c *Collector) Get(key string) (Shape, bool) {
	c.mu.Lock()
	s := c.shapes[key]
	c.mu.Unlock()
	if s == nil {
		return Shape{}, false
	}
	for _, sh := range c.Snapshot() {
		if sh.Key == key {
			return sh, true
		}
	}
	return Shape{}, false
}

// FormatMs is a time for people: 0.21, 35.1, 1,061.
func FormatMs(ms float64) string {
	switch {
	case ms < 1:
		return strconv.FormatFloat(ms, 'f', 2, 64)
	case ms < 100:
		return strconv.FormatFloat(ms, 'f', 1, 64)
	}
	s := strconv.FormatFloat(math.Round(ms), 'f', 0, 64)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}
