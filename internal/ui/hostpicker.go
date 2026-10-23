package ui

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/amirrossein/nscp/internal/profile"
	"github.com/amirrossein/nscp/internal/sshclient"
	"github.com/amirrossein/nscp/internal/vfs"
)

const recentLimit = 8

func (p *Pane) showHostMenu() {
	m := p.m
	items := []*fyne.MenuItem{
		withIcon(fyne.NewMenuItem("This computer", p.setLocal), theme.ComputerIcon()),
	}

	all := m.deps.Profiles.List()
	if recent := profile.Recent(all, recentLimit); len(recent) > 0 {
		items = append(items, fyne.NewMenuItemSeparator())
		for _, pr := range recent {
			items = append(items, p.profileItem(pr))
		}
	}

	items = append(items, fyne.NewMenuItemSeparator())
	if cfg := m.sshConfig(); cfg != nil {
		if aliases := cfg.Aliases(); len(aliases) > 0 {
			var sub []*fyne.MenuItem
			for _, a := range aliases {
				alias := a
				sub = append(sub, withIcon(fyne.NewMenuItem(alias, func() { p.connectAlias(alias) }), theme.StorageIcon()))
			}
			it := withIcon(fyne.NewMenuItem("SSH config", nil), theme.DocumentIcon())
			it.ChildMenu = fyne.NewMenu("", sub...)
			items = append(items, it)
		}
	}
	if len(all) > 0 {
		sorted := append([]profile.Profile(nil), all...)
		sort.Slice(sorted, func(i, j int) bool {
			return strings.ToLower(sorted[i].Title()) < strings.ToLower(sorted[j].Title())
		})
		var sub []*fyne.MenuItem
		for _, pr := range sorted {
			sub = append(sub, p.profileItem(pr))
		}
		it := withIcon(fyne.NewMenuItem("Saved servers", nil), theme.ListIcon())
		it.ChildMenu = fyne.NewMenu("", sub...)
		items = append(items, it)
	}
	items = append(items, withIcon(fyne.NewMenuItem("New connection…", func() {
		p.showConnectDialog(nil)
	}), theme.ContentAddIcon()))
	if p.prof != nil {
		cur := *p.prof
		items = append(items,
			fyne.NewMenuItemSeparator(),
			withIcon(fyne.NewMenuItem(fmt.Sprintf("Edit %q…", cur.Title()), func() { p.showConnectDialog(&cur) }), theme.SettingsIcon()),
			withIcon(fyne.NewMenuItem("Disconnect", p.setLocal), theme.LogoutIcon()),
		)
	}

	c := p.m.win.Canvas()
	pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(p.host).AddXY(0, p.host.Size().Height)
	widget.ShowPopUpMenuAtPosition(fyne.NewMenu("", items...), c, pos)
}

func (p *Pane) profileItem(pr profile.Profile) *fyne.MenuItem {
	label := pr.Title()
	if pr.SSHConfigAlias == "" && pr.DisplayName != "" {
		label += "  —  " + pr.Username + "@" + pr.Host
	}
	return withIcon(fyne.NewMenuItem(label, func() { p.connect(pr) }), theme.StorageIcon())
}

func withIcon(it *fyne.MenuItem, icon fyne.Resource) *fyne.MenuItem {
	it.Icon = icon
	return it
}

func (p *Pane) connectAlias(alias string) {
	store := p.m.deps.Profiles
	pr, ok := store.FindByAlias(alias)
	if !ok {
		pr = profile.NewProfile()
		pr.SSHConfigAlias = alias
		pr.UseAgent = true
		if err := store.Upsert(pr); err != nil {
			p.m.showError(err)
			return
		}
	}
	p.connect(pr)
}

func (p *Pane) connect(pr profile.Profile) {
	m := p.m
	ctx, cancel := context.WithCancel(context.Background())
	user, addr, jump := m.deps.Dialer.Endpoint(pr)
	msg := fmt.Sprintf("Connecting to %s@%s", user, addr)
	if jump != "" {
		msg += "\nvia " + jump
	}
	progress := dialog.NewCustomWithoutButtons("Connecting",
		container.NewVBox(widget.NewLabel(msg), widget.NewProgressBarInfinite()), m.win)
	progress.SetButtons([]fyne.CanvasObject{widget.NewButton("Cancel", cancel)})
	progress.Show()

	go func() {
		defer cancel()
		c, release, err := m.deps.Manager.Acquire(ctx, pr)
		var fsys *vfs.RemoteFS
		var home string
		if err == nil {
			fsys = vfs.NewRemoteFS(c, pr.Title())
			hctx, hcancel := context.WithTimeout(ctx, ioTimeout)
			home, err = fsys.Home(hctx)
			hcancel()
			if err != nil {
				release()
			}
		}
		runOnMain(func() {
			progress.Hide()
			if err != nil {
				if !errors.Is(err, sshclient.ErrCancelled) && !errors.Is(err, context.Canceled) {
					m.showError(connectError(pr, err))
				}
				return
			}
			m.deps.Profiles.Touch(pr.ID, time.Now())
			p.setRemote(pr, c, release, fsys, home)
		})
	}()
}

func connectError(pr profile.Profile, err error) error {
	var mismatch *sshclient.ErrHostKeyMismatch
	if errors.As(err, &mismatch) {
		return fmt.Errorf("WARNING: the host key for %s has changed.\n\nSomeone could be intercepting the connection, or the server was reinstalled.\nReceived %s.\n\nIf the change is expected, remove the old entry from ~/.ssh/known_hosts (or NSCP's known_hosts) and try again", mismatch.Host, mismatch.Fingerprint)
	}
	return fmt.Errorf("could not connect to %s: %w", pr.Title(), err)
}
