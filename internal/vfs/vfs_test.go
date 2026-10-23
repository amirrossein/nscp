package vfs

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestShellQuote(t *testing.T) {
	cases := map[string]string{
		"plain":       "'plain'",
		"it's":        `'it'\''s'`,
		"$(rm -rf /)": "'$(rm -rf /)'",
		"":            "''",
	}
	for in, want := range cases {
		if got := ShellQuote(in); got != want {
			t.Errorf("ShellQuote(%q) = %s, want %s", in, got, want)
		}
	}
	if _, err := exec.LookPath("sh"); err == nil {
		for in := range cases {
			out, err := exec.Command("sh", "-c", "printf %s "+ShellQuote(in)).Output()
			if err != nil || string(out) != in {
				t.Errorf("round trip %q: got %q (%v)", in, out, err)
			}
		}
	}
}

func TestParseList(t *testing.T) {
	raw := strings.Join([]string{
		"file with spaces.txt", "f", "f", "12", "644", "1700000000.5",
		"line\nbreak", "f", "f", "1", "600", "1700000000",
		"it's a dir", "d", "d", "4096", "755", "1700000000",
		"link-to-dir", "l", "d", "7", "777", "1700000000",
		"broken", "l", "N", "3", "777", "1700000000",
	}, "\x00") + "\x00"
	es, err := ParseList([]byte(raw), "/srv")
	if err != nil {
		t.Fatal(err)
	}
	if len(es) != 5 {
		t.Fatalf("got %d entries", len(es))
	}
	if es[0].Name != "file with spaces.txt" || es[0].Size != 12 || es[0].Mode != 0o644 || es[0].Path != "/srv/file with spaces.txt" {
		t.Errorf("entry 0 = %+v", es[0])
	}
	if es[1].Name != "line\nbreak" {
		t.Errorf("newline name = %q", es[1].Name)
	}
	if !es[2].IsDir || es[2].IsLink {
		t.Errorf("dir = %+v", es[2])
	}
	if !es[3].IsDir || !es[3].IsLink {
		t.Errorf("link to dir = %+v", es[3])
	}
	if es[4].IsDir || !es[4].IsLink {
		t.Errorf("broken link = %+v", es[4])
	}

	if es, err := ParseList(nil, "/srv"); err != nil || len(es) != 0 {
		t.Errorf("empty dir: %v %v", es, err)
	}
	if _, err := ParseList([]byte("a\x00b\x00"), "/"); err == nil {
		t.Error("expected error for truncated output")
	}
}

func TestParseWalk(t *testing.T) {
	raw := strings.Join([]string{
		"a", "d", "d", "4096", "755",
		"a/x.txt", "f", "f", "5", "600",
		"a/ln", "l", "f", "5", "777",
		"a/dl", "l", "d", "2", "777",
		"fifo", "p", "p", "0", "644",
	}, "\x00") + "\x00"
	items, warnings, linked, err := parseWalk([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 || len(warnings) != 2 || len(linked) != 1 || items[linked[0]].Rel != "a/ln" {
		t.Fatalf("items=%+v warnings=%v linked=%v", items, warnings, linked)
	}
}

type fakeConn struct {
	cmds []string
	out  []byte
	err  error
}

func (f *fakeConn) Run(_ context.Context, cmd string) ([]byte, error) {
	f.cmds = append(f.cmds, cmd)
	return f.out, f.err
}

func (f *fakeConn) RawSSHClient() *ssh.Client { return nil }


func TestRemoteCommandsQuotePaths(t *testing.T) {
	fc := &fakeConn{}
	r := NewRemoteFS(fc, "srv")
	ctx := context.Background()
	r.List(ctx, "/tmp/it's")
	r.Mkdir(ctx, "/tmp/a b")
	r.Rename(ctx, "/tmp/a", "/tmp/$(b)")
	r.Remove(ctx, "/tmp/x")
	want := []string{
		`find '/tmp/it'\''s' -mindepth 1 -maxdepth 1 -printf`,
		`mkdir -- '/tmp/a b'`,
		`mv -- '/tmp/a' '/tmp/$(b)'`,
		`rm -rf -- '/tmp/x'`,
	}
	for i, w := range want {
		if !strings.Contains(fc.cmds[i], w) {
			t.Errorf("cmd %d = %q, want it to contain %q", i, fc.cmds[i], w)
		}
	}
	if err := r.Remove(ctx, "/"); err == nil {
		t.Error("removing / must be refused")
	}
}

func TestRemoteClean(t *testing.T) {
	r := NewRemoteFS(&fakeConn{}, "srv")
	cases := map[string]string{
		"~":         "/home/u",
		"~/x/../y":  "/home/u/y",
		"rel":       "/home/u/rel",
		"/abs/./p/": "/abs/p",
	}
	for in, want := range cases {
		if got := r.Clean(in, "/home/u"); got != want {
			t.Errorf("Clean(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLocalFS(t *testing.T) {
	ctx := context.Background()
	l := LocalFS{}
	root := t.TempDir()
	if err := l.Mkdir(ctx, filepath.Join(root, "sub")); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, "sub", "f.txt"), []byte("hello"), 0o640)
	os.WriteFile(filepath.Join(root, "B.txt"), []byte("x"), 0o644)
	os.Symlink(filepath.Join(root, "sub"), filepath.Join(root, "sublink"))
	os.Symlink(filepath.Join(root, "sub", "f.txt"), filepath.Join(root, "sub", "flink"))

	es, err := l.List(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range es {
		names = append(names, e.Name)
	}
	if strings.Join(names, ",") != "sub,sublink,B.txt" {
		t.Fatalf("sorted names = %v", names)
	}
	if !es[1].IsDir || !es[1].IsLink {
		t.Errorf("sublink = %+v", es[1])
	}

	items, warnings, err := l.Walk(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]WalkItem{}
	for _, it := range items {
		got[it.Rel] = it
	}
	if !got["sub"].IsDir || got["sub/f.txt"].Size != 5 || got["sub/flink"].Size != 5 || got["sub/f.txt"].Mode != 0o640 {
		t.Errorf("walk items = %+v", items)
	}
	if _, ok := got["sublink"]; ok || len(warnings) != 1 {
		t.Errorf("symlinked dir should be skipped with a warning: %v", warnings)
	}

	if err := l.Rename(ctx, filepath.Join(root, "B.txt"), filepath.Join(root, "sub")); err == nil {
		t.Error("rename onto existing name must fail")
	}
	if err := l.Rename(ctx, filepath.Join(root, "B.txt"), filepath.Join(root, "C.txt")); err != nil {
		t.Fatal(err)
	}
	if err := l.Remove(ctx, filepath.Join(root, "sub")); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Stat(ctx, filepath.Join(root, "sub")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("stat after remove: %v", err)
	}
	if err := l.Remove(ctx, string(filepath.Separator)); err == nil {
		t.Error("removing root must be refused")
	}
}

func TestValidName(t *testing.T) {
	for _, n := range []string{"", ".", "..", "a/b", "a\nb"} {
		if ValidName(n) {
			t.Errorf("%q should be invalid", n)
		}
	}
	if !ValidName("ok name.txt") {
		t.Error("valid name rejected")
	}
}
