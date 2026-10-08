//go:build integration

package integration

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/amirrossein/nscp/internal/profile"
	"github.com/amirrossein/nscp/internal/sshclient"
	"github.com/amirrossein/nscp/internal/sshconfig"
	"github.com/amirrossein/nscp/internal/transfer"
	"github.com/amirrossein/nscp/internal/vfs"
)

const image = "nscp-it-sshd"

var env struct {
	keyPath     string
	bastionAddr string
	targetAddr  string
}

func TestMain(m *testing.M) {
	code, err := run(m)
	if err != nil {
		fmt.Fprintln(os.Stderr, "integration setup:", err)
		os.Exit(1)
	}
	os.Exit(code)
}

func docker(args ...string) (string, error) {
	out, err := exec.Command("docker", args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out)), nil
}

func run(m *testing.M) (int, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return 0, errors.New("docker is required for integration tests")
	}
	if _, err := docker("build", "-q", "-t", image, "."); err != nil {
		return 0, err
	}
	tmp, err := os.MkdirTemp("", "nscp-it")
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(tmp)

	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	block, _ := ssh.MarshalPrivateKey(priv, "")
	env.keyPath = filepath.Join(tmp, "id_test")
	os.WriteFile(env.keyPath, pem.EncodeToMemory(block), 0o600)
	signer, _ := ssh.NewSignerFromKey(priv)
	authorized := string(ssh.MarshalAuthorizedKey(signer.PublicKey()))

	network := fmt.Sprintf("nscp-it-%d", time.Now().UnixNano())
	if _, err := docker("network", "create", network); err != nil {
		return 0, err
	}
	defer docker("network", "rm", network)

	start := func(alias string) (string, func(), error) {
		id, err := docker("run", "-d", "--rm", "--network", network, "--network-alias", alias,
			"-e", "AUTHORIZED_KEY="+authorized, "-p", "127.0.0.1::22", image)
		if err != nil {
			return "", nil, err
		}
		stop := func() { docker("rm", "-f", id) }
		port, err := docker("port", id, "22/tcp")
		if err != nil {
			stop()
			return "", nil, err
		}
		addr := strings.Split(port, "\n")[0]
		return addr, stop, waitSSH(addr)
	}
	var stopB, stopT func()
	env.bastionAddr, stopB, err = start("bastion")
	if stopB != nil {
		defer stopB()
	}
	if err != nil {
		return 0, err
	}
	env.targetAddr, stopT, err = start("target")
	if stopT != nil {
		defer stopT()
	}
	if err != nil {
		return 0, err
	}
	return m.Run(), nil
}

