// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

package watch

import (
	"bytes"
	"encoding/binary"
	"strconv"
	"strings"
	"testing"

	"github.com/learned-query-optimizer/lqo/internal/hints"
	"github.com/learned-query-optimizer/lqo/internal/proxy"
)

const probe = "SELECT current_setting('enable_nestloop') AS v"

func activeCollector(t *testing.T, holdout float64) *Collector {
	t.Helper()
	c := NewCollector()
	t.Cleanup(c.Close)
	h := NewHint("no nested loop", []hints.Setting{{Name: "enable_nestloop", Value: "off"}}, "")
	c.SetActive(&ActiveSet{Hints: map[string]*Hint{"shop/" + c.fingerprint(probe): h}, Holdout: holdout, Live: true})
	return c
}

func framed(typ byte, body []byte) []byte {
	b := []byte{typ, 0, 0, 0, 0}
	binary.BigEndian.PutUint32(b[1:], uint32(len(body)+4))
	return append(b, body...)
}

func TestHintSimpleQuery(t *testing.T) {
	c := activeCollector(t, 0)
	s := c.Open(map[string]string{"user": "app", "database": "shop"})
	act := fc(s, 'Q', msg(probe))
	if act.Body == nil || !strings.HasPrefix(string(act.Body), "SELECT set_config('enable_nestloop', 'off', true), set_config('plan_cache_mode', 'force_custom_plan', true); "+probe) {
		t.Fatalf("not rewritten: %q", act.Body)
	}
	// LQO's own SELECT's replies go; the client's stay.
	for _, typ := range []byte("TDC") {
		if a := s.FromServer(typ, nil); !a.Drop {
			t.Errorf("LQO's %c was passed on", typ)
		}
	}
	for _, typ := range []byte("TDCZ") {
		body := []byte(nil)
		if typ == 'Z' {
			body = []byte{'I'}
		}
		if a := s.FromServer(typ, body); a.Drop {
			t.Errorf("the client's %c was dropped", typ)
		}
	}
	// Other shapes are left alone.
	if a := fc(s, 'Q', msg("SELECT 1")); a.Body != nil {
		t.Error("an unhinted shape was rewritten")
	}
	s.FromServer('C', msg("SELECT 1"))
	s.FromServer('Z', []byte{'T'})
	// Inside the client's transaction: saved, set, and set back after.
	act = fc(s, 'Q', msg(probe))
	h := c.active.Load().Hints["shop/"+c.fingerprint(probe)]
	if want := h.Save + "; " + probe + "\n;\n" + h.Restore + "\x00"; string(act.Body) != want {
		t.Fatalf("in a transaction:\n%q\nwant\n%q", act.Body, want)
	}
	for _, typ := range []byte("TDC") {
		if a := s.FromServer(typ, nil); !a.Drop {
			t.Errorf("Save's %c was passed on", typ)
		}
	}
	// The client's own replies, even a column called lqo_restore before
	// its CommandComplete, then the Restore's, taken out.
	for _, typ := range []byte("TDC") {
		body := []byte(nil)
		if typ == 'T' {
			body = rowDesc(restoreColumn)
		}
		if a := s.FromServer(typ, body); a.Drop {
			t.Errorf("the client's %c was dropped", typ)
		}
	}
	for _, typ := range []byte("TDC") {
		body := []byte(nil)
		if typ == 'T' {
			body = rowDesc(restoreColumn)
		}
		if a := s.FromServer(typ, body); !a.Drop {
			t.Errorf("Restore's %c was passed on", typ)
		}
	}
	if a := s.FromServer('Z', []byte{'T'}); a.Drop {
		t.Error("ReadyForQuery dropped")
	}
	// Two statements in one query, in a transaction: left alone.
	if a := fc(s, 'Q', msg(probe+"; COMMIT")); a.Body != nil {
		t.Error("a query of two statements was rewritten in a transaction")
	}
	s.FromServer('C', nil)
	s.FromServer('C', nil)
	s.FromServer('Z', []byte{'I'})
	// A failed transaction: left alone.
	s.FromServer('Z', []byte{'E'})
	if a := fc(s, 'Q', msg(probe)); a.Body != nil {
		t.Error("a query in a failed transaction was rewritten")
	}
}

func rowDesc(names ...string) []byte {
	b := binary.BigEndian.AppendUint16(nil, uint16(len(names)))
	for _, n := range names {
		b = append(b, n...)
		b = append(b, 0)
		b = append(b, make([]byte, 18)...)
	}
	return b
}

// lqoMsgs is what LQO sends to run sql as its own statement.
func lqoMsgs(sql string) []byte {
	b := framed('C', []byte("S"+injectName+"\x00"))
	b = append(b, framed('P', msg(injectName, sql, int16(0)))...)
	b = append(b, framed('B', msg(injectName, injectName, int16(0), int16(0), int16(0)))...)
	b = append(b, framed('E', msg(injectName, int32(0)))...)
	return append(b, framed('C', []byte("P"+injectName+"\x00"))...)
}

type reply struct {
	typ  byte
	drop bool
}

