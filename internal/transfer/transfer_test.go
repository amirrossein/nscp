package transfer

import (
	"bytes"
	"context"
	"crypto/rand"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/amirrossein/nscp/internal/profile"
	"github.com/amirrossein/nscp/internal/sshclient"
	"github.com/amirrossein/nscp/internal/sshtest"
	"github.com/amirrossein/nscp/internal/vfs"
)

func writeTree(t *testing.T, root string) map[string][]byte {
	t.Helper()
	big := make([]byte, 300<<10)
	rand.Read(big)
	files := map[string][]byte{
		"a.txt":                []byte("alpha"),
		"empty":                {},
		"sub/it's here.bin":    big,
		"sub/deeper/z w$x.txt": []byte("zed"),
	}
	for rel, data := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, data, 0o640); err != nil {
			t.Fatal(err)
		}
	}
	os.MkdirAll(filepath.Join(root, "sub", "emptydir"), 0o755)
	return files
}

func checkTree(t *testing.T, root string, files map[string][]byte) {
	t.Helper()
	for rel, want := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		got, err := os.ReadFile(p)
		if err != nil {
			t.Errorf("%s: %v", rel, err)
			continue
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s: content differs (%d vs %d bytes)", rel, len(got), len(want))
		}
	}
	if fi, err := os.Stat(filepath.Join(root, "sub", "emptydir")); err != nil || !fi.IsDir() {
		t.Errorf("empty dir not created: %v", err)
	}
}

func entry(t *testing.T, f vfs.FS, p string) vfs.Entry {
	t.Helper()
	e, err := f.Stat(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func runJob(t *testing.T, j *Job) Snapshot {
	t.Helper()
	Run(j)
	return j.Snapshot()
}

func TestLocalToLocalDir(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	files := writeTree(t, filepath.Join(src, "tree"))
	l := vfs.LocalFS{}
	j := NewJob(l, entry(t, l, filepath.Join(src, "tree")), l, dst)
	s := runJob(t, j)
	if s.Status != Done {
		t.Fatalf("status %v: %v", s.Status, s.Err)
	}
	checkTree(t, filepath.Join(dst, "tree"), files)
	if s.FilesDone != 4 || s.FilesTotal != 4 || s.BytesDone != s.BytesTotal {
		t.Errorf("progress = %+v", s)
	}
	fi, _ := os.Stat(filepath.Join(dst, "tree", "a.txt"))
	if fi.Mode().Perm() != 0o640 {
		t.Errorf("mode = %v", fi.Mode().Perm())
	}
}

func TestConflictAndOverwrite(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	l := vfs.LocalFS{}
	os.WriteFile(filepath.Join(src, "x"), []byte("new"), 0o644)
	os.MkdirAll(filepath.Join(dst, "x", "inner"), 0o755)

	_, exists, err := Conflict(context.Background(), l, dst, "x")
	if err != nil || !exists {
		t.Fatalf("conflict = %v, %v", exists, err)
	}
	j := NewJob(l, entry(t, l, filepath.Join(src, "x")), l, dst)
	if s := runJob(t, j); s.Status != Failed {
		t.Fatalf("expected failure without overwrite, got %v", s.Status)
	}
	j = NewJob(l, entry(t, l, filepath.Join(src, "x")), l, dst)
	j.Overwrite = true
	if s := runJob(t, j); s.Status != Done {
		t.Fatalf("overwrite: %v %v", s.Status, s.Err)
	}
	if got, _ := os.ReadFile(filepath.Join(dst, "x")); string(got) != "new" {
		t.Fatalf("got %q", got)
	}
}

func TestSelfCopyRejected(t *testing.T) {
	root := t.TempDir()
	l := vfs.LocalFS{}
	os.MkdirAll(filepath.Join(root, "d", "child"), 0o755)
	j := NewJob(l, entry(t, l, filepath.Join(root, "d")), l, filepath.Join(root, "d", "child"))
	if s := runJob(t, j); s.Status != Failed {
		t.Fatalf("copy into itself should fail, got %v", s.Status)
	}
	j = NewJob(l, entry(t, l, filepath.Join(root, "d")), l, root)
	j.Overwrite = true
	if s := runJob(t, j); s.Status != Failed {
		t.Fatalf("copy onto itself should fail, got %v", s.Status)
	}
	if _, err := os.Stat(filepath.Join(root, "d", "child")); err != nil {
		t.Fatal("source was damaged")
	}
}

func TestJobStateMachine(t *testing.T) {
	j := NewJob(vfs.LocalFS{}, vfs.Entry{Name: "x"}, vfs.LocalFS{}, "/")
	j.setDone()
	if j.Status() != Queued {
		t.Fatal("Queued -> Done must be a no-op")
	}
	j.Cancel()
	if j.Status() != Cancelled {
		t.Fatal("cancel while queued")
	}
	if j.setRunning() {
		t.Fatal("cancelled job must not start")
	}
	Run(j)
	if j.Status() != Cancelled {
		t.Fatal("Run must not revive a cancelled job")
	}

	j = NewJob(vfs.LocalFS{}, vfs.Entry{Name: "x"}, vfs.LocalFS{}, "/")
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			j.addBytes(1)
			j.Snapshot()
			j.setRunning()
			j.setFailed(context.Canceled)
		}()
	}
	wg.Wait()
	if s := j.Snapshot(); s.Status != Failed || s.BytesDone != 8 {
		t.Fatalf("snapshot = %+v", s)
	}
}

