package profile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	p := NewProfile()
	p.Host = "example.com"
	p.Username = "alice"
	p.AuthMethod = AuthKey
	p.KeyPath = "/home/alice/.ssh/id_ed25519"
	if err := s.Upsert(p); err != nil {
		t.Fatal(err)
	}

	s2, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s2.Get(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Host != p.Host || got.KeyPath != p.KeyPath {
		t.Fatalf("got %+v", got)
	}

	p.Host = "other.example.com"
	if err := s2.Upsert(p); err != nil {
		t.Fatal(err)
	}
	if n := len(s2.List()); n != 1 {
		t.Fatalf("want 1 profile after update, got %d", n)
	}
	if err := s2.Delete(p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s2.Get(p.ID); err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestStoreNeverWritesSecrets(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(dir)
	p := NewProfile()
	p.SavePassword = true
	if err := s.Upsert(p); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "profiles.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{`"password"`, `"passphrase"`} {
		if strings.Contains(string(data), k) {
			t.Fatalf("profiles.json contains %s", k)
		}
	}
}

func TestStoreCorruptFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "profiles.json"), []byte("{nope"), 0o600)
	if _, err := NewStore(dir); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestRecent(t *testing.T) {
	now := time.Now()
	ps := []Profile{
		{ID: "never"},
		{ID: "old", LastUsedAt: now.Add(-2 * time.Hour)},
		{ID: "new", LastUsedAt: now},
		{ID: "mid", LastUsedAt: now.Add(-time.Hour)},
	}
	got := Recent(ps, 2)
	if len(got) != 2 || got[0].ID != "new" || got[1].ID != "mid" {
		t.Fatalf("got %v", got)
	}
}
