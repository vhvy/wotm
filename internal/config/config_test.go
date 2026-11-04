package config

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func testStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := Initialize(path); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return s, path
}
func TestInitializeDoesNotOverwrite(t *testing.T) {
	s, path := testStore(t)
	before, _, _ := s.Snapshot()
	if err := Initialize(path); err == nil {
		t.Fatal("overwrote existing config")
	}
	after, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	c, _, _ := after.Snapshot()
	if c.Web.Password != before.Web.Password {
		t.Fatal("password replaced")
	}
}
func TestManualReloadAndConflict(t *testing.T) {
	s, path := testStore(t)
	_, revision, _ := s.Snapshot()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	b = []byte(strings.Replace(string(b), "username: admin", "username: operator", 1))
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.Update(revision, func(c *Config) error { c.Web.Username = "stale"; return nil }); !errors.Is(err, ErrConflict) {
		t.Fatalf("wanted conflict, got %v", err)
	}
	c, newRevision, loadError := s.Snapshot()
	if c.Web.Username != "operator" || newRevision == revision || loadError != "" {
		t.Fatal("manual config was not loaded")
	}
	if err := s.Update(newRevision, func(c *Config) error { c.Web.Password = "new-password"; return nil }); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	saved, _, _ := reopened.Snapshot()
	if saved.Web.Password != "new-password" {
		t.Fatal("update not persisted")
	}
}
func TestInvalidManualEditKeepsLastValidAndCannotBeOverwritten(t *testing.T) {
	s, path := testStore(t)
	before, revision, _ := s.Snapshot()
	invalid := []byte("version: 1\nweb:\n  username: admin\n  password: super-secret\n  accidental_field: secret-value\nkeys: []\nhosts: []\n")
	if err := os.WriteFile(path, invalid, 0600); err != nil {
		t.Fatal(err)
	}
	c, rev, loadError := s.Snapshot()
	if rev != revision || c.Web != before.Web || loadError == "" {
		t.Fatal("last valid config not retained")
	}
	if strings.Contains(loadError, "secret") {
		t.Fatal("parse error leaked configuration contents")
	}
	if err := s.Update(revision, func(c *Config) error { return nil }); !errors.Is(err, ErrConflict) {
		t.Fatalf("got %v", err)
	}
	disk, _ := os.ReadFile(path)
	if string(disk) != string(invalid) {
		t.Fatal("invalid manual edit overwritten")
	}
}
func TestValidationRejectsDanglingKeyAndMultipleDocuments(t *testing.T) {
	s, _ := testStore(t)
	c, rev, _ := s.Snapshot()
	err := s.Update(rev, func(c *Config) error {
		c.Hosts = []Host{{ID: "h", Name: "test", Address: "localhost", Port: 22, Username: "root", Auth: Auth{Mode: "key", KeyID: "missing"}}}
		return nil
	})
	if err == nil {
		t.Fatal("dangling key accepted")
	}
	after, _, _ := s.Snapshot()
	if len(after.Hosts) != len(c.Hosts) {
		t.Fatal("invalid mutation committed")
	}
	if _, err := decode([]byte("version: 1\nweb: {username: admin, password: test}\n---\nversion: 1\n")); err == nil {
		t.Fatal("multiple docs accepted")
	}
}
func TestEncryptedPrivateKeyRejected(t *testing.T) {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKeyWithPassphrase(key, "test", []byte("passphrase"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = ParsePrivateKey(string(pem.EncodeToMemory(block)))
	if err == nil || !strings.Contains(err.Error(), "不支持带口令") {
		t.Fatalf("got %v", err)
	}
}
