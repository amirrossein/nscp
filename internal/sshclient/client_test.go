package sshclient

import (
	"context"
	"encoding/pem"
	"errors"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"crypto/ed25519"
	"crypto/rand"

	"golang.org/x/crypto/ssh"

	"github.com/amirrossein/nscp/internal/credential"
	"github.com/amirrossein/nscp/internal/profile"
	"github.com/amirrossein/nscp/internal/sshconfig"
	"github.com/amirrossein/nscp/internal/sshtest"
)

type fakePrompter struct {
	mu          sync.Mutex
	password    Secret
	passphrase  Secret
	accept      bool
	hostPrompts int
	pwPrompts   int
}

func (f *fakePrompter) Password(_, _ string) (Secret, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pwPrompts++
	return f.password, f.password.Value != ""
}

func (f *fakePrompter) Passphrase(string) (Secret, bool) {
	return f.passphrase, f.passphrase.Value != ""
}

func (f *fakePrompter) ConfirmHostKey(string, ssh.PublicKey) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hostPrompts++
	return f.accept
}

func newDialer(t *testing.T, p Prompter) (*Dialer, *profile.Store, *credential.MemStore) {
	dir := t.TempDir()
	store, err := profile.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	creds := credential.NewMemStore()
	return &Dialer{
		Creds:      creds,
		Profiles:   store,
		KnownHosts: NewKnownHosts(filepath.Join(dir, "system_known_hosts"), filepath.Join(dir, "known_hosts")),
		Prompter:   p,
	}, store, creds
}

func profileFor(addr string) profile.Profile {
	host, port, _ := net.SplitHostPort(addr)
	p := profile.NewProfile()
	p.Host = host
	p.Port, _ = strconv.Atoi(port)
	p.Username = "tester"
	p.AuthMethod = profile.AuthPassword
	return p
}

func TestPasswordAuthAndRemember(t *testing.T) {
	srv := sshtest.Start(t, sshtest.Options{Password: "s3cret"})
	fp := &fakePrompter{password: Secret{Value: "s3cret", Remember: true}, accept: true}
	d, store, creds := newDialer(t, fp)
	p := profileFor(srv.Addr)
	store.Upsert(p)

	c, err := d.Connect(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	out, err := c.Run(context.Background(), "printf hello")
	if err != nil || string(out) != "hello" {
		t.Fatalf("run: %q %v", out, err)
	}
	c.Close()

	if pw, _ := creds.LoadPassword(p.KeyringService()); pw != "s3cret" {
		t.Fatalf("password not remembered: %q", pw)
	}
	stored, _ := store.Get(p.ID)
	if !stored.SavePassword {
		t.Fatal("SavePassword flag not set")
	}

	fp.password = Secret{}
	c, err = d.Connect(context.Background(), stored)
	if err != nil {
		t.Fatalf("second connect should use keychain: %v", err)
	}
	c.Close()
	if fp.hostPrompts != 1 {
		t.Fatalf("host key should be prompted once, got %d", fp.hostPrompts)
	}
}

func TestWrongPasswordFails(t *testing.T) {
	srv := sshtest.Start(t, sshtest.Options{Password: "right"})
	d, _, _ := newDialer(t, &fakePrompter{password: Secret{Value: "wrong"}, accept: true})
	if _, err := d.Connect(context.Background(), profileFor(srv.Addr)); err == nil {
		t.Fatal("expected auth failure")
	}
}

func TestPromptCancelled(t *testing.T) {
	srv := sshtest.Start(t, sshtest.Options{Password: "right"})
	d, _, _ := newDialer(t, &fakePrompter{accept: true})
	_, err := d.Connect(context.Background(), profileFor(srv.Addr))
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("want ErrCancelled, got %v", err)
	}
}