func waitSSH(addr string) error {
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", addr, time.Second)
		if err == nil {
			buf := make([]byte, 4)
			c.SetReadDeadline(time.Now().Add(2 * time.Second))
			n, _ := c.Read(buf)
			c.Close()
			if n == 4 && string(buf) == "SSH-" {
				return nil
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("sshd at %s did not come up", addr)
}

type prompter struct {
	mu          sync.Mutex
	password    string
	accept      bool
	hostPrompts int
}

func (p *prompter) Password(string, string) (sshclient.Secret, bool) {
	return sshclient.Secret{Value: p.password}, p.password != ""
}
func (p *prompter) Passphrase(string) (sshclient.Secret, bool) { return sshclient.Secret{}, false }
func (p *prompter) ConfirmHostKey(string, ssh.PublicKey) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.hostPrompts++
	return p.accept
}

func dialer(t *testing.T, pr sshclient.Prompter) *sshclient.Dialer {
	dir := t.TempDir()
	return &sshclient.Dialer{
		KnownHosts: sshclient.NewKnownHosts(filepath.Join(dir, "sys"), filepath.Join(dir, "app")),
		Prompter:   pr,
	}
}

func prof(addr string, method profile.AuthMethod) profile.Profile {
	host, port, _ := net.SplitHostPort(addr)
	p := profile.NewProfile()
	p.Host = host
	p.Port, _ = strconv.Atoi(port)
	p.Username = "tester"
	p.AuthMethod = method
	if method == profile.AuthKey {
		p.KeyPath = env.keyPath
	}
	return p
}

func connect(t *testing.T, d *sshclient.Dialer, p profile.Profile) *vfs.RemoteFS {
	t.Helper()
	c, err := d.Connect(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return vfs.NewRemoteFS(c, p.Title())
}

func TestPasswordAndKeyAuth(t *testing.T) {
	pr := &prompter{password: "secret", accept: true}
	d := dialer(t, pr)
	r := connect(t, d, prof(env.bastionAddr, profile.AuthPassword))
	home, err := r.Home(context.Background())
	if err != nil || home != "/home/tester" {
		t.Fatalf("home = %q, %v", home, err)
	}
	connect(t, d, prof(env.bastionAddr, profile.AuthKey))
	if pr.hostPrompts != 1 {
		t.Fatalf("host key should be confirmed once, got %d", pr.hostPrompts)
	}

	bad := dialer(t, &prompter{password: "nope", accept: true})
	if _, err := bad.Connect(context.Background(), prof(env.bastionAddr, profile.AuthPassword)); err == nil {
		t.Fatal("wrong password accepted")
	}
}

func TestProxyJump(t *testing.T) {
	bHost, bPort, _ := net.SplitHostPort(env.bastionAddr)
	cfgText := fmt.Sprintf(`
Host jumpbox
  HostName %s
  Port %s
  User tester
  IdentityFile %s
Host inner
  HostName target
  User tester
  IdentityFile %s
  ProxyJump jumpbox
`, bHost, bPort, env.keyPath, env.keyPath)
	cfg, err := sshconfig.Parse(strings.NewReader(cfgText), t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pr := &prompter{accept: true}
	d := dialer(t, pr)
	d.SetSSHConfig(cfg)
	p := profile.NewProfile()
	p.SSHConfigAlias = "inner"
	r := connect(t, d, p)
	out, err := r.Conn().Run(context.Background(), "hostname -i 2>/dev/null || hostname")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("reached %s through jumpbox", strings.TrimSpace(string(out)))
	if pr.hostPrompts != 2 {
		t.Fatalf("want 2 host key prompts (bastion + target), got %d", pr.hostPrompts)
	}
}

func TestHostKeyMismatch(t *testing.T) {
	// Learn the target container's host key, then record it under the
	// bastion's address so connecting to the bastion presents a different key.
	var targetKey ssh.PublicKey
	tmp := t.TempDir()
	learn := &sshclient.Dialer{
		KnownHosts: sshclient.NewKnownHosts(filepath.Join(tmp, "none"), filepath.Join(tmp, "learn")),
		Prompter:   &captureKey{&targetKey},
	}
	c, err := learn.Connect(context.Background(), prof(env.targetAddr, profile.AuthKey))
	if err != nil {
		t.Fatal(err)
	}
	c.Close()

	forged := filepath.Join(tmp, "forged")
	line := knownhosts.Line([]string{knownhosts.Normalize(env.bastionAddr)}, targetKey)
	os.WriteFile(forged, []byte(line+"\n"), 0o600)
	pr := &prompter{accept: true}
	d := &sshclient.Dialer{KnownHosts: sshclient.NewKnownHosts(forged, filepath.Join(tmp, "app")), Prompter: pr}
	_, err = d.Connect(context.Background(), prof(env.bastionAddr, profile.AuthKey))
	var mismatch *sshclient.ErrHostKeyMismatch
	if !errors.As(err, &mismatch) {
		t.Fatalf("want ErrHostKeyMismatch, got %v", err)
	}
	if pr.hostPrompts != 0 {
		t.Fatal("mismatch must not prompt")
	}
}

type captureKey struct{ key *ssh.PublicKey }

func (c *captureKey) Password(string, string) (sshclient.Secret, bool) {
	return sshclient.Secret{}, false
}
func (c *captureKey) Passphrase(string) (sshclient.Secret, bool) { return sshclient.Secret{}, false }
func (c *captureKey) ConfirmHostKey(_ string, k ssh.PublicKey) bool {
	*c.key = k
	return true
}

func TestRemoteFileOps(t *testing.T) {
	r := connect(t, dialer(t, &prompter{accept: true}), prof(env.bastionAddr, profile.AuthKey))
	ctx := context.Background()
	base := "/home/tester/ops-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	odd := base + "/it's a \"dir\" $(x)"
	if err := r.MkdirAll(ctx, odd); err != nil {
		t.Fatal(err)
	}
	if err := r.Mkdir(ctx, odd); err == nil {
		t.Fatal("Mkdir on existing dir should fail")
	}
	es, err := r.List(ctx, base)
	if err != nil || len(es) != 1 || es[0].Name != "it's a \"dir\" $(x)" || !es[0].IsDir {
		t.Fatalf("list = %+v, %v", es, err)
	}
	renamed := base + "/renamed"
	if err := r.Rename(ctx, odd, renamed); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Stat(ctx, odd); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("old name still exists: %v", err)
	}
	if e, err := r.Stat(ctx, renamed); err != nil || !e.IsDir {
		t.Fatalf("stat renamed: %+v %v", e, err)
	}
	if empty, err := r.List(ctx, renamed); err != nil || len(empty) != 0 {
		t.Fatalf("empty dir listing: %v %v", empty, err)
	}
	if err := r.Remove(ctx, base); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Stat(ctx, base); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("not removed: %v", err)
	}
}

func makeTree(t *testing.T, root string) map[string][]byte {
	big := make([]byte, 3<<20)
	rand.Read(big)
	files := map[string][]byte{
		"a.txt":                   []byte("alpha"),
		"empty":                   {},
		"sub/it's here.bin":       big,
		"sub/deeper/z w$x \".txt": []byte("zed"),
	}
	for rel, data := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, data, 0o640)
	}
	os.MkdirAll(filepath.Join(root, "sub", "emptydir"), 0o755)
	os.Symlink("a.txt", filepath.Join(root, "link-to-a"))
	return files
}

