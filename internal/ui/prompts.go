package ui

import (
	"fmt"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"golang.org/x/crypto/ssh"

	"github.com/amirrossein/nscp/internal/sshclient"
)

// prompter answers sshclient's questions with modal dialogs. It is always
// called from a connect goroutine and blocks it until the user responds.
type prompter struct {
	win     fyne.Window
	mu      sync.Mutex
	pending map[string]sshclient.Secret
}

var _ sshclient.Prompter = (*prompter)(nil)

func newPrompter(w fyne.Window) *prompter {
	return &prompter{win: w, pending: map[string]sshclient.Secret{}}
}

// preset supplies a secret typed into the connect dialog so the first prompt
// for it is answered without asking again.
func (p *prompter) preset(key string, s sshclient.Secret) {
	p.mu.Lock()
	p.pending[key] = s
	p.mu.Unlock()
}

func (p *prompter) take(key string) (sshclient.Secret, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	s, ok := p.pending[key]
	delete(p.pending, key)
	return s, ok
}

func (p *prompter) Password(user, host string) (sshclient.Secret, bool) {
	if s, ok := p.take("pw:" + user + "@" + host); ok {
		return s, true
	}
	return p.askSecret("Password required", fmt.Sprintf("Password for %s@%s", user, host), "Password")
}

func (p *prompter) Passphrase(keyPath string) (sshclient.Secret, bool) {
	if s, ok := p.take("pp:" + keyPath); ok {
		return s, true
	}
	return p.askSecret("Key passphrase", "Passphrase for "+keyPath, "Passphrase")
}

func (p *prompter) askSecret(title, message, label string) (sshclient.Secret, bool) {
	type result struct {
		s  sshclient.Secret
		ok bool
	}
	ch := make(chan result, 1)
	runOnMain(func() {
		entry := newKeyPasswordEntry()
		remember := widget.NewCheck("Remember in system keychain", nil)
		items := []*widget.FormItem{
			widget.NewFormItem("", widget.NewLabel(message)),
			widget.NewFormItem(label, entry),
			widget.NewFormItem("", remember),
		}
		d := dialog.NewForm(title, "OK", "Cancel", items, func(ok bool) {
			ch <- result{sshclient.Secret{Value: entry.Text, Remember: remember.Checked}, ok && entry.Text != ""}
		}, p.win)
		entry.OnSubmitted = func(string) { d.Submit() }
		entry.OnEscape = d.Hide
		d.Resize(fyne.NewSize(440, d.MinSize().Height))
		d.Show()
		p.win.Canvas().Focus(entry)
	})
	r := <-ch
	return r.s, r.ok
}

func (p *prompter) ConfirmHostKey(host string, key ssh.PublicKey) bool {
	ch := make(chan bool, 1)
	runOnMain(func() {
		msg := fmt.Sprintf("The authenticity of host %s can't be established.\n\n%s key fingerprint:\n%s\n\nTrust this host and continue connecting?",
			host, key.Type(), ssh.FingerprintSHA256(key))
		d := dialog.NewConfirm("Unknown host", msg, func(ok bool) { ch <- ok }, p.win)
		d.SetConfirmText("Trust")
		d.SetDismissText("Cancel")
		d.Show()
	})
	return <-ch
}
