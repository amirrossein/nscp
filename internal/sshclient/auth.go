package sshclient

import (
	"crypto/x509"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

var ErrCancelled = errors.New("cancelled by user")

type Secret struct {
	Value    string
	Remember bool
}

type Prompter interface {
	Password(user, host string) (Secret, bool)
	Passphrase(keyPath string) (Secret, bool)
	ConfirmHostKey(host string, key ssh.PublicKey) bool
}

type target struct {
	user          string
	addr          string
	keyring       string
	authMethod    string
	identityFiles []string
	useAgent      bool
	savedPassword string
	savedPass     string
}

type authState struct {
	t        target
	prompter Prompter

	mu            sync.Mutex
	password      *Secret
	usedPassword  string
	passphrases   map[string]Secret
	agentConn     net.Conn
	userCancelled bool
}

func newAuthState(t target, p Prompter) *authState {
	return &authState{t: t, prompter: p, passphrases: map[string]Secret{}}
}

func (a *authState) methods() []ssh.AuthMethod {
	var m []ssh.AuthMethod
	if a.t.authMethod != "password" {
		if a.t.authMethod == "key" || a.t.authMethod == "" {
			m = append(m, ssh.PublicKeysCallback(a.keySigners))
		}
		if a.t.useAgent || a.t.authMethod == "agent" {
			if s := a.agentSigners(); s != nil {
				m = append(m, ssh.PublicKeysCallback(s))
			}
		}
	}
	m = append(m,
		ssh.PasswordCallback(a.passwordFn),
		ssh.KeyboardInteractive(a.keyboardInteractive),
	)
	return m
}

func (a *authState) agentSigners() func() ([]ssh.Signer, error) {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return nil
	}
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return nil
	}
	a.agentConn = conn
	return agent.NewClient(conn).Signers
}

func (a *authState) keySigners() ([]ssh.Signer, error) {
	var signers []ssh.Signer
	for _, path := range a.t.identityFiles {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		signer, err := ssh.ParsePrivateKey(data)
		var missing *ssh.PassphraseMissingError
		if errors.As(err, &missing) {
			signer, err = a.decryptKey(path, data)
		}
		if err != nil {
			continue
		}
		signers = append(signers, signer)
	}
	return signers, nil
}

func (a *authState) decryptKey(path string, data []byte) (ssh.Signer, error) {
	if a.t.savedPass != "" {
		if s, err := ssh.ParsePrivateKeyWithPassphrase(data, []byte(a.t.savedPass)); err == nil {
			return s, nil
		}
	}
	for attempt := 0; attempt < 3; attempt++ {
		if a.prompter == nil {
			return nil, ErrCancelled
		}
		sec, ok := a.prompter.Passphrase(path)
		if !ok {
			a.markCancelled()
			return nil, ErrCancelled
		}
		s, err := ssh.ParsePrivateKeyWithPassphrase(data, []byte(sec.Value))
		if err == nil {
			a.mu.Lock()
			a.passphrases[path] = sec
			a.mu.Unlock()
			return s, nil
		}
		if !errors.Is(err, x509.IncorrectPasswordError) {
			return nil, err
		}
	}
	return nil, errors.New("too many wrong passphrases")
}

func (a *authState) passwordFn() (string, error) {
	return a.nextPassword()
}

// nextPassword offers the keychain password first and asks the user on every
// later call, so a stale saved password falls through to a prompt.
func (a *authState) nextPassword() (string, error) {
	a.mu.Lock()
	saved := a.t.savedPassword
	a.t.savedPassword = ""
	a.mu.Unlock()
	if saved != "" {
		a.mu.Lock()
		a.usedPassword = saved
		a.mu.Unlock()
		return saved, nil
	}
	if a.prompter == nil {
		return "", ErrCancelled
	}
	sec, ok := a.prompter.Password(a.t.user, a.t.addr)
	if !ok {
		a.markCancelled()
		return "", ErrCancelled
	}
	a.mu.Lock()
	a.password = &sec
	a.usedPassword = sec.Value
	a.mu.Unlock()
	return sec.Value, nil
}

func (a *authState) keyboardInteractive(_, _ string, questions []string, echos []bool) ([]string, error) {
	answers := make([]string, len(questions))
	for i := range questions {
		if echos[i] {
			continue
		}
		pw, err := a.nextPassword()
		if err != nil {
			return nil, err
		}
		answers[i] = pw
	}
	return answers, nil
}

func (a *authState) markCancelled() {
	a.mu.Lock()
	a.userCancelled = true
	a.mu.Unlock()
}

func (a *authState) cancelled() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.userCancelled
}

func (a *authState) close() {
	if a.agentConn != nil {
		a.agentConn.Close()
	}
}

func DefaultIdentityFiles() []string {
	home, _ := os.UserHomeDir()
	var out []string
	for _, n := range []string{"id_ed25519", "id_ecdsa", "id_rsa"} {
		out = append(out, filepath.Join(home, ".ssh", n))
	}
	return out
}
