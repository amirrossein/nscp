package sshclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/amirrossein/nscp/internal/credential"
	"github.com/amirrossein/nscp/internal/profile"
	"github.com/amirrossein/nscp/internal/sshconfig"
)

const (
	DialTimeout       = 15 * time.Second
	keepAliveInterval = 30 * time.Second
)

type Client struct {
	ssh       *ssh.Client
	hops      []*ssh.Client
	done      chan struct{}
	closeOnce sync.Once
}

func newClient(c *ssh.Client, hops []*ssh.Client) *Client {
	cl := &Client{ssh: c, hops: hops, done: make(chan struct{})}
	go func() {
		c.Wait()
		cl.Close()
	}()
	go cl.keepAlive()
	return cl
}

func (c *Client) RawSSHClient() *ssh.Client { return c.ssh }

func (c *Client) Done() <-chan struct{} { return c.done }

func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		c.ssh.Close()
		for i := len(c.hops) - 1; i >= 0; i-- {
			c.hops[i].Close()
		}
		close(c.done)
	})
	return nil
}

func (c *Client) keepAlive() {
	t := time.NewTicker(keepAliveInterval)
	defer t.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-t.C:
			if _, _, err := c.ssh.SendRequest("keepalive@openssh.com", true, nil); err != nil {
				c.Close()
				return
			}
		}
	}
}

type CommandError struct {
	Cmd    string
	Stderr string
	Err    error
}

func (e *CommandError) Error() string {
	if msg := strings.TrimSpace(e.Stderr); msg != "" {
		return msg
	}
	return fmt.Sprintf("remote command failed: %v", e.Err)
}

func (e *CommandError) Unwrap() error { return e.Err }

func (c *Client) Run(ctx context.Context, cmd string) ([]byte, error) {
	s, err := c.ssh.NewSession()
	if err != nil {
		return nil, err
	}
	defer s.Close()
	var stdout, stderr bytes.Buffer
	s.Stdout = &stdout
	s.Stderr = &stderr
	if err := s.Start(cmd); err != nil {
		return nil, err
	}
	waitErr := make(chan error, 1)
	go func() { waitErr <- s.Wait() }()
	select {
	case <-ctx.Done():
		s.Signal(ssh.SIGKILL)
		s.Close()
		return nil, ctx.Err()
	case err := <-waitErr:
		if err != nil {
			return stdout.Bytes(), &CommandError{Cmd: cmd, Stderr: stderr.String(), Err: err}
		}
		return stdout.Bytes(), nil
	}
}

type Dialer struct {
	Creds      credential.Store
	Profiles   *profile.Store
	KnownHosts *KnownHosts
	Prompter   Prompter

	cfgMu     sync.Mutex
	sshConfig *sshconfig.Config
}

func (d *Dialer) SetSSHConfig(c *sshconfig.Config) {
	d.cfgMu.Lock()
	d.sshConfig = c
	d.cfgMu.Unlock()
}

func (d *Dialer) SSHConfig() *sshconfig.Config {
	d.cfgMu.Lock()
	defer d.cfgMu.Unlock()
	return d.sshConfig
}

type hop struct {
	target
	profileID      string
	savePassword   bool
	savePassphrase bool
}

func (d *Dialer) Connect(ctx context.Context, p profile.Profile) (*Client, error) {
	dest, jumpSpec := d.resolveProfile(p)
	if p.SavePassword || p.SavePassphrase {
		if d.Creds != nil {
			if p.SavePassword {
				dest.savedPassword, _ = d.Creds.LoadPassword(p.KeyringService())
			}
			if p.SavePassphrase {
				dest.savedPass, _ = d.Creds.LoadPassphrase(p.KeyringService())
			}
		}
	}
	chain, err := d.resolveJumps(jumpSpec)
	if err != nil {
		return nil, err
	}
	chain = append(chain, dest)

	var prev *ssh.Client
	var opened []*ssh.Client
	fail := func(err error) (*Client, error) {
		for i := len(opened) - 1; i >= 0; i-- {
			opened[i].Close()
		}
		return nil, err
	}
	var lastAuth *authState
	for _, h := range chain {
		c, st, err := d.dialHop(ctx, prev, h)
		if err != nil {
			return fail(err)
		}
		opened = append(opened, c)
		prev = c
		lastAuth = st
	}
	d.rememberSecrets(p, lastAuth)
	return newClient(prev, opened[:len(opened)-1]), nil
}

// Endpoint describes where a profile connects, after applying ~/.ssh/config.
func (d *Dialer) Endpoint(p profile.Profile) (user, addr, proxyJump string) {
	h, jump := d.resolveProfile(p)
	return h.user, h.addr, jump
}

