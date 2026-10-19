// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

package watch

import (
	"encoding/binary"
	"math/rand/v2"
	"strconv"
	"sync"
	"time"

	"github.com/learned-query-optimizer/lqo/internal/proxy"
)

// maxPending caps the requests a session tracks at once. A client that
// pipelines more than this simply stops being measured until it catches up.
const maxPending = 10000

// maxStatements caps the prepared statements a session remembers.
const maxStatements = 5000

type statement struct {
	text string
	oids []uint32 // from Parse, then filled in from ParameterDescription
	key  string   // database and shape ID, worked out when first needed

	// decided is set when LQO decided at Parse time, for a hint with a
	// comment that had to go in the Parse text: the hint, or the holdout.
	decided *decision
}

type decision struct {
	hint    *Hint
	holdout bool
}

type pendingKind uint8

const (
	pendSimple pendingKind = iota
	pendExec
	pendSync
	pendInject // the replies to messages LQO put in, to take out again
)

type pending struct {
	kind   pendingKind
	sent   time.Time
	stmt   *statement
	params []string // literals; nil when the statement has none
	noText bool     // a value couldn't be written as SQL
	err    bool

	hinted    bool // LQO applied a hint to this one
	holdout   bool // a hint applied to its shape, but not to this call
	prefixLen int  // characters LQO put in front of a simple query

	// For pendInject: replies to pass on first (the client's own, sent
	// before LQO's messages), then the replies to LQO's messages, to drop.
	pass, expect []byte
	// For pendInject with a hint comment: the call it's for, which loses
	// its hinted mark if the reply says pg_hint_plan isn't loaded.
	check *pending
}

// session follows one client connection's requests and replies. The proxy
// calls FromClient and FromServer from two goroutines.
//
// In active mode it also applies proven hints. A hint is applied only to a
// statement that starts a fresh implicit transaction: the session is idle,
// nothing else is in flight, and the statement is the whole of what the
// client sent before its Sync. Its settings are set with set_config(...,
// true), which is SET LOCAL: they end with that implicit transaction, so
// they can't reach any other statement.
type session struct {
	c        *Collector
	db, user string

	mu        sync.Mutex
	stmts     map[string]*statement
	portals   map[string]*pending // what each portal would run
	queue     []*pending
	describes []string // statement names awaiting ParameterDescription
	lastDone  time.Time
	lost      bool   // stopped tracking: too much pipelined
	txStatus  byte   // from the last ReadyForQuery
	batch     []byte // the client's messages since the last reply was complete
	injected  map[string]string
	rng       *rand.Rand

	// shift is how many characters LQO put in front of the text of the
	// statement being answered, until the next ReadyForQuery, so error
	// positions can be moved back.
	shift int
	// noHintPlan: this session's server has no pg_hint_plan loaded, so
	// hints with comments aren't applied here.
	noHintPlan bool
}

var _ proxy.Session = (*session)(nil)