func TestHostKeyRejectedAndMismatch(t *testing.T) {
	srv := sshtest.Start(t, sshtest.Options{Password: "pw"})
	fp := &fakePrompter{password: Secret{Value: "pw"}}
	d, _, _ := newDialer(t, fp)
	p := profileFor(srv.Addr)
	if _, err := d.Connect(context.Background(), p); !errors.Is(err, ErrHostKeyRejected) {
		t.Fatalf("want ErrHostKeyRejected, got %v", err)
	}

	other := sshtest.NewSigner(t)
	d.KnownHosts.add(srv.Addr, other.PublicKey())
	fp.hostPrompts = 0
	_, err := d.Connect(context.Background(), p)
	var mismatch *ErrHostKeyMismatch
	if !errors.As(err, &mismatch) {
		t.Fatalf("want mismatch, got %v", err)
	}
	if fp.hostPrompts != 0 {
		t.Fatal("mismatch must not prompt")
	}
}

func writeKey(t *testing.T, passphrase string) (string, ssh.PublicKey) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	var block *pem.Block
	var err error
	if passphrase == "" {
		block, err = ssh.MarshalPrivateKey(priv, "")
	} else {
		block, err = ssh.MarshalPrivateKeyWithPassphrase(priv, "", []byte(passphrase))
	}
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "id_test")
	os.WriteFile(path, pem.EncodeToMemory(block), 0o600)
	signer, _ := ssh.NewSignerFromKey(priv)
	return path, signer.PublicKey()
}

func TestEncryptedKeyAuth(t *testing.T) {
	keyPath, pub := writeKey(t, "open sesame")
	srv := sshtest.Start(t, sshtest.Options{AuthorizedKey: pub})
	fp := &fakePrompter{passphrase: Secret{Value: "open sesame", Remember: true}, accept: true}
	d, store, creds := newDialer(t, fp)
	p := profileFor(srv.Addr)
	p.AuthMethod = profile.AuthKey
	p.KeyPath = keyPath
	store.Upsert(p)
	c, err := d.Connect(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	if pp, _ := creds.LoadPassphrase(p.KeyringService()); pp != "open sesame" {
		t.Fatalf("passphrase not remembered: %q", pp)
	}
	if fp.pwPrompts != 0 {
		t.Fatal("should not prompt for password when key works")
	}
}

func TestProxyJumpViaSSHConfig(t *testing.T) {
	keyPath, pub := writeKey(t, "")
	bastion := sshtest.Start(t, sshtest.Options{AuthorizedKey: pub})
	target := sshtest.Start(t, sshtest.Options{AuthorizedKey: pub})
	bHost, bPort, _ := net.SplitHostPort(bastion.Addr)
	tHost, tPort, _ := net.SplitHostPort(target.Addr)

	cfgText := strings.Join([]string{
		"Host bastion",
		"  HostName " + bHost,
		"  Port " + bPort,
		"  IdentityFile " + keyPath,
		"Host app",
		"  HostName " + tHost,
		"  Port " + tPort,
		"  IdentityFile " + keyPath,
		"  ProxyJump bastion",
	}, "\n")
	cfg, err := sshconfig.Parse(strings.NewReader(cfgText), t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fp := &fakePrompter{accept: true}
	d, _, _ := newDialer(t, fp)
	d.SSHConfig = cfg
	p := profile.NewProfile()
	p.SSHConfigAlias = "app"
	c, err := d.Connect(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if len(c.hops) != 1 {
		t.Fatalf("want 1 hop, got %d", len(c.hops))
	}
	u, _ := user.Current()
	out, err := c.Run(context.Background(), "whoami")
	if err != nil || strings.TrimSpace(string(out)) != u.Username {
		t.Fatalf("run through jump: %q %v", out, err)
	}
	if fp.hostPrompts != 2 {
		t.Fatalf("each hop should verify its host key, got %d prompts", fp.hostPrompts)
	}
}

func TestManagerSharesConnection(t *testing.T) {
	srv := sshtest.Start(t, sshtest.Options{Password: "pw"})
	d, _, _ := newDialer(t, &fakePrompter{password: Secret{Value: "pw"}, accept: true})
	m := NewManager(d)
	p := profileFor(srv.Addr)
	c1, rel1, err := m.Acquire(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	c2, rel2, err := m.Acquire(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if c1 != c2 {
		t.Fatal("expected shared client")
	}
	rel1()
	rel1()
	if isClosed(c1) {
		t.Fatal("closed while still referenced")
	}
	rel2()
	if !isClosed(c1) {
		t.Fatal("not closed after last release")
	}
}
