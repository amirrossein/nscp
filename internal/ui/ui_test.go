package ui

import (
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"

	"github.com/amirrossein/nscp/internal/credential"
	"github.com/amirrossein/nscp/internal/profile"
	"github.com/amirrossein/nscp/internal/sshclient"
	"github.com/amirrossein/nscp/internal/transfer"
	"github.com/amirrossein/nscp/internal/vfs"
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
		if p.fs.Label() != "Local" || p.root != home || p.host.title.Text != "This computer" {
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
	if got := strings.Join(names(p, root), ","); got != "dir,.hidden,b.txt" {
		t.Fatalf("hidden files should show by default, got %s", got)
	}
	p.toggleHidden()
	if got := strings.Join(names(p, root), ","); got != "dir,b.txt" {
		t.Fatalf("after hiding = %s", got)
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
	m.panes[1].tree.Select(filepath.Join(root, "report.pdf"))

	dst := t.TempDir()
	local := vfs.LocalFS{}
	ok := transfer.NewJob(local, vfs.Entry{Name: "report.pdf", Path: filepath.Join(root, "report.pdf")}, local, dst)
	bad := transfer.NewJob(local, vfs.Entry{Name: "todo.txt", Path: filepath.Join(root, "todo.txt")}, local, filepath.Join(dst, "missing"))
	waiting := transfer.NewJob(local, vfs.Entry{Name: "photos", Path: filepath.Join(root, "photos"), IsDir: true}, local, dst)
	transfer.Run(ok)
	transfer.Run(bad)
	for _, j := range []*transfer.Job{ok, bad, waiting} {
		m.queue.Submit(j)
	}
	pump(t, "jobs", func() bool { return waiting.Status().Terminal() })
	m.transfers.refresh()

	f, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	png.Encode(f, m.win.Canvas().Capture())
}

func overlays(m *MainWindow) int { return len(m.win.Canvas().Overlays().List()) }

func TestAddressBarClickEditsAndEscapeCancels(t *testing.T) {
	m := newTestWindow(t)
	p := m.panes[0]
	root := t.TempDir()
	p.navigate(root)
	if !p.pathEntry.Hidden {
		t.Fatal("address bar should start in breadcrumb mode")
	}
	var area *tapArea
	for _, o := range test.LaidOutObjects(p.pathStack) {
		if a, ok := o.(*tapArea); ok {
			area = a
		}
	}
	if area == nil {
		t.Fatal("no clickable area behind the breadcrumbs")
	}
	test.Tap(area)
	if p.pathEntry.Hidden || p.pathEntry.Text != root {
		t.Fatalf("click should start editing the current path (hidden=%v text=%q)", p.pathEntry.Hidden, p.pathEntry.Text)
	}
	p.pathEntry.TypedKey(&fyne.KeyEvent{Name: fyne.KeyEscape})
	if !p.pathEntry.Hidden || p.root != root {
		t.Fatal("Escape should leave edit mode without navigating")
	}
}

func TestNewFolderEnterCreatesEscapeCancels(t *testing.T) {
	m := newTestWindow(t)
	p := m.panes[0]
	root := t.TempDir()
	p.navigate(root)

	p.newFolder(root)
	entry, ok := m.win.Canvas().Focused().(*keyEntry)
	if !ok {
		t.Fatal("name field should be focused")
	}
	test.Type(entry, "made-with-enter")
	entry.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	pump(t, "folder created", func() bool {
		fi, err := os.Stat(filepath.Join(root, "made-with-enter"))
		return err == nil && fi.IsDir()
	})

	before := overlays(m)
	p.newFolder(root)
	if overlays(m) == before {
		t.Fatal("dialog did not open")
	}
	entry = m.win.Canvas().Focused().(*keyEntry)
	test.Type(entry, "cancelled")
	entry.TypedKey(&fyne.KeyEvent{Name: fyne.KeyEscape})
	if overlays(m) != before {
		t.Fatal("Escape should close the dialog")
	}
	time.Sleep(50 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(root, "cancelled")); err == nil {
		t.Fatal("Escape must not create the folder")
	}
}

func TestTransfersCollapseAndClear(t *testing.T) {
	m := newTestWindow(t)
	tp := m.transfers
	tp.setCollapsed(false)
	expanded := tp.content.MinSize().Height
	tp.setCollapsed(true)
	if !tp.body.Hidden || tp.content.MinSize().Height >= expanded {
		t.Fatal("collapsing should hide the list and shrink the panel")
	}
	if !fyne.CurrentApp().Preferences().Bool(prefTransfersMin) {
		t.Fatal("collapsed state should be remembered")
	}
	tp.setCollapsed(false)
	if tp.body.Hidden || tp.content.MinSize().Height != expanded {
		t.Fatal("expanding should restore the panel")
	}

	src, dst := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(src, "f"), []byte("x"), 0o644)
	local := vfs.LocalFS{}
	j := transfer.NewJob(local, vfs.Entry{Name: "f", Path: filepath.Join(src, "f")}, local, dst)
	m.queue.Submit(j)
	pump(t, "job", func() bool { return j.Status().Terminal() })
	tp.refresh()
	if tp.clear.Disabled() || len(tp.jobs) != 1 {
		t.Fatal("Clear should be enabled with a finished job")
	}
	test.Tap(tp.clear)
	if len(tp.jobs) != 0 || !tp.clear.Disabled() || tp.empty.Hidden {
		t.Fatal("Clear should empty the list")
	}
}

func TestAddressBarLeavesEditModeWhenClickingElsewhere(t *testing.T) {
	m := newTestWindow(t)
	p := m.panes[0]
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "f.txt"), nil, 0o644)
	p.navigate(root)
	p.childUIDs(root)
	pump(t, "listing", func() bool { _, ok := p.children[root]; return ok })
	p.tree.Refresh()

	editing := func() bool { return !p.pathEntry.Hidden }

	// Clicking empty space: the canvas drops focus.
	p.setEditingPath(true)
	m.win.Canvas().Unfocus()
	pump(t, "exit after focus loss", func() bool { return !editing() })

	// Window losing focus (switching apps) keeps editing.
	p.setEditingPath(true)
	p.pathEntry.FocusLost()
	for i := 0; i < 5; i++ {
		select {
		case f := <-mainQueue:
			f()
		case <-time.After(10 * time.Millisecond):
		}
	}
	if !editing() {
		t.Fatal("a window-level focus loss must not end editing")
	}

	// Clicking a row or the tree's blank area, which keep the entry focused.
	var row *fileRow
	pump(t, "row", func() bool {
		for r := range p.rows {
			if r.uid == filepath.Join(root, "f.txt") {
				row = r
				return true
			}
		}
		return false
	})
	row.MouseDown(&desktop.MouseEvent{Button: desktop.MouseButtonPrimary})
	if editing() || p.selected != row.uid {
		t.Fatal("clicking a row should end editing and select it")
	}
	p.setEditingPath(true)
	test.Tap(p.tree)
	if editing() || p.selected != "" {
		t.Fatal("clicking the blank tree area should end editing and clear the selection")
	}

	// Editing in one pane ends when the other pane is used.
	p.setEditingPath(true)
	test.Tap(m.panes[1].host)
	if editing() {
		t.Fatal("using the other pane should end editing")
	}
}
