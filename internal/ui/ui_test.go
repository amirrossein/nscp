package ui

import (
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/amirrossein/nscp/internal/credential"
	"github.com/amirrossein/nscp/internal/profile"
	"github.com/amirrossein/nscp/internal/sshclient"
)

var mainQueue = make(chan func(), 4096)

func TestMain(m *testing.M) {
	runOnMain = func(f func()) { mainQueue <- f }
	os.Exit(m.Run())
}

// pump plays the role of the Fyne event loop on the test goroutine.
func pump(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for !cond() {
		select {
		case f := <-mainQueue:
			f()
		case <-time.After(5 * time.Millisecond):
		case <-deadline:
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

func newTestWindow(t *testing.T) *MainWindow {
	t.Helper()
	a := test.NewTempApp(t)
	dir := t.TempDir()
	store, err := profile.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	creds := credential.NewMemStore()
	d := &sshclient.Dialer{Creds: creds, Profiles: store,
		KnownHosts: sshclient.NewKnownHosts(filepath.Join(dir, "a"), filepath.Join(dir, "b"))}
	m := NewMainWindow(a, Deps{
		Profiles: store, Creds: creds, Dialer: d,
		Manager: sshclient.NewManager(d), SSHConfigPath: filepath.Join(dir, "ssh_config"),
	})
	m.win.Resize(fyne.NewSize(1200, 760))
	m.win.Show()
	t.Cleanup(func() { m.win.Close() })
	return m
}

func names(p *Pane, dir string) []string {
	var out []string
	for _, id := range p.childUIDs(dir) {
		if e, ok := p.entries[id]; ok {
			out = append(out, e.Name)
		} else {
			out = append(out, id)
		}
	}
	return out
}

func TestBothPanesStartLocalAtHome(t *testing.T) {
	m := newTestWindow(t)
	home, _ := os.UserHomeDir()
	for _, p := range m.panes {
		if p.fs.Label() != "Local" || p.root != home || p.hostBtn.Text != "Local  ▾" {
			t.Fatalf("pane %d: label=%s root=%s", p.idx, p.fs.Label(), p.root)
		}
	}
}

func TestPaneLazyLoadAndNavigate(t *testing.T) {
	m := newTestWindow(t)
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "dir", "inner"), 0o755)
	os.WriteFile(filepath.Join(root, "b.txt"), []byte("b"), 0o644)
	os.WriteFile(filepath.Join(root, ".hidden"), nil, 0o644)

	p := m.panes[0]
	p.navigate(root)
	if got := p.childUIDs(root); len(got) != 1 || !strings.HasPrefix(got[0], placeholderPrefix+"loading:") {
		t.Fatalf("expected loading placeholder, got %q", got)
	}
	pump(t, "root listing", func() bool { _, ok := p.children[root]; return ok })
	if got := strings.Join(names(p, root), ","); got != "dir,b.txt" {
		t.Fatalf("children = %s", got)
	}
	p.toggleHidden()
	if got := strings.Join(names(p, root), ","); got != "dir,.hidden,b.txt" {
		t.Fatalf("with hidden = %s", got)
	}

	dir := filepath.Join(root, "dir")
	if !p.isBranch(dir) || p.isBranch(filepath.Join(root, "b.txt")) {
		t.Fatal("isBranch wrong")
	}
	p.childUIDs(dir)
	pump(t, "subdir listing", func() bool { _, ok := p.children[dir]; return ok })
	if got := strings.Join(names(p, dir), ","); got != "inner" {
		t.Fatalf("subdir children = %s", got)
	}

	p.navigate(dir)
	if p.pathEntry.Text != dir {
		t.Fatalf("path entry = %q", p.pathEntry.Text)
	}
	p.goUp()
	if p.root != root {
		t.Fatalf("goUp root = %q", p.root)
	}

	p.goToTyped(filepath.Join(root, "b.txt"))
	pump(t, "error dialog", func() bool { return len(m.win.Canvas().Overlays().List()) > 0 })
	if p.root != root {
		t.Fatal("navigating to a file must not change root")
	}
}

func TestListingErrorShowsPlaceholder(t *testing.T) {
	m := newTestWindow(t)
	p := m.panes[0]
	missing := filepath.Join(t.TempDir(), "gone")
	p.navigate(missing)
	p.childUIDs(missing)
	pump(t, "listing error", func() bool { return p.errs[missing] != nil })
	if got := p.childUIDs(missing); len(got) != 1 || !strings.HasPrefix(got[0], placeholderPrefix+"error:") {
		t.Fatalf("got %q", got)
	}
}

func TestDragFileToOtherPane(t *testing.T) {
	m := newTestWindow(t)
	src, dst := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(src, "hello.txt"), []byte("hi there"), 0o644)
	os.Mkdir(filepath.Join(dst, "target"), 0o755)

	left, right := m.panes[0], m.panes[1]
	left.navigate(src)
	right.navigate(dst)
	left.childUIDs(src)
	right.childUIDs(dst)
	pump(t, "listings", func() bool {
		_, a := left.children[src]
		_, b := right.children[dst]
		return a && b
	})
	left.tree.Refresh()
	right.tree.Refresh()

	var targetRow *fileRow
	pump(t, "rendered rows", func() bool {
		for r := range right.rows {
			if r.uid == filepath.Join(dst, "target") {
				targetRow = r
				return true
			}
		}
		return false
	})

	drv := fyne.CurrentApp().Driver()
	over := drv.AbsolutePositionForObject(targetRow).AddXY(10, targetRow.Size().Height/2)
	if p, r := m.hitTest(over); p != right || r != targetRow {
		t.Fatalf("hitTest = pane %v row %v", p, r)
	}
	if p, _ := m.hitTest(fyne.NewPos(1, 1)); p != nil {
		t.Fatal("point outside panes must not hit")
	}

	m.dnd.dragged(left, filepath.Join(src, "hello.txt"), &fyne.DragEvent{PointEvent: fyne.PointEvent{AbsolutePosition: over}})
	if !right.frame.Visible() || m.dnd.hoverRow != targetRow {
		t.Fatal("drop target not highlighted")
	}
	m.dnd.dragEnd()
	if right.frame.Visible() {
		t.Fatal("highlight not cleared")
	}

	want := filepath.Join(dst, "target", "hello.txt")
	pump(t, "copied file", func() bool {
		b, err := os.ReadFile(want)
		return err == nil && string(b) == "hi there"
	})

	// Dropping the same file again must ask before overwriting.
	m.dnd.dragged(left, filepath.Join(src, "hello.txt"), &fyne.DragEvent{PointEvent: fyne.PointEvent{AbsolutePosition: over}})
	m.dnd.dragEnd()
	pump(t, "conflict dialog", func() bool { return len(m.win.Canvas().Overlays().List()) > 0 })
}