func TestQueueRunsInOrder(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	l := vfs.LocalFS{}
	var mu sync.Mutex
	var order []string
	done := make(chan struct{})
	q := NewQueue(func(j *Job) {
		mu.Lock()
		order = append(order, j.Source.Name)
		if len(order) == 3 {
			close(done)
		}
		mu.Unlock()
	})
	defer q.Close()
	for _, n := range []string{"1", "2", "3"} {
		os.WriteFile(filepath.Join(src, n), []byte(n), 0o644)
		q.Submit(NewJob(l, entry(t, l, filepath.Join(src, n)), l, dst))
	}
	<-done
	if len(order) != 3 || order[0] != "1" || order[2] != "3" {
		t.Fatalf("order = %v", order)
	}
	q.ClearFinished()
	if len(q.Jobs()) != 0 {
		t.Fatal("finished jobs not cleared")
	}
}

type acceptAll struct{}

func (acceptAll) Password(string, string) (sshclient.Secret, bool) {
	return sshclient.Secret{Value: "pw"}, true
}
func (acceptAll) Passphrase(string) (sshclient.Secret, bool)  { return sshclient.Secret{}, false }
func (acceptAll) ConfirmHostKey(string, ssh.PublicKey) bool { return true }

// remoteFS connects to an in-process server whose "remote" filesystem is
// this machine, so results can be checked with plain os calls.
func remoteFS(t *testing.T) *vfs.RemoteFS {
	t.Helper()
	if _, err := exec.LookPath("scp"); err != nil {
		t.Skip("scp not installed")
	}
	srv := sshtest.Start(t, sshtest.Options{Password: "pw"})
	dir := t.TempDir()
	d := &sshclient.Dialer{
		KnownHosts: sshclient.NewKnownHosts(filepath.Join(dir, "a"), filepath.Join(dir, "b")),
		Prompter:   acceptAll{},
	}
	host, port, _ := net.SplitHostPort(srv.Addr)
	p := profile.NewProfile()
	p.Host = host
	p.Port, _ = strconv.Atoi(port)
	p.Username = "tester"
	p.AuthMethod = profile.AuthPassword
	c, err := d.Connect(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return vfs.NewRemoteFS(c, "test")
}

func requireGNUTools(t *testing.T) {
	if exec.Command("/bin/sh", "-c", "find / -maxdepth 0 -printf '' && stat -L --printf '' /").Run() != nil {
		t.Skip("GNU find/stat not available; covered by the Docker integration tests")
	}
}

func TestLocalToRemoteDir(t *testing.T) {
	r := remoteFS(t)
	src, dst := t.TempDir(), t.TempDir()
	files := writeTree(t, filepath.Join(src, "tree"))
	l := vfs.LocalFS{}
	j := NewJob(l, entry(t, l, filepath.Join(src, "tree")), r, filepath.ToSlash(dst))
	s := runJob(t, j)
	if s.Status != Done {
		t.Fatalf("status %v: %v", s.Status, s.Err)
	}
	checkTree(t, filepath.Join(dst, "tree"), files)
	if s.BytesDone != s.BytesTotal {
		t.Errorf("bytes %d/%d", s.BytesDone, s.BytesTotal)
	}
}

func TestRemoteToLocalAndRelay(t *testing.T) {
	requireGNUTools(t)
	r1, r2 := remoteFS(t), remoteFS(t)
	src, mid, dst := t.TempDir(), t.TempDir(), t.TempDir()
	files := writeTree(t, filepath.Join(src, "tree"))

	j := NewJob(r1, entry(t, r1, filepath.Join(src, "tree")), vfs.LocalFS{}, mid)
	if s := runJob(t, j); s.Status != Done {
		t.Fatalf("download: %v %v", s.Status, s.Err)
	}
	checkTree(t, filepath.Join(mid, "tree"), files)

	j = NewJob(r1, entry(t, r1, filepath.Join(src, "tree")), r2, dst)
	if s := runJob(t, j); s.Status != Done {
		t.Fatalf("relay: %v %v", s.Status, s.Err)
	}
	checkTree(t, filepath.Join(dst, "tree"), files)
}