// lqoReplies are the replies to lqoMsgs, all taken out.
var lqoReplies = []reply{{'3', true}, {'1', true}, {'2', true}, {'D', true}, {'C', true}, {'3', true}}

func replies(t *testing.T, s proxy.Session, z byte, steps ...[]reply) {
	t.Helper()
	i := 0
	for _, part := range steps {
		for _, st := range part {
			body := []byte(nil)
			if st.typ == 'Z' {
				body = []byte{z}
			}
			if a := s.FromServer(st.typ, body); a.Drop != st.drop {
				t.Fatalf("reply %d (%c): drop %v, want %v", i, st.typ, a.Drop, st.drop)
			}
			i++
		}
	}
}

// In the client's transaction, an extended statement gets Save before its
// Bind and Restore before its Sync; LQO's replies are taken out and the
// client's are passed on.
func TestHintExtendedInTransaction(t *testing.T) {
	c := activeCollector(t, 0)
	s := c.Open(map[string]string{"user": "app", "database": "shop"})
	s.FromServer('Z', []byte{'T'})
	h := c.active.Load().Hints["shop/"+c.fingerprint(probe)]
	ahead := append(framed('E', msg("", int32(0))), framed('S', nil)...)
	for call := 0; call < 2; call++ {
		fc(s, 'P', msg("", probe, int16(0)))
		act := s.FromClient('B', msg("", "", int16(0), int16(0), int16(0)), ahead)
		if !bytes.Equal(act.Before, lqoMsgs(h.Save)) {
			t.Fatalf("call %d, before the Bind: %q", call, act.Before)
		}
		fc(s, 'E', msg("", int32(0)))
		act = fc(s, 'S', nil)
		if !bytes.Equal(act.Before, lqoMsgs(h.Restore)) {
			t.Fatalf("call %d, before the Sync: %q", call, act.Before)
		}
		replies(t, s, 'T', []reply{{'1', false}}, lqoReplies, []reply{{'2', false}, {'D', false}, {'C', false}}, lqoReplies, []reply{{'Z', false}})
	}
	// The client's statement fails: the server skips the rest up to the
	// Sync, Restore included.
	fc(s, 'P', msg("", probe, int16(0)))
	s.FromClient('B', msg("", "", int16(0), int16(0), int16(0)), ahead)
	fc(s, 'E', msg("", int32(0)))
	fc(s, 'S', nil)
	replies(t, s, 'E', []reply{{'1', false}}, lqoReplies, []reply{{'2', false}, {'E', false}, {'Z', false}})
	if len(s.(*session).queue) != 0 {
		t.Errorf("queue left: %d", len(s.(*session).queue))
	}
}

// A held-out call of a prepared statement, and a call of a statement LQO
// has hinted once the hint is switched off, are planned fresh as the hinted
// calls are (plan_cache_mode only), so the hinted plans' costs can't move
// them to the generic plan. A statement never hinted gets nothing.
func TestFreshPlansWithoutTheHint(t *testing.T) {
	c := activeCollector(t, 1) // every call held out
	s := c.Open(map[string]string{"user": "app", "database": "shop"})
	ahead := append(framed('E', msg("", int32(0))), framed('S', nil)...)
	fc(s, 'P', msg("st", probe, int16(0)))
	s.FromServer('1', nil)
	s.FromServer('Z', []byte{'I'})
	act := s.FromClient('B', msg("", "st", int16(0), int16(0), int16(0)), ahead)
	if !bytes.Equal(act.Before, lqoMsgs(plainHint.SQL)) {
		t.Fatalf("held out: %q", act.Before)
	}
	fc(s, 'E', msg("", int32(0)))
	fc(s, 'S', nil)
	replies(t, s, 'I', lqoReplies, []reply{{'2', false}, {'D', false}, {'C', false}, {'Z', false}})

	c2 := activeCollector(t, 0)
	s = c2.Open(map[string]string{"user": "app", "database": "shop"})
	fc(s, 'P', msg("st", probe, int16(0)))
	s.FromServer('1', nil)
	s.FromServer('Z', []byte{'I'})
	call := func() []byte {
		act := s.FromClient('B', msg("", "st", int16(0), int16(0), int16(0)), ahead)
		fc(s, 'E', msg("", int32(0)))
		fc(s, 'S', nil)
		if act.Before != nil {
			replies(t, s, 'I', lqoReplies, []reply{{'2', false}, {'D', false}, {'C', false}, {'Z', false}})
		} else {
			replies(t, s, 'I', []reply{{'2', false}, {'D', false}, {'C', false}, {'Z', false}})
		}
		return act.Before
	}
	h := c2.active.Load().Hints["shop/"+c2.fingerprint(probe)]
	if b := call(); !bytes.Equal(b, lqoMsgs(h.SQL)) {
		t.Fatalf("hinted: %q", b)
	}
	// The shape's hint taken back (or the shape switched off) while LQO
	// applies others: fresh plans.
	c2.SetActive(&ActiveSet{Hints: map[string]*Hint{}, Live: true})
	if b := call(); !bytes.Equal(b, lqoMsgs(plainHint.SQL)) {
		t.Fatalf("after the hint is taken back: %q", b)
	}
	// Every hint switched off, or LQO paused: nothing at all.
	for _, set := range []*ActiveSet{{Hints: map[string]*Hint{}, Off: true}, {Hints: map[string]*Hint{}}} {
		c2.SetActive(set)
		if b := call(); b != nil {
			t.Fatalf("switched off or paused: %q", b)
		}
	}
	// Another statement, never hinted: nothing.
	fc(s, 'P', msg("other", probe, int16(0)))
	s.FromServer('1', nil)
	s.FromServer('Z', []byte{'I'})
	if act := s.FromClient('B', msg("", "other", int16(0), int16(0), int16(0)), ahead); act.Before != nil {
		t.Errorf("a statement never hinted: %q", act.Before)
	}
}