func TestPickRow(t *testing.T) {
	view := box{fyne.NewPos(0, 0), fyne.NewSize(100, 100)}
	rows := []box{
		{fyne.NewPos(0, 0), fyne.NewSize(100, 20)},
		{fyne.NewPos(0, 20), fyne.NewSize(100, 20)},
		{fyne.NewPos(0, 120), fyne.NewSize(100, 20)},
	}
	cases := []struct {
		pos  fyne.Position
		want int
	}{
		{fyne.NewPos(5, 5), 0},
		{fyne.NewPos(5, 25), 1},
		{fyne.NewPos(5, 125), -1},
		{fyne.NewPos(5, 60), -1},
	}
	for _, c := range cases {
		if got := pickRow(view, rows, c.pos); got != c.want {
			t.Errorf("pickRow(%v) = %d, want %d", c.pos, got, c.want)
		}
	}
}

func TestResolveConflictsWithoutConflicts(t *testing.T) {
	var got []pendingItem
	resolveConflicts(nil, []pendingItem{{}, {}}, func(c []pendingItem) { got = c })
	if len(got) != 2 {
		t.Fatalf("got %d", len(got))
	}
}

func TestProfileFromForm(t *testing.T) {
	pr := profile.NewProfile()
	got, err := profileFromForm(pr, false, "", "alice@example.com", "2222", "", profile.AuthKey, "~/.ssh/id", true, "")
	if err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	if got.Username != "alice" || got.Host != "example.com" || got.Port != 2222 || got.KeyPath != filepath.Join(home, ".ssh/id") || !got.UseAgent {
		t.Fatalf("got %+v", got)
	}
	if _, err := profileFromForm(pr, false, "", "h", "99999", "u", profile.AuthPassword, "", false, ""); err == nil {
		t.Fatal("bad port accepted")
	}
}

func TestHumanBytes(t *testing.T) {
	cases := map[int64]string{0: "0 B", 1023: "1023 B", 1536: "1.5 KiB", 5 << 30: "5.0 GiB"}
	for in, want := range cases {
		if got := humanBytes(in); got != want {
			t.Errorf("humanBytes(%d) = %s, want %s", in, got, want)
		}
	}
}

// TestScreenshot writes a render of the main window when NSCP_SCREENSHOT is set.
func TestScreenshot(t *testing.T) {
	out := os.Getenv("NSCP_SCREENSHOT")
	if out == "" {
		t.Skip("set NSCP_SCREENSHOT=path.png to render")
	}
	m := newTestWindow(t)
	root := t.TempDir()
	for _, d := range []string{"projects/nscp", "photos", "notes"} {
		os.MkdirAll(filepath.Join(root, d), 0o755)
	}
	os.WriteFile(filepath.Join(root, "report.pdf"), make([]byte, 240<<10), 0o644)
	os.WriteFile(filepath.Join(root, "todo.txt"), []byte("x"), 0o644)
	for _, p := range m.panes {
		p.navigate(root)
		p.childUIDs(root)
	}
	pump(t, "listings", func() bool {
		_, a := m.panes[0].children[root]
		_, b := m.panes[1].children[root]
		return a && b
	})
	m.panes[0].tree.OpenBranch(filepath.Join(root, "projects"))
	m.panes[0].childUIDs(filepath.Join(root, "projects"))
	pump(t, "branch", func() bool { _, ok := m.panes[0].children[filepath.Join(root, "projects")]; return ok })
	m.panes[0].tree.Refresh()
	m.panes[1].tree.Refresh()
	f, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	png.Encode(f, m.win.Canvas().Capture())
}
