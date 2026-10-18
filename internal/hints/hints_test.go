// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

package hints

import (
	"strings"
	"testing"
)

func TestStandard(t *testing.T) {
	arms := Standard()
	if len(arms) != 13 {
		t.Fatalf("got %d arms", len(arms))
	}
	names := map[string]bool{}
	for _, a := range arms {
		if names[a.Name] {
			t.Errorf("duplicate arm %q", a.Name)
		}
		names[a.Name] = true
		if len(a.Settings) == 0 {
			t.Errorf("%q changes nothing", a.Name)
		}
		for _, l := range a.SetLocal() {
			if !strings.HasPrefix(l, "SET LOCAL ") || !strings.HasSuffix(l, ";") {
				t.Errorf("bad line %q", l)
			}
		}
	}
}

func TestFullIsBaosFortyEight(t *testing.T) {
	arms := Full()
	if len(arms) != 48 {
		t.Fatalf("got %d arms", len(arms))
	}
	seen := map[string]bool{}
	for _, a := range arms {
		key := strings.Join(a.Names(), ",")
		if seen[key] {
			t.Errorf("duplicate combination %s", key)
		}
		seen[key] = true
		joinsOff, scansOff := 0, 0
		for _, s := range a.Settings {
			switch s.Name {
			case "enable_hashjoin", "enable_mergejoin", "enable_nestloop":
				joinsOff++
			default:
				scansOff++
			}
		}
		if joinsOff == 3 || scansOff == 3 || joinsOff+scansOff == 0 {
			t.Errorf("%s leaves no join or scan method, or changes nothing", a.Name)
		}
	}
}

func TestSet(t *testing.T) {
	if a, err := Set(""); err != nil || len(a) != 13 {
		t.Error("default should be standard")
	}
	if a, err := Set("full"); err != nil || len(a) != 48 {
		t.Error("full")
	}
	if _, err := Set("bogus"); err == nil {
		t.Error("bogus set accepted")
	}
}

func TestSupported(t *testing.T) {
	known := map[string]bool{}
	for _, n := range SettingNames(Standard()) {
		if n != "enable_memoize" {
			known[n] = true
		}
	}
	kept, dropped := Supported(Standard(), known)
	if len(kept) != 12 || len(dropped) != 1 || dropped[0] != "no memoize" {
		t.Errorf("kept %d, dropped %v", len(kept), dropped)
	}
}
