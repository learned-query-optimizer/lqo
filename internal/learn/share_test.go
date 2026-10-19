// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

package learn

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/learned-query-optimizer/lqo/internal/hints"
	"github.com/learned-query-optimizer/lqo/internal/state"
)

// shareRig is a learner with one proven shape and a new relative of it.
func shareRig(t *testing.T, opt Options) (*Learner, *state.Learned, []hints.Arm) {
	t.Helper()
	opt.fill()
	l := &Learner{opt: opt, file: state.New(), rng: rand.New(rand.NewPCG(1, 2))}
	arms := hints.Standard()
	old := &state.Learned{Key: "shop/aaaa", ID: "aaaa", Database: "shop", Family: "customers,orders", Status: Proven, Proven: "no nested loop", Arms: map[string]*state.Arm{}}
	for _, a := range arms {
		switch a.Name {
		case "no nested loop":
			old.Arms[a.Name] = &state.Arm{N: 3, Mean: math.Log(0.5), M2: 0.01, Wins: 2, MaxRatio: 0.55}
		case "no parallel workers":
			old.Arms[a.Name] = &state.Arm{N: 1, Out: "it was 68% slower on a text, twice"}
		default:
			old.Arms[a.Name] = &state.Arm{N: 3, SamePlan: 3, Out: "it doesn't change the plan"}
		}
	}
	l.file.Serve[old.Key] = old
	other := &state.Learned{Key: "shop/cccc", ID: "cccc", Database: "shop", Family: "products", Arms: map[string]*state.Arm{
		"no index scan": {N: 3, Mean: math.Log(0.3), M2: 0.01, Wins: 3, MaxRatio: 0.35},
	}}
	l.file.Serve[other.Key] = other
	e := &state.Learned{Key: "shop/bbbb", ID: "bbbb", Database: "shop", Family: "customers,orders", Status: Learning, Arms: map[string]*state.Arm{}}
	l.file.Serve[e.Key] = e
	return l, e, arms
}

// firstThree counts, over many draws, how often each hint is among the first
// three a new shape would time.
func firstThree(l *Learner, e *state.Learned, arms []hints.Arm) map[string]int {
	got := map[string]int{}
	for i := 0; i < 1000; i++ {
		want, _ := l.choose(e, arms, "SELECT 1")
		for k, idx := range want {
			if k == 3 {
				break
			}
			got[arms[idx].Name]++
		}
	}
	return got
}

// A new shape tries what worked for its relatives first, and what was slower
// for them last; only relatives that read the same tables count.
func TestRelativesGiveAHeadStart(t *testing.T) {
	l, e, arms := shareRig(t, Options{})
	got := firstThree(l, e, arms)
	if got["no nested loop"] < 950 {
		t.Errorf("with relatives, no nested loop led %d times in 1000", got["no nested loop"])
	}
	if got["no parallel workers"] > got["no merge join"] {
		t.Errorf("a hint the relative ruled out led more often (%d) than one it never tried (%d)", got["no parallel workers"], got["no merge join"])
	}
	if got["no index scan"] > 400 {
		t.Errorf("another family's winner led %d times", got["no index scan"])
	}
	if e.HeadStart != 1 {
		t.Errorf("head start from %d shapes, want 1", e.HeadStart)
	}

	l, e, arms = shareRig(t, Options{NoShare: true})
	got = firstThree(l, e, arms)
	if got["no nested loop"] > 450 {
		t.Errorf("without sharing, no nested loop still led %d times in 1000", got["no nested loop"])
	}
	if e.HeadStart != 0 {
		t.Errorf("head start %d with sharing off", e.HeadStart)
	}
}

// What relatives say only orders the hints. It never proves one, and the
// shape's own results soon outweigh it.
func TestRelativesNeverProve(t *testing.T) {
	l, e, arms := shareRig(t, Options{})
	l.choose(e, arms, "SELECT 1")
	l.settle(e, arms)
	if e.Status != Learning || e.Proven != "" {
		t.Fatalf("proven on relatives' results alone: %+v", e)
	}
	// Here no nested loop is 40% slower, on three texts: it drops from
	// first to last, although the relative said otherwise.
	st := e.Arms["no nested loop"]
	for i := 0; i < 3; i++ {
		observe(st, math.Log(1.4), 1.4)
	}
	got := firstThree(l, e, arms)
	if got["no nested loop"] > 50 {
		t.Errorf("after three slower texts of its own, no nested loop still led %d times in 1000", got["no nested loop"])
	}
}

// The prior counts for at most ShareWeight texts, however many relatives
// there are.
func TestPriorWeightIsCapped(t *testing.T) {
	l, e, _ := shareRig(t, Options{})
	for i := 0; i < 20; i++ {
		k := string(rune('d'+i)) + "xxx"
		l.file.Serve["shop/"+k] = &state.Learned{Key: "shop/" + k, ID: k, Database: "shop", Family: e.Family, Arms: map[string]*state.Arm{
			"no nested loop": {N: 10, Mean: math.Log(0.5), Wins: 5},
		}}
	}
	p, _ := l.priors(e)
	if n := p["no nested loop"].n; n > l.opt.ShareWeight+1e-9 {
		t.Errorf("prior counts for %.1f texts, cap %.1f", n, l.opt.ShareWeight)
	}
	if e.HeadStart != 21 {
		t.Errorf("head start from %d shapes, want 21", e.HeadStart)
	}
	// A relative on another database isn't one.
	e.Database = "other"
	if p, _ := l.priors(e); len(p) != 0 || e.HeadStart != 0 {
		t.Errorf("relatives across databases: %v", p)
	}
}

// When a relative proved a hint, a new shape tries that hint alone, until
// its own texts say otherwise; then it's back to drawing a few.
func TestFocusOnARelativesProvenHint(t *testing.T) {
	l, e, arms := shareRig(t, Options{})
	want, focus := l.choose(e, arms, "SELECT 1")
	if !focus || arms[want[0]].Name != "no nested loop" {
		t.Fatalf("focus %v on %q", focus, arms[want[0]].Name)
	}
	// A clear win on the shape's own text: still focused.
	observe(e.Arms["no nested loop"], math.Log(0.5), 0.5)
	e.Arms["no nested loop"].Wins++
	if _, focus := l.choose(e, arms, "SELECT 1"); !focus {
		t.Error("lost focus after a good text")
	}
	// A text where it saves little: no more focus.
	observe(e.Arms["no nested loop"], math.Log(0.97), 0.97)
	if _, focus := l.choose(e, arms, "SELECT 1"); focus {
		t.Error("still focused on a hint the shape's own texts don't bear out")
	}
	// No relative proved anything: no focus, only a head start.
	l, e, arms = shareRig(t, Options{})
	l.file.Serve["shop/aaaa"].Status = Learning
	if _, focus := l.choose(e, arms, "SELECT 1"); focus {
		t.Error("focused without a proven relative")
	}
	l, e, arms = shareRig(t, Options{NoShare: true})
	if _, focus := l.choose(e, arms, "SELECT 1"); focus {
		t.Error("focused with sharing off")
	}
}
