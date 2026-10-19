// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

package watch

import (
	"encoding/binary"
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
}

type pendingKind uint8

const (
	pendSimple pendingKind = iota
	pendExec
	pendSync
)

type pending struct {
	kind   pendingKind
	sent   time.Time
	stmt   *statement
	params []string // literals; nil when the statement has none
	noText bool     // a value couldn't be written as SQL
	err    bool
}

// session follows one client connection's requests and replies. The proxy
// calls FromClient and FromServer from two goroutines.
type session struct {
	c        *Collector
	db, user string

	mu        sync.Mutex
	stmts     map[string]*statement
	portals   map[string]*pending // what each portal would run
	queue     []*pending
	describes []string // statement names awaiting ParameterDescription
	lastDone  time.Time
	lost      bool // stopped tracking: too much pipelined
}

var _ proxy.Session = (*session)(nil)

func (s *session) FromClient(typ byte, body []byte) {
	switch typ {
	case 'Q', 'P', 'B', 'E', 'S', 'D', 'C':
	default:
		return // COPY data and the like
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	switch typ {
	case 'Q':
		text, _, ok := cstring(body)
		if ok {
			s.push(&pending{kind: pendSimple, sent: now, stmt: &statement{text: text}})
		}
	case 'P':
		name, rest, ok := cstring(body)
		if !ok {
			return
		}
		text, rest, ok := cstring(rest)
		if !ok {
			return
		}
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
	case 'B':
		m, err := parseBind(body)
		if err != nil {
			return
		}
		st := s.stmts[m.stmt]
		if st == nil {
			return
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
		if len(s.portals) >= maxStatements {
			s.portals = map[string]*pending{}
		}
		s.portals[m.portal] = p
	case 'E':
		portal, _, ok := cstring(body)
		if !ok {
			return
		}
		if p := s.portals[portal]; p != nil {
			s.push(&pending{kind: pendExec, sent: now, stmt: p.stmt, params: p.params, noText: p.noText})
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

func (s *session) FromServer(typ byte, body []byte) {
	switch typ {
	case 't', 'C', 'I', 's', 'E', 'Z':
	default:
		return // rows and the like: nothing to note, and many of them
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch typ {
	case 't':
		if len(s.describes) == 0 {
			return
		}
		name := s.describes[0]
		s.describes = s.describes[1:]
		st := s.stmts[name]
		if st == nil || len(body) < 2 {
			return
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
		f := s.front()
		if f == nil {
			return
		}
		f.err = true
		if f.kind == pendExec {
			s.done(f)
			// The server skips everything up to the next Sync.
			for len(s.queue) > 0 && s.queue[0].kind == pendExec {
				s.queue = s.queue[1:]
			}
		}
	case 'Z':
		for len(s.queue) > 0 {
			f := s.queue[0]
			if f.kind == pendSimple {
				s.done(f)
				return
			}
			s.queue = s.queue[1:]
			if f.kind == pendSync {
				s.lastDone = time.Now()
				return
			}
		}
	}
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
		db:     s.db,
		user:   s.user,
		text:   f.stmt.text,
		params: f.params,
		noText: f.noText,
		ms:     float64(now.Sub(start).Microseconds()) / 1000,
		err:    f.err,
		at:     now,
	})
}

func (s *session) Close() {}
