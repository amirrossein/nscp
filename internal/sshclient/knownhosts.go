package sshclient

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

type ErrHostKeyMismatch struct {
	Host        string
	Fingerprint string
	Known       []string
}

func (e *ErrHostKeyMismatch) Error() string {
	return fmt.Sprintf("host key for %s has changed (got %s); possible man-in-the-middle attack. Remove the old key from known_hosts if the change is expected", e.Host, e.Fingerprint)
}

var ErrHostKeyRejected = errors.New("host key rejected by user")

type ConfirmHostKeyFunc func(host string, key ssh.PublicKey) bool

type KnownHosts struct {
	mu      sync.Mutex
	files   []string
	appFile string
}

func NewKnownHosts(systemFile, appFile string) *KnownHosts {
	return &KnownHosts{files: []string{systemFile, appFile}, appFile: appFile}
}

func DefaultKnownHosts(appDir string) *KnownHosts {
	home, _ := os.UserHomeDir()
	return NewKnownHosts(filepath.Join(home, ".ssh", "known_hosts"), filepath.Join(appDir, "known_hosts"))
}

func (k *KnownHosts) db() (ssh.HostKeyCallback, error) {
	var existing []string
	for _, f := range k.files {
		if _, err := os.Stat(f); err == nil {
			existing = append(existing, f)
		}
	}
	if len(existing) == 0 {
		return func(host string, _ net.Addr, _ ssh.PublicKey) error {
			return &knownhosts.KeyError{}
		}, nil
	}
	return knownhosts.New(existing...)
}

func (k *KnownHosts) Callback(confirm ConfirmHostKeyFunc) (ssh.HostKeyCallback, error) {
	k.mu.Lock()
	db, err := k.db()
	k.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("read known_hosts: %w", err)
	}
	return func(host string, remote net.Addr, key ssh.PublicKey) error {
		err := db(host, remote, key)
		var keyErr *knownhosts.KeyError
		if !errors.As(err, &keyErr) {
			return err
		}
		if len(keyErr.Want) > 0 {
			known := make([]string, len(keyErr.Want))
			for i, w := range keyErr.Want {
				known[i] = ssh.FingerprintSHA256(w.Key)
			}
			return &ErrHostKeyMismatch{Host: host, Fingerprint: ssh.FingerprintSHA256(key), Known: known}
		}
		if confirm == nil || !confirm(host, key) {
			return ErrHostKeyRejected
		}
		return k.add(host, key)
	}, nil
}

func (k *KnownHosts) add(host string, key ssh.PublicKey) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(k.appFile), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(k.appFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintln(f, knownhosts.Line([]string{knownhosts.Normalize(host)}, key))
	return err
}

var probeKey = func() ssh.PublicKey {
	pub, _, _ := ed25519.GenerateKey(nil)
	k, _ := ssh.NewPublicKey(pub)
	return k
}()

// HostKeyAlgorithms returns the algorithms of keys already known for host so
// the handshake negotiates a key we can verify instead of failing as a
// mismatch when the server also offers an unknown algorithm.
func (k *KnownHosts) HostKeyAlgorithms(host string) []string {
	k.mu.Lock()
	db, err := k.db()
	k.mu.Unlock()
	if err != nil {
		return nil
	}
	var keyErr *knownhosts.KeyError
	if !errors.As(db(host, &net.TCPAddr{}, probeKey), &keyErr) {
		return nil
	}
	var algos []string
	seen := map[string]bool{}
	for _, w := range keyErr.Want {
		for _, a := range algorithmsForKeyType(w.Key.Type()) {
			if !seen[a] {
				seen[a] = true
				algos = append(algos, a)
			}
		}
	}
	return algos
}

func algorithmsForKeyType(t string) []string {
	switch t {
	case ssh.KeyAlgoRSA:
		return []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA}
	case ssh.CertAlgoRSAv01:
		return []string{ssh.CertAlgoRSASHA512v01, ssh.CertAlgoRSASHA256v01, ssh.CertAlgoRSAv01}
	}
	if strings.HasPrefix(t, "@") {
		return nil
	}
	return []string{t}
}