func (s *session) FromClient(typ byte, body, ahead []byte) proxy.Action {
	switch typ {
	case 'Q', 'P', 'B', 'E', 'S', 'D', 'C', 'F', 'H':
	default:
		return proxy.Action{} // COPY data and the like
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	var act proxy.Action
	switch typ {
	case 'Q':
		text, _, ok := cstring(body)
		if ok {
			p := &pending{kind: pendSimple, sent: now, stmt: &statement{text: text}}
			act = s.hintSimple(p)
			s.push(p)
		}
	case 'P':
		name, rest, ok := cstring(body)
		if !ok {
			break
		}
		text, rest, ok := cstring(rest)
		if !ok {
			break
		}
		tail := rest // the parameter types, as sent
		st := &statement{text: text}
		if len(rest) >= 2 {
			n := int(binary.BigEndian.Uint16(rest))
			rest = rest[2:]
			for i := 0; i < n && len(rest) >= 4; i++ {
				st.oids = append(st.oids, binary.BigEndian.Uint32(rest))
				rest = rest[4:]
			}
		}
		if len(s.stmts) >= maxStatements {
			s.stmts = map[string]*statement{}
		}
		s.stmts[name] = st
		act = s.hintParse(name, st, tail, ahead)
	case 'B':
		m, err := parseBind(body)
		if err != nil {
			break
		}
		st := s.stmts[m.stmt]
		if st == nil {
			break
		}
		p := &pending{kind: pendExec, stmt: st}
		if len(m.values) > 0 {
			p.params = make([]string, len(m.values))
			for i, v := range m.values {
				var oid uint32
				if i < len(st.oids) {
					oid = st.oids[i]
				}
				lit, ok := literal(v, v == nil, m.binary(i), oid)
				if !ok {
					p.noText = true
					p.params = nil
					break
				}
				p.params[i] = lit
			}
		}
		act = s.hintExtended(p, ahead)
		if len(s.portals) >= maxStatements {
			s.portals = map[string]*pending{}
		}
		s.portals[m.portal] = p
	case 'E':
		portal, _, ok := cstring(body)
		if !ok {
			break
		}
		if p := s.portals[portal]; p != nil {
			s.push(&pending{kind: pendExec, sent: now, stmt: p.stmt, params: p.params, noText: p.noText, hinted: p.hinted, holdout: p.holdout})
			// A portal is hinted once: run again after its Sync, its
			// transaction and settings are gone.
			p.hinted, p.holdout = false, false
		}
	case 'S':
		s.push(&pending{kind: pendSync, sent: now})
	case 'D':
		if len(body) > 0 && body[0] == 'S' {
			name, _, ok := cstring(body[1:])
			if ok {
				s.describes = append(s.describes, name)
			}
		}
	case 'C':
		if len(body) > 0 {
			name, _, ok := cstring(body[1:])
			if ok {
				if body[0] == 'S' {
					delete(s.stmts, name)
				} else {
					delete(s.portals, name)
				}
			}
		}
	}
	if len(s.batch) < 64 {
		s.batch = append(s.batch, typ)
	}
	return act
}

// fresh reports whether a statement would start a new implicit
// transaction of its own: the session is idle, everything sent before has
// been answered, and the client has sent nothing since but what's in
// allowed.
func (s *session) fresh(allowed string) bool {
	if s.txStatus != 'I' || len(s.queue) > 0 || s.lost || len(s.batch) > len(allowed) {
		return false
	}
	return string(s.batch) == allowed[:len(s.batch)] && len(s.batch) == len(allowed)
}

// hintFor finds the active hint for a statement's shape, and decides
// whether this call is held out to compare against.
func (s *session) hintFor(st *statement, p *pending) *Hint {
	set := s.c.active.Load()
	if set == nil || set.Off || len(set.Hints) == 0 {
		return nil
	}
	if st.key == "" {
		st.key = s.db + "/" + s.c.fingerprint(st.text)
	}
	h := set.Hints[st.key]
	if h == nil || (h.Comment != "" && s.noHintPlan) {
		return nil
	}
	if s.rng == nil {
		s.rng = rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), uint64(len(s.db))))
	}
	if set.Holdout > 0 && s.rng.Float64() < set.Holdout {
		p.holdout = true
		return nil
	}
	return h
}

// hintSimple puts the hint's settings in front of a simple query, in the
// same Query message, so they share its implicit transaction.
func (s *session) hintSimple(p *pending) proxy.Action {
	if !s.fresh("") {
		return proxy.Action{}
	}
	h := s.hintFor(p.stmt, p)
	if h == nil {
		return proxy.Action{}
	}
	// A hint comment goes first: pg_hint_plan reads it from the start of
	// the whole query string, and applies it to each statement in it.
	prefix := h.prefix() + h.SQL + "; "
	body := make([]byte, 0, len(prefix)+len(p.stmt.text)+1)
	body = append(body, prefix...)
	body = append(body, p.stmt.text...)
	body = append(body, 0)
	p.hinted = true
	p.prefixLen = len(prefix)
	inj := &pending{kind: pendInject, expect: []byte("TDC")}
	if h.Comment != "" {
		inj.check = p
	}
	s.push(inj)
	return proxy.Action{Body: body}
}

// hintParse decides at Parse time for a shape whose hint has a comment,
// which has to be in the statement's text when it's planned. Only for the
// unnamed statement, sent with its Bind, an optional Describe, Execute and
// Sync in one batch, so the text is planned once, for this call alone.
func (s *session) hintParse(name string, st *statement, tail, ahead []byte) proxy.Action {
	if name != "" || !s.fresh("") || !parseBatch(ahead) {
		return proxy.Action{}
	}
	p := &pending{}
	h := s.hintFor(st, p)
	switch {
	case p.holdout:
		st.decided = &decision{holdout: true}
		return proxy.Action{}
	case h == nil:
		return proxy.Action{}
	}
	st.decided = &decision{hint: h}
	if h.Comment == "" {
		return proxy.Action{}
	}
	prefix := h.prefix()
	body := make([]byte, 0, 1+len(prefix)+len(st.text)+1+len(tail))
	body = append(body, 0) // the unnamed statement
	body = append(body, prefix...)
	body = append(body, st.text...)
	body = append(body, 0)
	body = append(body, tail...)
	s.shift = len(prefix)
	return proxy.Action{Body: body}
}