func (d *Dialer) resolveProfile(p profile.Profile) (hop, string) {
	h := hop{profileID: p.ID}
	h.keyring = p.KeyringService()
	jump := p.ProxyJump
	if cfg := d.SSHConfig(); p.SSHConfigAlias != "" && cfg != nil {
		r := cfg.Resolve(p.SSHConfigAlias)
		h.user, h.addr = r.User, net.JoinHostPort(r.HostName, strconv.Itoa(r.Port))
		if p.KeyPath != "" {
			h.identityFiles = append(h.identityFiles, p.KeyPath)
		}
		h.identityFiles = append(h.identityFiles, r.IdentityFiles...)
		if len(h.identityFiles) == 0 {
			h.identityFiles = DefaultIdentityFiles()
		}
		h.useAgent = true
		if p.AuthMethod == profile.AuthPassword {
			h.authMethod = "password"
		}
		if jump == "" {
			jump = r.ProxyJump
		}
		return h, jump
	}
	h.user, h.addr = p.Username, p.Addr()
	h.authMethod = string(p.AuthMethod)
	h.useAgent = p.UseAgent || p.AuthMethod == profile.AuthAgent
	if p.KeyPath != "" {
		h.identityFiles = []string{p.KeyPath}
	} else if p.AuthMethod == profile.AuthKey {
		h.identityFiles = DefaultIdentityFiles()
	}
	return h, jump
}

func (d *Dialer) resolveJumps(spec string) ([]hop, error) {
	if spec == "" || strings.EqualFold(spec, "none") {
		return nil, nil
	}
	var hops []hop
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(part), "ssh://"))
		if part == "" {
			continue
		}
		user, hostport := "", part
		if i := strings.LastIndex(part, "@"); i >= 0 {
			user, hostport = part[:i], part[i+1:]
		}
		host, port := hostport, 0
		if h, p, err := net.SplitHostPort(hostport); err == nil {
			host = h
			if port, err = strconv.Atoi(p); err != nil {
				return nil, fmt.Errorf("invalid ProxyJump port in %q", part)
			}
		}
		h := hop{target: target{useAgent: true, identityFiles: DefaultIdentityFiles()}}
		hostname := host
		if cfg := d.SSHConfig(); cfg != nil {
			r := cfg.Resolve(host)
			hostname = r.HostName
			if user == "" {
				user = r.User
			}
			if port == 0 {
				port = r.Port
			}
			if len(r.IdentityFiles) > 0 {
				h.identityFiles = r.IdentityFiles
			}
		}
		if port == 0 {
			port = 22
		}
		h.user, h.addr = user, net.JoinHostPort(hostname, strconv.Itoa(port))
		hops = append(hops, h)
	}
	return hops, nil
}

func (d *Dialer) dialHop(ctx context.Context, via *ssh.Client, h hop) (*ssh.Client, *authState, error) {
	dialCtx, cancel := context.WithTimeout(ctx, DialTimeout)
	defer cancel()
	var conn net.Conn
	var err error
	if via == nil {
		var nd net.Dialer
		conn, err = nd.DialContext(dialCtx, "tcp", h.addr)
	} else {
		conn, err = via.DialContext(dialCtx, "tcp", h.addr)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("connect %s: %w", h.addr, err)
	}

	var confirm ConfirmHostKeyFunc
	if d.Prompter != nil {
		confirm = d.Prompter.ConfirmHostKey
	}
	cb, err := d.KnownHosts.Callback(confirm)
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	st := newAuthState(h.target, d.Prompter)
	defer st.close()
	cfg := &ssh.ClientConfig{
		User:              h.user,
		Auth:              st.methods(),
		HostKeyCallback:   cb,
		HostKeyAlgorithms: d.KnownHosts.HostKeyAlgorithms(h.addr),
		Timeout:           DialTimeout,
	}

	// The handshake can block on user prompts, so only cancellation (not a
	// timer) is allowed to abort it.
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	c, chans, reqs, err := ssh.NewClientConn(conn, h.addr, cfg)
	stop()
	if err != nil {
		conn.Close()
		var mismatch *ErrHostKeyMismatch
		switch {
		case errors.As(err, &mismatch):
			return nil, nil, mismatch
		case errors.Is(err, ErrHostKeyRejected):
			return nil, nil, ErrHostKeyRejected
		case st.cancelled(), ctx.Err() != nil:
			return nil, nil, ErrCancelled
		}
		return nil, nil, fmt.Errorf("%s@%s: %w", h.user, h.addr, err)
	}
	return ssh.NewClient(c, chans, reqs), st, nil
}

func (d *Dialer) rememberSecrets(p profile.Profile, st *authState) {
	if st == nil || d.Creds == nil || p.ID == "" {
		return
	}
	st.mu.Lock()
	pw := st.password
	var pass *Secret
	for _, s := range st.passphrases {
		s := s
		pass = &s
	}
	st.mu.Unlock()

	changed := false
	if pw != nil && pw.Remember && pw.Value == st.usedPassword {
		if d.Creds.SavePassword(p.KeyringService(), pw.Value) == nil {
			p.SavePassword, changed = true, true
		}
	}
	if pass != nil && pass.Remember {
		if d.Creds.SavePassphrase(p.KeyringService(), pass.Value) == nil {
			p.SavePassphrase, changed = true, true
		}
	}
	if changed && d.Profiles != nil {
		if stored, err := d.Profiles.Get(p.ID); err == nil {
			stored.SavePassword = stored.SavePassword || p.SavePassword
			stored.SavePassphrase = stored.SavePassphrase || p.SavePassphrase
			d.Profiles.Upsert(stored)
		}
	}
}
