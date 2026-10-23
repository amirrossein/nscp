// Package sshtest runs an in-process SSH server for tests. Exec requests are
// executed with the local /bin/sh, so SCP works whenever scp is installed.
package sshtest

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"io"
	"net"
	"os/exec"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"
)

type Options struct {
	Password      string
	AuthorizedKey ssh.PublicKey
}

type Server struct {
	Addr    string
	HostKey ssh.Signer
	cfg     *ssh.ServerConfig
	ln      net.Listener
	wg      sync.WaitGroup
}

func NewSigner(t testing.TB) ssh.Signer {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func Start(t testing.TB, opts Options) *Server {
	t.Helper()
	s := &Server{HostKey: NewSigner(t)}
	s.cfg = &ssh.ServerConfig{
		PasswordCallback: func(_ ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
			if opts.Password != "" && string(pw) == opts.Password {
				return nil, nil
			}
			return nil, errDenied
		},
		PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if opts.AuthorizedKey != nil && string(key.Marshal()) == string(opts.AuthorizedKey.Marshal()) {
				return nil, nil
			}
			return nil, errDenied
		},
	}
	s.cfg.AddHostKey(s.HostKey)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s.ln = ln
	s.Addr = ln.Addr().String()
	s.wg.Add(1)
	go s.accept()
	t.Cleanup(s.Close)
	return s
}

type denied struct{}

func (denied) Error() string { return "denied" }

var errDenied = denied{}

func (s *Server) Close() {
	s.ln.Close()
	s.wg.Wait()
}

func (s *Server) accept() {
	defer s.wg.Done()
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.serve(c)
	}
}

func (s *Server) serve(c net.Conn) {
	sc, chans, reqs, err := ssh.NewServerConn(c, s.cfg)
	if err != nil {
		c.Close()
		return
	}
	defer sc.Close()
	go func() {
		for r := range reqs {
			if r.WantReply {
				r.Reply(r.Type == "keepalive@openssh.com", nil)
			}
		}
	}()
	for nc := range chans {
		switch nc.ChannelType() {
		case "session":
			go handleSession(nc)
		case "direct-tcpip":
			go handleDirect(nc)
		default:
			nc.Reject(ssh.UnknownChannelType, "unsupported")
		}
	}
}

func handleSession(nc ssh.NewChannel) {
	ch, reqs, err := nc.Accept()
	if err != nil {
		return
	}
	defer ch.Close()
	for r := range reqs {
		if r.Type != "exec" {
			if r.WantReply {
				r.Reply(false, nil)
			}
			continue
		}
		var payload struct{ Cmd string }
		ssh.Unmarshal(r.Payload, &payload)
		r.Reply(true, nil)
		cmd := exec.Command("/bin/sh", "-c", payload.Cmd)
		cmd.Stdout = ch
		cmd.Stderr = ch.Stderr()
		stdin, _ := cmd.StdinPipe()
		go func() {
			io.Copy(stdin, ch)
			stdin.Close()
		}()
		status := uint32(0)
		if err := cmd.Run(); err != nil {
			status = 1
			if ee, ok := err.(*exec.ExitError); ok {
				status = uint32(ee.ExitCode())
			}
		}
		b := make([]byte, 4)
		binary.BigEndian.PutUint32(b, status)
		ch.SendRequest("exit-status", false, b)
		return
	}
}

func handleDirect(nc ssh.NewChannel) {
	var p struct {
		Host     string
		Port     uint32
		OrigHost string
		OrigPort uint32
	}
	if err := ssh.Unmarshal(nc.ExtraData(), &p); err != nil {
		nc.Reject(ssh.ConnectionFailed, err.Error())
		return
	}
	target, err := net.Dial("tcp", net.JoinHostPort(p.Host, itoa(p.Port)))
	if err != nil {
		nc.Reject(ssh.ConnectionFailed, err.Error())
		return
	}
	ch, reqs, err := nc.Accept()
	if err != nil {
		target.Close()
		return
	}
	go ssh.DiscardRequests(reqs)
	go func() {
		io.Copy(target, ch)
		target.Close()
	}()
	io.Copy(ch, target)
	ch.Close()
}

func itoa(n uint32) string {
	var b [10]byte
	i := len(b)
	for {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
		if n == 0 {
			break
		}
	}
	return string(b[i:])
}
