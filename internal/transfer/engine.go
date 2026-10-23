package transfer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"

	"github.com/povsister/scp"

	"github.com/amirrossein/nscp/internal/vfs"
)

type fileItem struct {
	src, dst string
	rel      string
	size     int64
	mode     fs.FileMode
}

// Conflict reports whether the job's destination already exists.
func Conflict(ctx context.Context, dst vfs.FS, dstDir, name string) (vfs.Entry, bool, error) {
	e, err := dst.Stat(ctx, dst.Join(dstDir, name))
	if errors.Is(err, fs.ErrNotExist) {
		return vfs.Entry{}, false, nil
	}
	if err != nil {
		return vfs.Entry{}, false, err
	}
	return e, true, nil
}

func Run(j *Job) {
	if !j.setRunning() {
		return
	}
	err := run(j)
	switch {
	case err == nil:
		j.setDone()
	case errors.Is(err, context.Canceled) || j.ctx.Err() != nil:
		j.setCancelled()
	default:
		j.setFailed(err)
	}
}

func run(j *Job) error {
	ctx := j.ctx
	if !vfs.ValidName(j.Source.Name) {
		return fmt.Errorf("unsupported file name %q", j.Source.Name)
	}
	if err := checkSelfCopy(j); err != nil {
		return err
	}
	dstRoot := j.DstPath()

	if existing, exists, err := Conflict(ctx, j.Dst, j.DstDir, j.Source.Name); err != nil {
		return err
	} else if exists {
		if !j.Overwrite {
			return fmt.Errorf("%s already exists", j.Source.Name)
		}
		if existing.IsDir != j.Source.IsDir || existing.IsLink {
			if err := j.Dst.Remove(ctx, dstRoot); err != nil {
				return fmt.Errorf("replace %s: %w", j.Source.Name, err)
			}
		}
	}

	var dirs []string
	var files []fileItem
	var total int64
	if j.Source.IsDir {
		items, warnings, err := j.Src.Walk(ctx, j.Source.Path)
		if err != nil {
			return err
		}
		dirs = append(dirs, dstRoot)
		for _, it := range items {
			if !vfs.ValidName(path.Base(it.Rel)) {
				warnings = append(warnings, fmt.Sprintf("skipped unsupported name %q", it.Rel))
				continue
			}
			d := j.Dst.Join(dstRoot, it.Rel)
			if it.IsDir {
				dirs = append(dirs, d)
				continue
			}
			files = append(files, fileItem{src: j.Src.Join(j.Source.Path, it.Rel), dst: d, rel: it.Rel, size: it.Size, mode: it.Mode})
			total += it.Size
		}
		j.setPlan(len(files), total, warnings)
	} else {
		size, mode, err := j.Src.SizeOf(ctx, j.Source.Path)
		if err != nil {
			return err
		}
		files = []fileItem{{src: j.Source.Path, dst: dstRoot, rel: j.Source.Name, size: size, mode: mode}}
		j.setPlan(1, size, nil)
	}

	if err := mkdirAll(ctx, j.Dst, dirs); err != nil {
		return err
	}
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		j.startFile(f.rel)
		if err := copyFile(ctx, j, f); err != nil {
			removePartial(j.Dst, f.dst)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("%s: %w", f.rel, err)
		}
		j.finishFile()
	}
	return nil
}

func checkSelfCopy(j *Job) error {
	if !sameFS(j.Src, j.Dst) {
		return nil
	}
	dst := j.DstPath()
	if dst == j.Source.Path {
		return errors.New("source and destination are the same")
	}
	if j.Source.IsDir {
		sep := "/"
		if j.Dst.Kind() == vfs.Local {
			sep = string(os.PathSeparator)
		}
		if strings.HasPrefix(j.DstDir+sep, strings.TrimSuffix(j.Source.Path, sep)+sep) {
			return errors.New("cannot copy a folder into itself")
		}
	}
	return nil
}

func sameFS(a, b vfs.FS) bool {
	if a.Kind() != b.Kind() {
		return false
	}
	if a.Kind() == vfs.Local {
		return true
	}
	ra, rb := a.(*vfs.RemoteFS), b.(*vfs.RemoteFS)
	return ra.Conn() == rb.Conn()
}