func TestHintExtendedQuery(t *testing.T) {
	c := activeCollector(t, 0)
	s := c.Open(map[string]string{"user": "app", "database": "shop"})
	ahead := append(append(framed('D', msg("P", "")), framed('E', msg("", int32(0)))...), framed('S', nil)...)
	early := c.Open(map[string]string{"user": "app", "database": "shop"})
	fc(early, 'P', msg("", probe, int16(0)))
	if act := early.FromClient('B', msg("", "", int16(0), int16(0), int16(0)), nil); act.Before != nil {
		t.Error("hinted without seeing the rest of the batch")
	}
	h := c.active.Load().Hints["shop/"+c.fingerprint(probe)]
	for call := 0; call < 2; call++ {
		fc(s, 'P', msg("", probe, int16(0)))
		act := s.FromClient('B', msg("", "", int16(0), int16(0), int16(0)), ahead)
		if !bytes.Equal(act.Before, lqoMsgs(h.SQL)) {
			t.Fatalf("call %d: before: %q", call, act.Before)
		}
		s.FromClient('D', msg("P", ""), ahead[0:0])
		s.FromClient('E', msg("", int32(0)), nil)
		s.FromClient('S', nil, nil)
		// The client's ParseComplete, then LQO's replies, then the rest.
		replies(t, s, 'I', []reply{{'1', false}}, lqoReplies, []reply{{'2', false}, {'T', false}, {'D', false}, {'C', false}, {'Z', false}})
	}
	c.Flush()
}

func TestHintNotWithMoreInTheBatch(t *testing.T) {
	c := activeCollector(t, 0)
	s := c.Open(map[string]string{"user": "app", "database": "shop"})
	// Two statements before one Sync share a transaction: no hint.
	ahead := append(append(framed('E', msg("", int32(0))), framed('B', msg("", "", int16(0), int16(0), int16(0)))...), framed('E', msg("", int32(0)))...)
	ahead = append(ahead, framed('S', nil)...)
	fc(s, 'P', msg("", probe, int16(0)))
	if act := s.FromClient('B', msg("", "", int16(0), int16(0), int16(0)), ahead); act.Before != nil {
		t.Error("hinted a statement that shares its transaction")
	}
}

func TestErrorPositionIsFixed(t *testing.T) {
	c := activeCollector(t, 0)
	s := c.Open(map[string]string{"user": "app", "database": "shop"})
	act := fc(s, 'Q', msg(probe))
	prefix := len(act.Body) - len(probe) - 1
	for _, typ := range []byte("TDC") {
		s.FromServer(typ, nil)
	}
	errBody := []byte("SERROR\x00C42703\x00Mcolumn does not exist\x00P" + strconv.Itoa(prefix+8) + "\x00\x00")
	a := s.FromServer('E', errBody)
	if !bytes.Contains(a.Body, []byte("\x00P8\x00")) {
		t.Errorf("position not fixed: %q", a.Body)
	}
}

func TestHoldoutLeavesSomeCallsAlone(t *testing.T) {
	c := activeCollector(t, 0.3)
	s := c.Open(map[string]string{"user": "app", "database": "shop"})
	hinted := 0
	for i := 0; i < 1000; i++ {
		if a := fc(s, 'Q', msg(probe)); a.Body != nil {
			hinted++
			for _, typ := range []byte("TDC") {
				s.FromServer(typ, nil)
			}
		}
		s.FromServer('C', nil)
		s.FromServer('Z', []byte{'I'})
	}
	if hinted < 620 || hinted > 780 {
		t.Errorf("%d of 1000 hinted with a 30%% holdout", hinted)
	}
	c.Flush()
	sh := c.Snapshot()[0]
	if sh.HintedCalls != int64(hinted) || sh.HoldoutCalls != int64(1000-hinted) || !sh.Active {
		t.Errorf("hinted %d holdout %d active %v", sh.HintedCalls, sh.HoldoutCalls, sh.Active)
	}
}

func TestOffMeansNoHints(t *testing.T) {
	c := activeCollector(t, 0)
	set := *c.active.Load()
	set.Off = true
	c.SetActive(&set)
	s := c.Open(map[string]string{"user": "app", "database": "shop"})
	if a := fc(s, 'Q', msg(probe)); a.Body != nil {
		t.Error("hinted with the master switch off")
	}
}

var _ proxy.Session = (*session)(nil)
