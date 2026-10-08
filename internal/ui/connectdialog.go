package ui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"

	"github.com/amirrossein/nscp/internal/profile"
	"github.com/amirrossein/nscp/internal/sshclient"
)

var authLabels = []string{"SSH agent", "Key file", "Password"}

func authFromLabel(s string) profile.AuthMethod {
	switch s {
	case "SSH agent":
		return profile.AuthAgent
	case "Password":
		return profile.AuthPassword
	}
	return profile.AuthKey
}

func labelFromAuth(a profile.AuthMethod) string {
	switch a {
	case profile.AuthAgent:
		return "SSH agent"
	case profile.AuthPassword:
		return "Password"
	}
	return "Key file"
}

// showConnectDialog creates a new profile (existing == nil) or edits one.
// Profiles imported from ~/.ssh/config keep host, user and port in that file,
// so only the authentication overrides are editable here.
func (p *Pane) showConnectDialog(existing *profile.Profile) {
	m := p.m
	pr := profile.NewProfile()
	pr.AuthMethod = profile.AuthKey
	if existing != nil {
		pr = *existing
	}
	fromConfig := pr.SSHConfigAlias != ""

	name := widget.NewEntry()
	name.SetText(pr.DisplayName)
	name.SetPlaceHolder("optional")
	host := widget.NewEntry()
	host.SetText(pr.Host)
	host.SetPlaceHolder("server.example.com")
	port := widget.NewEntry()
	port.SetText(strconv.Itoa(max(pr.Port, 1)))
	user := widget.NewEntry()
	user.SetText(pr.Username)
	jump := widget.NewEntry()
	jump.SetText(pr.ProxyJump)
	jump.SetPlaceHolder("optional, e.g. user@bastion:22")

	keyPath := widget.NewEntry()
	keyPath.SetText(pr.KeyPath)
	keyPath.SetPlaceHolder("~/.ssh/id_ed25519")
	browse := widget.NewButton("Browse…", func() {
		fd := dialog.NewFileOpen(func(r fyne.URIReadCloser, err error) {
			if err == nil && r != nil {
				keyPath.SetText(r.URI().Path())
				r.Close()
			}
		}, m.win)
		if home, err := os.UserHomeDir(); err == nil {
			if lister, err := storage.ListerForURI(storage.NewFileURI(filepath.Join(home, ".ssh"))); err == nil {
				fd.SetLocation(lister)
			}
		}
		fd.SetView(dialog.ListView)
		fd.Show()
	})
	keyRow := container.NewBorder(nil, nil, nil, browse, keyPath)

	secret := widget.NewPasswordEntry()
	remember := widget.NewCheck("Remember in system keychain", nil)
	useAgent := widget.NewCheck("Also try keys from ssh-agent", nil)
	useAgent.SetChecked(pr.UseAgent)

	keyItem := widget.NewFormItem("Key file", keyRow)
	secretItem := widget.NewFormItem("Passphrase", secret)
	agentItem := widget.NewFormItem("", useAgent)

	auth := widget.NewRadioGroup(authLabels, nil)
	auth.Horizontal = true
	auth.SetSelected(labelFromAuth(pr.AuthMethod))

	form := widget.NewForm()
	updateAuth := func(string) {
		method := authFromLabel(auth.Selected)
		keyRow.Hidden = method != profile.AuthKey
		useAgent.Hidden = method != profile.AuthKey
		secret.Hidden = method == profile.AuthAgent
		remember.Hidden = method == profile.AuthAgent
		keyItem.Text = ""
		if method == profile.AuthKey {
			keyItem.Text = "Key file"
			secretItem.Text = "Passphrase"
			secret.SetPlaceHolder("leave empty to be asked if needed")
		} else if method == profile.AuthPassword {
			secretItem.Text = "Password"
			secret.SetPlaceHolder("leave empty to be asked on connect")
		} else {
			secretItem.Text = ""
		}
		form.Refresh()
	}
	auth.OnChanged = updateAuth

	if fromConfig {
		cu, addr, cj := m.deps.Dialer.Endpoint(pr)
		form.Append("Host", widget.NewLabel(fmt.Sprintf("%s  (%s@%s from ~/.ssh/config)", pr.SSHConfigAlias, cu, addr)))
		if cj != "" && pr.ProxyJump == "" {
			jump.SetPlaceHolder(cj + " (from ~/.ssh/config)")
		}
	} else {
		form.Append("Name", name)
		form.Append("Host", host)
		form.Append("Port", port)
		form.Append("User", user)
	}
	form.Append("Auth", auth)
	form.AppendItem(keyItem)
	form.AppendItem(agentItem)
	form.AppendItem(secretItem)
	form.Append("", remember)
	form.Append("ProxyJump", jump)
	updateAuth("")

	title := "New connection"
	if existing != nil {
		title = "Edit " + pr.Title()
	}
	var d *dialog.CustomDialog
	connectBtn := widget.NewButton("Connect", func() {
		next, err := profileFromForm(pr, fromConfig, name.Text, host.Text, port.Text, user.Text,
			authFromLabel(auth.Selected), keyPath.Text, useAgent.Checked, jump.Text)
		if err != nil {
			dialog.ShowError(err, m.win)
			return
		}
		if err := m.deps.Profiles.Upsert(next); err != nil {
			dialog.ShowError(err, m.win)
			return
		}
		if s := secret.Text; s != "" {
			sec := sshclient.Secret{Value: s, Remember: remember.Checked}
			cu, addr, _ := m.deps.Dialer.Endpoint(next)
			if next.AuthMethod == profile.AuthPassword {
				m.prompter.preset("pw:"+cu+"@"+addr, sec)
			} else {
				m.prompter.preset("pp:"+expandHome(next.KeyPath), sec)
			}
		}
		d.Hide()
		p.connect(next)
	})
	connectBtn.Importance = widget.HighImportance
	buttons := []fyne.CanvasObject{widget.NewButton("Cancel", func() { d.Hide() })}
	if existing != nil {
		buttons = append(buttons, widget.NewButton("Forget server", func() {
			dialog.ShowConfirm("Forget server",
				fmt.Sprintf("Remove %q from NSCP and delete its saved secrets from the keychain?", pr.Title()),
				func(ok bool) {
					if !ok {
						return
					}
					m.deps.Creds.Delete(pr.KeyringService())
					m.deps.Profiles.Delete(pr.ID)
					d.Hide()
				}, m.win)
		}))
	}
	buttons = append(buttons, connectBtn)

	d = dialog.NewCustomWithoutButtons(title, form, m.win)
	d.SetButtons(buttons)
	d.Resize(fyne.NewSize(560, d.MinSize().Height))
	d.Show()
	if !fromConfig {
		m.win.Canvas().Focus(host)
	}
}

