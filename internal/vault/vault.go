package vault

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var (
	ErrNotFound     = errors.New("vault: credentials not found")
	ErrUnsupported  = errors.New("vault: secure storage unsupported on this platform")
	ErrEmptyRiverID = errors.New("vault: river id required")
	ErrEmptyBlob    = errors.New("vault: empty credential blob")
)

// Store encrypts per-river credential blobs under dataDir/rivers/<id>/credentials.enc.
type Store struct {
	dataDir string
}

func New(dataDir string) (*Store, error) {
	dataDir = strings.TrimSpace(dataDir)
	if dataDir == "" {
		return nil, fmt.Errorf("vault: data dir required")
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "rivers"), 0700); err != nil {
		return nil, fmt.Errorf("vault: create rivers dir: %w", err)
	}
	return &Store{dataDir: dataDir}, nil
}

func (s *Store) pathFor(riverID string) (string, error) {
	id := strings.TrimSpace(riverID)
	if id == "" {
		return "", ErrEmptyRiverID
	}
	if strings.Contains(id, "/") || strings.Contains(id, `\`) || strings.Contains(id, "..") {
		return "", fmt.Errorf("vault: invalid river id %q", riverID)
	}
	dir := filepath.Join(s.dataDir, "rivers", id)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("vault: create river dir: %w", err)
	}
	_ = os.Chmod(dir, 0700)
	return filepath.Join(dir, "credentials.enc"), nil
}

// Put encrypts and atomically writes credentials for a river.
func (s *Store) Put(riverID string, plaintext []byte) error {
	if len(plaintext) == 0 {
		return ErrEmptyBlob
	}
	path, err := s.pathFor(riverID)
	if err != nil {
		return err
	}
	sealed, err := seal(plaintext)
	if err != nil {
		return err
	}
	return writeAtomic(path, sealed)
}

// Get decrypts credentials for a river.
func (s *Store) Get(riverID string) ([]byte, error) {
	path, err := s.pathFor(riverID)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("vault: read: %w", err)
	}
	plain, err := open(raw)
	if err != nil {
		return nil, err
	}
	return plain, nil
}

// Delete removes stored credentials for a river.
func (s *Store) Delete(riverID string) error {
	path, err := s.pathFor(riverID)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("vault: delete: %w", err)
	}
	return nil
}

// Exists reports whether ciphertext is present (does not decrypt).
func (s *Store) Exists(riverID string) bool {
	path, err := s.pathFor(riverID)
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".cred-*.enc")
	if err != nil {
		return fmt.Errorf("vault: temp: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return fmt.Errorf("vault: chmod temp: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("vault: write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("vault: sync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("vault: close temp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("vault: rename: %w", err)
	}
	_ = os.Chmod(path, 0600)
	return nil
}
