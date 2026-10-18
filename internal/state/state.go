// Copyright lqo.net 2026
// SPDX-License-Identifier: Apache-2.0

// Package state keeps what LQO has learned in one local JSON file. The file
// is replaced whole on every save: written to a temporary file, flushed to
// disk, then renamed over the old one, so a crash at any moment leaves
// either the old file or the new one, never a mix.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/learned-query-optimizer/lqo/internal/hints"
)

// FormatVersion is the state file's layout version.
const FormatVersion = 1

// File is the whole state file.
type File struct {
	Format  int               `json:"format"`
	Updated time.Time         `json:"updated"`
	Shapes  map[string]*Entry `json:"shapes"`
}

// Entry is the latest finding for one query shape.
type Entry struct {
	ID         string          `json:"id"`
	QueryID    string          `json:"query_id,omitempty"`
	Normalized string          `json:"normalized"`
	Example    string          `json:"example"`
	Database   string          `json:"database"`
	Server     int             `json:"server_version_num"`
	Tested     time.Time       `json:"tested"`
	Verdict    string          `json:"verdict"`
	Reason     string          `json:"reason,omitempty"`
	BaselineMs float64         `json:"baseline_ms,omitempty"`
	Hint       string          `json:"hint,omitempty"`
	Settings   []hints.Setting `json:"settings,omitempty"`
	HintMs     float64         `json:"hint_ms,omitempty"`
	Gain       float64         `json:"gain,omitempty"`
	Change     string          `json:"plan_change,omitempty"`
}

// New is an empty state.
func New() *File { return &File{Format: FormatVersion, Shapes: map[string]*Entry{}} }

// ErrCorrupt means the file exists but can't be read as a state file.
var ErrCorrupt = errors.New("the state file is damaged")

// Load reads a state file. A missing file is an empty state.
func Load(path string) (*File, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return New(), nil
	}
	if err != nil {
		return nil, err
	}
	var f File
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrCorrupt, path, err)
	}
	if f.Format != FormatVersion {
		return nil, fmt.Errorf("%w: %s has format %d; this LQO reads format %d", ErrCorrupt, path, f.Format, FormatVersion)
	}
	if f.Shapes == nil {
		f.Shapes = map[string]*Entry{}
	}
	return &f, nil
}

// LoadOrSetAside reads a state file. If it is damaged, it is renamed to
// path.damaged-<time> so nothing is lost, and an empty state is returned
// with a note saying where the old file went.
func LoadOrSetAside(path string) (*File, string, error) {
	f, err := Load(path)
	if err == nil {
		return f, "", nil
	}
	if !errors.Is(err, ErrCorrupt) {
		return nil, "", err
	}
	aside := fmt.Sprintf("%s.damaged-%s", path, time.Now().UTC().Format("20060102T150405Z"))
	if rerr := os.Rename(path, aside); rerr != nil {
		return nil, "", fmt.Errorf("%v, and it couldn't be moved aside: %w", err, rerr)
	}
	return New(), fmt.Sprintf("%v; it was moved to %s and LQO started a new one", err, aside), nil
}

// Put records the latest finding for a shape, replacing any earlier one.
func (f *File) Put(e *Entry) { f.Shapes[e.ID] = e }

// Sorted lists the entries: faster ones first, by gain, then the rest by ID.
func (f *File) Sorted() []*Entry {
	out := make([]*Entry, 0, len(f.Shapes))
	for _, e := range f.Shapes {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if (a.Verdict == "faster") != (b.Verdict == "faster") {
			return a.Verdict == "faster"
		}
		if a.Gain != b.Gain {
			return a.Gain > b.Gain
		}
		return a.ID < b.ID
	})
	return out
}

// Save writes the state atomically.
func (f *File) Save(path string) error {
	f.Format = FormatVersion
	f.Updated = time.Now().UTC()
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return WriteAtomic(path, b)
}

// WriteAtomic replaces path with data so that a crash leaves either the old
// content or the new, whole.
func WriteAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name) // no-op once renamed
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, 0o644); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	removeStale(dir, filepath.Base(path))
	return syncDir(dir)
}

// removeStale deletes temporary files an earlier, killed save left behind.
// Only files more than ten minutes old go, so a save running at the same
// moment in another process keeps its own.
func removeStale(dir, base string) {
	matches, _ := filepath.Glob(filepath.Join(dir, "."+base+".tmp-*"))
	for _, m := range matches {
		if st, err := os.Stat(m); err == nil && time.Since(st.ModTime()) > 10*time.Minute {
			os.Remove(m)
		}
	}
}
