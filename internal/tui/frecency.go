package tui

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	frecencyFileName   = "palette-frecency.json"
	frecencyHalfLifeMS = 14 * 24 * 60 * 60 * 1000 // 14 days
)

type frecencyEntry struct {
	Count      int   `json:"count"`
	LastUsedMS int64 `json:"last_used_ms"`
}

type frecencyStore struct {
	path  string
	items map[string]frecencyEntry
	dirty bool
}

func loadFrecency(dataDir string) *frecencyStore {
	s := &frecencyStore{
		items: make(map[string]frecencyEntry),
	}
	if strings.TrimSpace(dataDir) == "" {
		return s
	}
	s.path = filepath.Join(dataDir, frecencyFileName)
	data, err := os.ReadFile(s.path)
	if err != nil {
		return s
	}
	var raw map[string]frecencyEntry
	if err := json.Unmarshal(data, &raw); err != nil || raw == nil {
		return s
	}
	s.items = raw
	return s
}

func (s *frecencyStore) score(key string) float64 {
	if s == nil || key == "" {
		return 0
	}
	e, ok := s.items[key]
	if !ok || e.Count <= 0 {
		return 0
	}
	age := float64(time.Now().UnixMilli() - e.LastUsedMS)
	if age < 0 {
		age = 0
	}
	decay := math.Exp(-math.Ln2 * age / float64(frecencyHalfLifeMS))
	return float64(e.Count) * decay
}

func (s *frecencyStore) bump(key string) {
	if s == nil || key == "" {
		return
	}
	if s.items == nil {
		s.items = make(map[string]frecencyEntry)
	}
	e := s.items[key]
	e.Count++
	e.LastUsedMS = time.Now().UnixMilli()
	s.items[key] = e
	s.dirty = true
}

func (s *frecencyStore) save() {
	if s == nil || !s.dirty || s.path == "" {
		return
	}
	writeFrecencyFile(s.path, s.items)
	s.dirty = false
}

// saveCmd snapshots the store and returns a tea.Cmd that writes it to disk
// off the UI goroutine. The snapshot avoids a data race between this
// background write and later bump()/score() calls mutating s.items on the
// Update goroutine while the write is in flight.
func (s *frecencyStore) saveCmd() tea.Cmd {
	if s == nil || !s.dirty || s.path == "" {
		return nil
	}
	path := s.path
	snapshot := make(map[string]frecencyEntry, len(s.items))
	for k, v := range s.items {
		snapshot[k] = v
	}
	s.dirty = false
	return func() tea.Msg {
		writeFrecencyFile(path, snapshot)
		return nil
	}
}

func writeFrecencyFile(path string, items map[string]frecencyEntry) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
	}
}
