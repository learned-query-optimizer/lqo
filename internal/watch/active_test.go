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
	h := NewHint("no nested loop", []hints.Setting{{Name: "enable_nestloop", Value: "off"}})
	c.SetActive(&ActiveSet{Hints: map[string]*Hint{"shop/" + c.fingerprint(probe): h}, Holdout: holdout})
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
	// Other shapes, and the same shape inside the client's own
	// transaction, are left alone.
	if a := fc(s, 'Q', msg("SELECT 1")); a.Body != nil {
		t.Error("an unhinted shape was rewritten")
	}
	s.FromServer('C', msg("SELECT 1"))
	s.FromServer('Z', []byte{'T'})
	if a := fc(s, 'Q', msg(probe)); a.Body != nil {
		t.Error("a query inside the client's transaction was rewritten")
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
	fc(s, 'P', msg("", probe, int16(0)))
	act := s.FromClient('B', msg("", "", int16(0), int16(0), int16(0)), ahead)
	if !bytes.Contains(act.Before, []byte("set_config('enable_nestloop', 'off', true)")) || act.Before[0] != 'P' {
		t.Fatalf("before: %q", act.Before)
	}
	s.FromClient('D', msg("P", ""), ahead[0:0])
	s.FromClient('E', msg("", int32(0)), nil)
	s.FromClient('S', nil, nil)
	// The client's ParseComplete, then LQO's four replies, then the rest.
	want := []struct {
		typ  byte
		drop bool
	}{{'1', false}, {'1', true}, {'2', true}, {'D', true}, {'C', true}, {'2', false}, {'T', false}, {'D', false}, {'C', false}, {'Z', false}}
	for _, w := range want {
		body := []byte(nil)
		if w.typ == 'Z' {
			body = []byte{'I'}
		}
		if a := s.FromServer(w.typ, body); a.Drop != w.drop {
			t.Errorf("%c: drop %v, want %v", w.typ, a.Drop, w.drop)
		}
	}
	// Second time: LQO's statement is already prepared, so no Parse.
	fc(s, 'P', msg("", probe, int16(0)))
	act = s.FromClient('B', msg("", "", int16(0), int16(0), int16(0)), ahead)
	if len(act.Before) == 0 || act.Before[0] != 'B' {
		t.Errorf("second time: %q", act.Before)
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
