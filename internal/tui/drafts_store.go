package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const draftsFileName = "tui_drafts.json"

// loadDrafts reads persistent drafts for all conversations from dataDir/tui_drafts.json.
// If the file doesn't exist or is invalid, it returns an empty map.
func loadDrafts(dataDir string) map[string]string {
	drafts := make(map[string]string)
	dataDir = strings.TrimSpace(dataDir)
	if dataDir == "" {
		return drafts
	}
	raw, err := os.ReadFile(filepath.Join(dataDir, draftsFileName))
	if err != nil {
		return drafts
	}
	var loaded map[string]string
	if err := json.Unmarshal(raw, &loaded); err != nil {
		return drafts
	}
	for id, text := range loaded {
		trimmedID := strings.TrimSpace(id)
		if trimmedID != "" && strings.TrimSpace(text) != "" {
			drafts[trimmedID] = text
		}
	}
	return drafts
}

// saveDrafts atomically writes the drafts map to dataDir/tui_drafts.json.
func saveDrafts(dataDir string, drafts map[string]string) error {
	dataDir = strings.TrimSpace(dataDir)
	if dataDir == "" {
		return nil
	}
	clean := make(map[string]string, len(drafts))
	for id, text := range drafts {
		trimmedID := strings.TrimSpace(id)
		if trimmedID != "" && strings.TrimSpace(text) != "" {
			clean[trimmedID] = text
		}
	}

	raw, err := json.MarshalIndent(clean, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal drafts: %w", err)
	}

	target := filepath.Join(dataDir, draftsFileName)
	tmp := fmt.Sprintf("%s.tmp.%d", target, os.Getpid())
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return fmt.Errorf("write drafts temp file: %w", err)
	}

	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("commit drafts file: %w", err)
	}
	return nil
}
