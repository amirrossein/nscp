package ui

import (
	"context"
	"fmt"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"

	"github.com/amirrossein/nscp/internal/credential"
	"github.com/amirrossein/nscp/internal/profile"
	"github.com/amirrossein/nscp/internal/sshclient"
	"github.com/amirrossein/nscp/internal/sshconfig"
	"github.com/amirrossein/nscp/internal/transfer"
	"github.com/amirrossein/nscp/internal/vfs"
)

const ioTimeout = 30 * time.Second

type Deps struct {
	Profiles      *profile.Store
	Creds         credential.Store
	Dialer        *sshclient.Dialer
	Manager       *sshclient.Manager
	SSHConfigPath string
}

type MainWindow struct {
	app      fyne.App
	win      fyne.Window
	deps     Deps
	prompter *prompter

	panes     [2]*Pane
	queue     *transfer.Queue
	transfers *transfersPanel
	dnd       *dragCoordinator
	jobRefs   map[int64][]func()
}

func NewMainWindow(a fyne.App, deps Deps) *MainWindow {
	m := &MainWindow{app: a, deps: deps, jobRefs: map[int64][]func(){}}
	m.win = a.NewWindow("NSCP")
	m.prompter = newPrompter(m.win)
	deps.Dialer.Prompter = m.prompter

	m.queue = transfer.NewQueue(func(j *transfer.Job) {
		runOnMain(func() { m.jobFinished(j) })
	})
	m.dnd = newDragCoordinator(m)
	m.panes[0] = newPane(m, 0)
	m.panes[1] = newPane(m, 1)
	for _, p := range m.panes {
		p.setLocal()
	}
	m.transfers = newTransfersPanel(m)

	split := container.NewHSplit(m.panes[0].content, m.panes[1].content)
	vsplit := container.NewVSplit(split, m.transfers.content)
	vsplit.Offset = 0.75
	m.win.SetContent(container.NewStack(vsplit, m.dnd.layer))
	m.win.SetOnDropped(m.onOSDrop)
	m.win.Resize(fyne.NewSize(1200, 760))
	m.win.SetOnClosed(func() {
		m.transfers.stop()
		m.queue.Close()
		deps.Manager.CloseAll()
	})
	return m
}

func (m *MainWindow) Window() fyne.Window { return m.win }

func (m *MainWindow) ShowAndRun() { m.win.ShowAndRun() }

func (m *MainWindow) other(p *Pane) *Pane {
	if m.panes[0] == p {
		return m.panes[1]
	}
	return m.panes[0]
}

func (m *MainWindow) sshConfig() *sshconfig.Config {
	cfg, err := sshconfig.Load(m.deps.SSHConfigPath)
	if err != nil {
		return m.deps.Dialer.SSHConfig()
	}
	m.deps.Dialer.SetSSHConfig(cfg)
	return cfg
}

func (m *MainWindow) showError(err error) {
	dialog.ShowError(err, m.win)
}

type pendingItem struct {
	entry     vfs.Entry
	exists    bool
	existing  vfs.Entry
	overwrite bool
}

// startTransfers checks for name conflicts off the main goroutine, asks the
// user about each one, then queues a job per remaining entry.
func (m *MainWindow) startTransfers(src *Pane, srcFS vfs.FS, entries []vfs.Entry, dst *Pane, dstDir string) {
	dstFS := dst.fs
	refs := m.retain(src, srcFS, dst)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), ioTimeout)
		defer cancel()
		items := make([]pendingItem, 0, len(entries))
		for _, e := range entries {
			existing, exists, err := transfer.Conflict(ctx, dstFS, dstDir, e.Name)
			if err != nil {
				runOnMain(func() {
					releaseAll(refs)
					m.showError(fmt.Errorf("check destination: %w", err))
				})
				return
			}
			items = append(items, pendingItem{entry: e, exists: exists, existing: existing})
		}
		runOnMain(func() {
			resolveConflicts(m.win, items, func(chosen []pendingItem) {
				if len(chosen) == 0 {
					releaseAll(refs)
					return
				}
				for i, it := range chosen {
					j := transfer.NewJob(srcFS, it.entry, dstFS, dstDir)
					j.Overwrite = it.overwrite
					if i == 0 {
						m.jobRefs[j.ID] = refs
					} else {
						m.jobRefs[j.ID] = m.retain(src, srcFS, dst)
					}
					m.queue.Submit(j)
				}
				m.transfers.refresh()
			})
		})
	}()
}

func (m *MainWindow) retain(src *Pane, srcFS vfs.FS, dst *Pane) []func() {
	var refs []func()
	for _, p := range []*Pane{src, dst} {
		if p == nil || p.client == nil {
			continue
		}
		if p == src && srcFS != p.fs {
			continue
		}
		if rel, ok := m.deps.Manager.Retain(p.client); ok {
			refs = append(refs, rel)
		}
	}
	return refs
}

func releaseAll(refs []func()) {
	for _, r := range refs {
		r()
	}
}

func (m *MainWindow) jobFinished(j *transfer.Job) {
	releaseAll(m.jobRefs[j.ID])
	delete(m.jobRefs, j.ID)
	for _, p := range m.panes {
		if p.fs == j.Dst {
			p.reload(j.DstDir)
		}
	}
	m.transfers.refresh()
}

func (m *MainWindow) onOSDrop(pos fyne.Position, uris []fyne.URI) {
	target, row := m.hitTest(pos)
	if target == nil {
		return
	}
	go func() {
		local := vfs.LocalFS{}
		var entries []vfs.Entry
		for _, u := range uris {
			if u.Scheme() != "file" {
				continue
			}
			e, err := local.Stat(context.Background(), u.Path())
			if err == nil {
				entries = append(entries, e)
			}
		}
		if len(entries) == 0 {
			return
		}
		runOnMain(func() {
			m.startTransfers(nil, local, entries, target, target.dropDir(row))
		})
	}()
}