func profileFromForm(pr profile.Profile, fromConfig bool, name, host, port, user string,
	auth profile.AuthMethod, keyPath string, useAgent bool, jump string) (profile.Profile, error) {
	if !fromConfig {
		host = strings.TrimSpace(host)
		if host == "" {
			return pr, errors.New("host is required")
		}
		if u, h, ok := strings.Cut(host, "@"); ok && strings.TrimSpace(user) == "" {
			user, host = u, h
		}
		n, err := strconv.Atoi(strings.TrimSpace(port))
		if err != nil || n < 1 || n > 65535 {
			return pr, errors.New("port must be a number between 1 and 65535")
		}
		if strings.TrimSpace(user) == "" {
			return pr, errors.New("user is required")
		}
		pr.DisplayName = strings.TrimSpace(name)
		pr.Host = host
		pr.Port = n
		pr.Username = strings.TrimSpace(user)
	}
	pr.AuthMethod = auth
	pr.KeyPath = ""
	pr.UseAgent = false
	switch auth {
	case profile.AuthKey:
		pr.KeyPath = expandHome(strings.TrimSpace(keyPath))
		pr.UseAgent = useAgent
	case profile.AuthAgent:
		pr.UseAgent = true
	}
	if fromConfig && auth != profile.AuthPassword {
		pr.UseAgent = true
	}
	pr.ProxyJump = strings.TrimSpace(jump)
	return pr, nil
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[1:])
		}
	}
	return p
}