func checkTree(t *testing.T, root string, files map[string][]byte) {
	t.Helper()
	for rel, want := range files {
		got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("%s: mismatch (%d bytes vs %d, err %v)", rel, len(got), len(want), err)
		}
	}
	if b, err := os.ReadFile(filepath.Join(root, "link-to-a")); err != nil || string(b) != "alpha" {
		t.Errorf("symlinked file content: %q %v", b, err)
	}
	if fi, err := os.Stat(filepath.Join(root, "sub", "emptydir")); err != nil || !fi.IsDir() {
		t.Errorf("empty dir missing: %v", err)
	}
}

func runJob(t *testing.T, src vfs.FS, srcPath string, dst vfs.FS, dstDir string) {
	t.Helper()
	e, err := src.Stat(context.Background(), srcPath)
	if err != nil {
		t.Fatal(err)
	}
	j := transfer.NewJob(src, e, dst, dstDir)
	transfer.Run(j)
	s := j.Snapshot()
	if s.Status != transfer.Done {
		t.Fatalf("%s → %s: %v %v", srcPath, dstDir, s.Status, s.Err)
	}
	if s.BytesDone != s.BytesTotal {
		t.Errorf("progress %d/%d", s.BytesDone, s.BytesTotal)
	}
}

func TestUploadDownloadRelay(t *testing.T) {
	d := dialer(t, &prompter{accept: true})
	bastion := connect(t, d, prof(env.bastionAddr, profile.AuthKey))
	target := connect(t, d, prof(env.targetAddr, profile.AuthKey))
	local := vfs.LocalFS{}
	ctx := context.Background()

	src := t.TempDir()
	files := makeTree(t, filepath.Join(src, "tree"))
	remoteDir := "/home/tester/xfer-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	for _, r := range []*vfs.RemoteFS{bastion, target} {
		if err := r.MkdirAll(ctx, remoteDir); err != nil {
			t.Fatal(err)
		}
		defer r.Remove(ctx, remoteDir)
	}

	runJob(t, local, filepath.Join(src, "tree"), bastion, remoteDir)
	runJob(t, bastion, remoteDir+"/tree", target, remoteDir)

	back := t.TempDir()
	runJob(t, target, remoteDir+"/tree", local, back)
	checkTree(t, filepath.Join(back, "tree"), files)

	single := t.TempDir()
	runJob(t, target, remoteDir+"/tree/sub/it's here.bin", local, single)
	if got, _ := os.ReadFile(filepath.Join(single, "it's here.bin")); !bytes.Equal(got, files["sub/it's here.bin"]) {
		t.Error("single file download mismatch")
	}

	e, _ := target.Stat(ctx, remoteDir+"/tree/a.txt")
	if e.Mode != 0o640 {
		t.Errorf("mode not preserved: %v", e.Mode)
	}
}
