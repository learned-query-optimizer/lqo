// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/learned-query-optimizer/lqo/internal/hints"
)

func entry(id string, gain float64) *Entry {
	v := "keep"
	if gain > 0 {
		v = "faster"
	}
	return &Entry{ID: id, Verdict: v, Gain: gain, Example: "SELECT " + id, Settings: []hints.Setting{{"enable_nestloop", "off"}}}
}

func TestMissingFileIsEmpty(t *testing.T) {
	f, err := Load(filepath.Join(t.TempDir(), "none.json"))
	if err != nil || len(f.Shapes) != 0 {
		t.Fatalf("%v %v", f, err)
	}
}

func TestSaveLoadAndPut(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.json")
	f := New()
	f.Put(entry("bbbbbbbbbbbbbbbb", 0))
	f.Put(entry("aaaaaaaaaaaaaaaa", 0.4))
	f.Put(entry("cccccccccccccccc", 0.6))
	if err := f.Save(p); err != nil {
		t.Fatal(err)
	}
	g, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Shapes) != 3 || g.Updated.IsZero() {
		t.Fatalf("%+v", g)
	}
	s := g.Sorted()
	if s[0].ID != "cccccccccccccccc" || s[1].ID != "aaaaaaaaaaaaaaaa" || s[2].Verdict != "keep" {
		t.Errorf("order: %s %s %s", s[0].ID, s[1].ID, s[2].ID)
	}
	// A newer finding replaces the older one for the same shape.
	g.Put(entry("aaaaaaaaaaaaaaaa", 0))
	g.Save(p)
	h, _ := Load(p)
	if h.Shapes["aaaaaaaaaaaaaaaa"].Verdict != "keep" || len(h.Shapes) != 3 {
		t.Error("Put didn't replace")
	}
	// No temporary files are left behind.
	files, _ := os.ReadDir(filepath.Dir(p))
	if len(files) != 1 {
		t.Errorf("left behind: %v", files)
	}
}

func TestDamagedFileIsSetAside(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "s.json")
	os.WriteFile(p, []byte(`{"format": 1, "shapes": {"x": `), 0o644)
	if _, err := Load(p); err == nil {
		t.Fatal("a damaged file should fail to load")
	}
	f, note, err := LoadOrSetAside(p)
	if err != nil || len(f.Shapes) != 0 || !strings.Contains(note, "damaged") {
		t.Fatalf("%v %q %v", f, note, err)
	}
	matches, _ := filepath.Glob(p + ".damaged-*")
	if len(matches) != 1 {
		t.Fatalf("the damaged file wasn't kept: %v", matches)
	}
	os.WriteFile(p, []byte(`{"format": 99, "shapes": {}}`), 0o644)
	if _, err := Load(p); err == nil {
		t.Error("an unknown format should fail to load")
	}
}

// TestKilledWhileSaving starts a process that saves the state file over
// and over, kills it with SIGKILL at a random moment, and checks the file
// is whole every time. It's the state file's main promise.
func TestKilledWhileSaving(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses SIGKILL")
	}
	if os.Getenv("LQO_STATE_WRITER") != "" {
		writer(os.Getenv("LQO_STATE_WRITER"))
		return
	}
	p := filepath.Join(t.TempDir(), "s.json")
	kills := 40
	if testing.Short() {
		kills = 10
	}
	for i := 0; i < kills; i++ {
		cmd := exec.Command(os.Args[0], "-test.run=^TestKilledWhileSaving$")
		cmd.Env = append(os.Environ(), "LQO_STATE_WRITER="+p)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Duration(30+i*7%90) * time.Millisecond)
		cmd.Process.Kill()
		cmd.Wait()
		f, err := Load(p)
		if err != nil {
			t.Fatalf("after kill %d: %v", i+1, err)
		}
		if len(f.Shapes) > 0 && len(f.Shapes) != 500 {
			t.Fatalf("after kill %d: %d shapes, want 0 or 500", i+1, len(f.Shapes))
		}
	}
	files, _ := filepath.Glob(filepath.Join(filepath.Dir(p), ".*tmp-*"))
	t.Logf("%d kills, file whole each time; %d temporary files left by killed writers", kills, len(files))
	// The next save clears temporary files once they are old enough.
	old := time.Now().Add(-time.Hour)
	for _, f := range files {
		os.Chtimes(f, old, old)
	}
	if err := New().Save(p); err != nil {
		t.Fatal(err)
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(p), ".*tmp-*")); len(left) != 0 {
		t.Errorf("stale temporary files not cleared: %v", left)
	}
}

// writer saves a 500-shape state file in a loop until it is killed.
func writer(path string) {
	for n := 0; ; n++ {
		f := New()
		for i := 0; i < 500; i++ {
			e := entry(fmt.Sprintf("%016x", i), float64(n%7)/10)
			e.Normalized = strings.Repeat("select x from t where y = ? ", 20)
			f.Put(e)
		}
		if err := f.Save(path); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}