// parseBatch reports whether ahead is a Bind, an optional Describe, an
// Execute and a Sync: the rest of one statement's batch after its Parse.
func parseBatch(ahead []byte) bool {
	if len(ahead) < 5 || ahead[0] != 'B' {
		return false
	}
	n := int(binary.BigEndian.Uint32(ahead[1:]))
	if n < 4 || len(ahead) < 1+n {
		return false
	}
	return wholeBatch(ahead[1+n:])
}

// hintExtended runs the hint's settings, as a statement of LQO's own,
// between the client's Parse and its Bind, where the plan is made. Only
// when the client's batch is Parse (optional), Bind, Describe (optional),
// Execute, Sync, all already sent, so nothing else shares the
// transaction.
func (s *session) hintExtended(p *pending, ahead []byte) proxy.Action {
	parsed := s.fresh("P")
	if !parsed && !s.fresh("") {
		return proxy.Action{}
	}
	if !wholeBatch(ahead) {
		return proxy.Action{}
	}
	var h *Hint
	if d := p.stmt.decided; d != nil {
		// Decided at Parse, for this batch.
		p.stmt.decided = nil
		if d.holdout {
			p.holdout = true
			return proxy.Action{}
		}
		h = d.hint
	} else {
		h = s.hintFor(p.stmt, p)
		if h != nil && h.Comment != "" {
			// Its comment would have had to be in the Parse text.
			return proxy.Action{}
		}
	}
	if h == nil {
		return proxy.Action{}
	}
	if s.injected == nil {
		s.injected = map[string]string{}
	}
	var before []byte
	expect := []byte("2DC")
	name, ok := s.injected[h.SQL]
	if !ok {
		name = "lqo_hint_" + strconv.Itoa(len(s.injected)+1)
		s.injected[h.SQL] = name
		before = appendMsg(before, 'P', cstr(name), cstr(h.SQL), []byte{0, 0})
		expect = []byte("12DC")
	}
	before = appendMsg(before, 'B', cstr("lqo_hint"), cstr(name), []byte{0, 0, 0, 0, 0, 0})
	before = appendMsg(before, 'E', cstr("lqo_hint"), []byte{0, 0, 0, 0})
	inj := &pending{kind: pendInject, expect: expect}
	if h.Comment != "" {
		inj.check = p
	}
	if parsed {
		inj.pass = []byte("1") // the client's own ParseComplete comes first
	}
	s.push(inj)
	p.hinted = true
	return proxy.Action{Before: before}
}

// wholeBatch reports whether ahead starts with an optional Describe, an
// Execute and a Sync: the rest of a single statement's batch.
func wholeBatch(ahead []byte) bool {
	var types []byte
	for len(ahead) >= 5 && len(types) < 3 {
		n := int(binary.BigEndian.Uint32(ahead[1:]))
		if n < 4 || len(ahead) < 1+n {
			return false
		}
		types = append(types, ahead[0])
		ahead = ahead[1+n:]
		if ahead := types[len(types)-1]; ahead == 'S' {
			break
		}
	}
	switch string(types) {
	case "ES", "DES":
		return true
	}
	return false
}

func cstr(s string) []byte { return append([]byte(s), 0) }

// lastColumn is the text of a DataRow's last column, or "" for NULL or a
// row it can't read.
func lastColumn(body []byte) string {
	if len(body) < 2 {
		return ""
	}
	n := int(binary.BigEndian.Uint16(body))
	b := body[2:]
	v := ""
	for i := 0; i < n; i++ {
		if len(b) < 4 {
			return ""
		}
		l := int(int32(binary.BigEndian.Uint32(b)))
		b = b[4:]
		if l < 0 {
			v = ""
			continue
		}
		if len(b) < l {
			return ""
		}
		v = string(b[:l])
		b = b[l:]
	}
	return v
}

func appendMsg(b []byte, typ byte, parts ...[]byte) []byte {
	n := 4
	for _, p := range parts {
		n += len(p)
	}
	b = append(b, typ)
	b = binary.BigEndian.AppendUint32(b, uint32(n))
	for _, p := range parts {
		b = append(b, p...)
	}
	return b
}

func (s *session) push(p *pending) {
	if s.lost {
		if p.kind != pendSync && p.kind != pendSimple {
			return
		}
		// Start again at a clean boundary once the queue has drained.
		if len(s.queue) > 0 {
			return
		}
		s.lost = false
	}
	if len(s.queue) >= maxPending {
		s.queue = nil
		s.lost = true
		return
	}
	s.queue = append(s.queue, p)
}