func mkdirAll(ctx context.Context, dst vfs.FS, dirs []string) error {
	if len(dirs) == 0 {
		return nil
	}
	r, ok := dst.(*vfs.RemoteFS)
	if !ok {
		for _, d := range dirs {
			if err := dst.MkdirAll(ctx, d); err != nil {
				return err
			}
		}
		return nil
	}
	// Batch to keep round trips low while staying well under ARG_MAX.
	const batchBytes = 64 << 10
	var b strings.Builder
	flush := func() error {
		if b.Len() == 0 {
			return nil
		}
		_, err := r.Conn().Run(ctx, "mkdir -p --"+b.String())
		b.Reset()
		return err
	}
	for _, d := range dirs {
		b.WriteString(" ")
		b.WriteString(vfs.ShellQuote(d))
		if b.Len() > batchBytes {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	return flush()
}

func removePartial(dst vfs.FS, p string) {
	dst.Remove(context.Background(), p)
}

func copyFile(ctx context.Context, j *Job, f fileItem) error {
	switch {
	case j.Src.Kind() == vfs.Local && j.Dst.Kind() == vfs.Local:
		return localToLocal(ctx, j, f)
	case j.Src.Kind() == vfs.Local:
		return localToRemote(ctx, j, f)
	case j.Dst.Kind() == vfs.Local:
		return remoteToLocal(ctx, j, f)
	default:
		return remoteToRemote(ctx, j, f)
	}
}

func localToLocal(ctx context.Context, j *Job, f fileItem) error {
	in, err := os.Open(f.src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(f.dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.mode|0o200)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, &progressReader{ctx: ctx, r: in, job: j}); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chmod(f.dst, f.mode)
}

func localToRemote(ctx context.Context, j *Job, f fileItem) error {
	in, err := os.Open(f.src)
	if err != nil {
		return err
	}
	defer in.Close()
	return upload(ctx, j, f, in)
}

func remoteToLocal(ctx context.Context, j *Job, f fileItem) error {
	out, err := os.OpenFile(f.dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.mode|0o200)
	if err != nil {
		return err
	}
	err = download(ctx, j.Src.(*vfs.RemoteFS), f.src, &progressWriter{w: out, job: j})
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Chmod(f.dst, f.mode)
}

// remoteToRemote relays through this machine: the download's output is piped
// straight into the upload so nothing touches the local disk.
func remoteToRemote(ctx context.Context, j *Job, f fileItem) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	pr, pw := io.Pipe()
	dlErr := make(chan error, 1)
	go func() {
		err := download(ctx, j.Src.(*vfs.RemoteFS), f.src, pw)
		if err != nil {
			pw.CloseWithError(err)
		} else {
			pw.Close()
		}
		dlErr <- err
	}()
	upErr := upload(ctx, j, f, pr)
	pr.CloseWithError(errors.New("upload finished"))
	if upErr != nil {
		cancel()
	}
	if err := <-dlErr; err != nil && upErr == nil {
		return err
	}
	return upErr
}

// The scp library wraps the remote path in single quotes without escaping, so
// we cd into the directory via a properly quoted RemoteBinary prefix and only
// hand it a bare file name.
func scpClient(r *vfs.RemoteFS, dir string) (*scp.Client, error) {
	return scp.NewClientFromExistingSSH(r.Conn().RawSSHClient(), &scp.ClientOption{
		RemoteBinary: "cd " + vfs.ShellQuote(dir) + " && scp",
	})
}

func upload(ctx context.Context, j *Job, f fileItem, r io.Reader) error {
	dst := j.Dst.(*vfs.RemoteFS)
	c, err := scpClient(dst, dst.Dir(f.dst))
	if err != nil {
		return err
	}
	mode := f.mode
	if mode == 0 {
		mode = scp.DefaultFilePerm
	}
	body := &sizedReader{progressReader: progressReader{ctx: ctx, r: io.LimitReader(r, f.size), job: j}, size: f.size}
	return c.CopyToRemote(body, dst.Base(f.dst), &scp.FileTransferOption{Context: ctx, Perm: mode})
}

func download(ctx context.Context, src *vfs.RemoteFS, p string, w io.Writer) error {
	c, err := scpClient(src, src.Dir(p))
	if err != nil {
		return err
	}
	name := "./" + strings.ReplaceAll(src.Base(p), "'", `'\''`)
	return c.CopyFromRemote(name, w, &scp.FileTransferOption{Context: ctx})
}

type progressReader struct {
	ctx context.Context
	r   io.Reader
	job *Job
}

func (p *progressReader) Read(b []byte) (int, error) {
	if err := p.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := p.r.Read(b)
	p.job.addBytes(int64(n))
	return n, err
}

type sizedReader struct {
	progressReader
	size int64
}

func (s *sizedReader) Size() int64 { return s.size }

type progressWriter struct {
	w   io.Writer
	job *Job
}

func (p *progressWriter) Write(b []byte) (int, error) {
	n, err := p.w.Write(b)
	p.job.addBytes(int64(n))
	return n, err
}
