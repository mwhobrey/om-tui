package vault

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestPutGetDelete(t *testing.T) {
	if err := os.Setenv("OPENMESSAGES_VAULT_INSECURE", "1"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Unsetenv("OPENMESSAGES_VAULT_INSECURE") })

	dir := t.TempDir()
	store, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte(`{"token":"xoxp-test"}`)
	if err := store.Put("slack-team1", want); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := store.Get("slack-team1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
	encPath := filepath.Join(dir, "rivers", "slack-team1", "credentials.enc")
	raw, err := os.ReadFile(encPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("xoxp-test")) {
		t.Fatal("plaintext token leaked into ciphertext file")
	}
	if err := store.Delete("slack-team1"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get("slack-team1"); err != ErrNotFound {
		t.Fatalf("Get after delete = %v, want ErrNotFound", err)
	}
}
