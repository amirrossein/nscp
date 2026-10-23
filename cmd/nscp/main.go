package main

import (
	"os"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/dialog"

	"github.com/amirrossein/nscp/internal/credential"
	"github.com/amirrossein/nscp/internal/profile"
	"github.com/amirrossein/nscp/internal/sshclient"
	"github.com/amirrossein/nscp/internal/sshconfig"
	"github.com/amirrossein/nscp/internal/ui"
)

func main() {
	a := app.NewWithID("com.nscp.app")

	dir, err := profile.DefaultDir()
	var store *profile.Store
	if err == nil {
		store, err = profile.NewStore(dir)
	}
	if err != nil {
		fatal(a, err)
	}

	creds := credential.NewKeyringStore()
	dialer := &sshclient.Dialer{
		Creds:      creds,
		Profiles:   store,
		KnownHosts: sshclient.DefaultKnownHosts(dir),
	}
	cfgPath := sshconfig.DefaultPath()
	if cfg, err := sshconfig.Load(cfgPath); err == nil {
		dialer.SetSSHConfig(cfg)
	}

	ui.NewMainWindow(a, ui.Deps{
		Profiles:      store,
		Creds:         creds,
		Dialer:        dialer,
		Manager:       sshclient.NewManager(dialer),
		SSHConfigPath: cfgPath,
	}).ShowAndRun()
}

func fatal(a fyne.App, err error) {
	w := a.NewWindow("NSCP")
	d := dialog.NewError(err, w)
	d.SetOnClosed(func() { os.Exit(1) })
	w.Resize(fyne.NewSize(480, 200))
	d.Show()
	w.ShowAndRun()
	os.Exit(1)
}
