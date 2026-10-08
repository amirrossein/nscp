package sshconfig

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseAndResolve(t *testing.T) {
	home := t.TempDir()
	sshDir := filepath.Join(home, ".ssh")
	os.MkdirAll(filepath.Join(sshDir, "conf.d"), 0o700)
	os.WriteFile(filepath.Join(sshDir, "conf.d", "work"), []byte(`
Host bastion
  HostName bastion.corp.example
  User ops

Host db-*
  ProxyJump bastion
`), 0o600)

	cfg := `
# global defaults come first in this file
Include ~/.ssh/conf.d/*

Host web prod-web
  HostName 10.0.0.5
  User = deploy
  Port 2222
  IdentityFile ~/.ssh/id_web
  IdentityFile "%d/.ssh/other key"

Host db-1
  HostName db1.internal

Host !prod-web *
  User fallback
  IdentityFile ~/.ssh/id_default
`
	c, err := Parse(strings.NewReader(cfg), sshDir, home)
	if err != nil {
		t.Fatal(err)
	}

	if got, want := c.Aliases(), []string{"bastion", "web", "prod-web", "db-1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("aliases = %v, want %v", got, want)
	}

	web := c.Resolve("web")
	if web.HostName != "10.0.0.5" || web.User != "deploy" || web.Port != 2222 {
		t.Fatalf("web = %+v", web)
	}
	wantIDs := []string{
		filepath.Join(home, ".ssh", "id_web"),
		filepath.Join(home, ".ssh", "other key"),
		filepath.Join(home, ".ssh", "id_default"),
	}
	if !reflect.DeepEqual(web.IdentityFiles, wantIDs) {
		t.Fatalf("identity files = %v", web.IdentityFiles)
	}

	prod := c.Resolve("prod-web")
	if prod.User != "deploy" || len(prod.IdentityFiles) != 2 {
		t.Fatalf("negated pattern should exclude prod-web from fallback block: %+v", prod)
	}

	db := c.Resolve("db-1")
	if db.ProxyJump != "bastion" || db.HostName != "db1.internal" || db.User != "fallback" {
		t.Fatalf("db-1 = %+v", db)
	}

	unknown := c.Resolve("other")
	if unknown.HostName != "other" || unknown.Port != 22 {
		t.Fatalf("unknown = %+v", unknown)
	}
}

func TestLoadMissingFile(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "nope"))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Aliases()) != 0 {
		t.Fatal("expected no aliases")
	}
}

func TestSplitArgs(t *testing.T) {
	got := splitArgs(`a "b c"  d # comment`)
	if want := []string{"a", "b c", "d"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q", got)
	}
}