func (s *session) FromServer(typ byte, body []byte) proxy.Action {
	switch typ {
	case 't', 'C', 'I', 's', 'E', 'Z', 'T', 'D', '1', '2', 'N':
	default:
		return proxy.Action{} // nothing to note
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Replies to messages LQO put in are taken out, in order.
	if f := s.front(); f != nil && f.kind == pendInject {
		switch {
		case typ == 'N':
			// A notice: pass it on.
		case len(f.pass) > 0 && typ == f.pass[0]:
			f.pass = f.pass[1:]
			return proxy.Action{}
		case len(f.pass) == 0 && len(f.expect) > 0 && typ == f.expect[0]:
			if typ == 'D' && f.check != nil && lastColumn(body) != "t" {
				// pg_hint_plan isn't loaded in this session: the comment
				// did nothing, so the call isn't a hinted one, and comments
				// aren't put in here again.
				s.noHintPlan = true
				f.check.hinted = false
				s.c.noHintPlan.Add(1)
			}
			f.expect = f.expect[1:]
			if len(f.expect) == 0 {
				s.queue = s.queue[1:]
			}
			return proxy.Action{Drop: true}
		default:
			// Something else, such as an error: the server will skip
			// LQO's messages along with the client's. Stop expecting.
			s.queue = s.queue[1:]
		}
	}
	switch typ {
	case 'T', 'D', '1', '2':
		return proxy.Action{}
	case 'N':
		return s.fixPosition(body)
	case 't':
		if len(s.describes) == 0 {
			return proxy.Action{}
		}
		name := s.describes[0]
		s.describes = s.describes[1:]
		st := s.stmts[name]
		if st == nil || len(body) < 2 {
			return proxy.Action{}
		}
		n := int(binary.BigEndian.Uint16(body))
		b := body[2:]
		oids := make([]uint32, 0, n)
		for i := 0; i < n && len(b) >= 4; i++ {
			oids = append(oids, binary.BigEndian.Uint32(b))
			b = b[4:]
		}
		// Keep the types the client gave in Parse; fill in the rest.
		for i := range oids {
			if i < len(st.oids) && st.oids[i] != 0 {
				oids[i] = st.oids[i]
			}
		}
		st.oids = oids
	case 'C', 'I', 's':
		if f := s.front(); f != nil && f.kind == pendExec {
			s.done(f)
		}
	case 'E':
		act := s.fixPosition(body)
		f := s.front()
		if f == nil {
			return act
		}
		f.err = true
		if f.kind == pendExec {
			s.done(f)
			// The server skips everything up to the next Sync.
			for len(s.queue) > 0 && (s.queue[0].kind == pendExec || s.queue[0].kind == pendInject) {
				s.queue = s.queue[1:]
			}
		}
		return act
	case 'Z':
		s.shift = 0
		if len(body) > 0 {
			s.txStatus = body[0]
		}
		for len(s.queue) > 0 {
			f := s.queue[0]
			if f.kind == pendSimple {
				s.done(f)
				break
			}
			s.queue = s.queue[1:]
			if f.kind == pendSync {
				s.lastDone = time.Now()
				break
			}
		}
		if len(s.queue) == 0 {
			s.batch = s.batch[:0]
		}
	}
	return proxy.Action{}
}

// fixPosition corrects the error position in an error or notice about a
// simple query LQO put a hint in front of, so the client points at the
// right character of its own text.
func (s *session) fixPosition(body []byte) proxy.Action {
	shift := s.shift
	if f := s.front(); f != nil && f.kind == pendSimple && f.prefixLen > 0 {
		shift = f.prefixLen
	}
	if shift == 0 {
		return proxy.Action{}
	}
	out := make([]byte, 0, len(body))
	changed := false
	b := body
	for len(b) > 0 && b[0] != 0 {
		code := b[0]
		val, rest, ok := cstring(b[1:])
		if !ok {
			return proxy.Action{}
		}
		if code == 'P' {
			if n, err := strconv.Atoi(val); err == nil && n > shift {
				val = strconv.Itoa(n - shift)
				changed = true
			}
		}
		out = append(out, code)
		out = append(out, val...)
		out = append(out, 0)
		b = rest
	}
	out = append(out, 0)
	if !changed {
		return proxy.Action{}
	}
	return proxy.Action{Body: out}
}

func (s *session) front() *pending {
	if len(s.queue) == 0 {
		return nil
	}
	return s.queue[0]
}

// done ends the request at the front of the queue and reports it. Its time
// runs from when it was sent, or from when the one before it finished if
// that was later, so a pipelined request isn't charged for the wait.
func (s *session) done(f *pending) {
	s.queue = s.queue[1:]
	now := time.Now()
	start := f.sent
	if s.lastDone.After(start) {
		start = s.lastDone
	}
	s.lastDone = now
	s.c.record(event{
		db:      s.db,
		user:    s.user,
		text:    f.stmt.text,
		params:  f.params,
		noText:  f.noText,
		ms:      float64(now.Sub(start).Microseconds()) / 1000,
		err:     f.err,
		at:      now,
		hinted:  f.hinted,
		holdout: f.holdout,
	})
}

func (s *session) Close() {}
