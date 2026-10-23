package transfer

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/amirrossein/nscp/internal/vfs"
)

type Status int

const (
	Queued Status = iota
	Running
	Done
	Failed
	Cancelled
)

func (s Status) String() string {
	return [...]string{"Queued", "Running", "Done", "Failed", "Cancelled"}[s]
}

func (s Status) Terminal() bool { return s >= Done }

var nextID atomic.Int64

type Job struct {
	ID        int64
	Src       vfs.FS
	Source    vfs.Entry
	Dst       vfs.FS
	DstDir    string
	Overwrite bool

	ctx    context.Context
	cancel context.CancelFunc

	bytesDone atomic.Int64

	mu         sync.Mutex
	status     Status
	bytesTotal int64
	filesDone  int
	filesTotal int
	current    string
	err        error
	warnings   []string
}

type Snapshot struct {
	Status     Status
	BytesDone  int64
	BytesTotal int64
	FilesDone  int
	FilesTotal int
	Current    string
	Err        error
	Warnings   []string
}

func NewJob(src vfs.FS, source vfs.Entry, dst vfs.FS, dstDir string) *Job {
	ctx, cancel := context.WithCancel(context.Background())
	return &Job{ID: nextID.Add(1), Src: src, Source: source, Dst: dst, DstDir: dstDir, ctx: ctx, cancel: cancel}
}

func (j *Job) DstPath() string { return j.Dst.Join(j.DstDir, j.Source.Name) }

func (j *Job) Snapshot() Snapshot {
	j.mu.Lock()
	defer j.mu.Unlock()
	return Snapshot{
		Status:     j.status,
		BytesDone:  j.bytesDone.Load(),
		BytesTotal: j.bytesTotal,
		FilesDone:  j.filesDone,
		FilesTotal: j.filesTotal,
		Current:    j.current,
		Err:        j.err,
		Warnings:   append([]string(nil), j.warnings...),
	}
}

func (j *Job) Status() Status {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.status
}

func (j *Job) Cancel() {
	j.mu.Lock()
	if j.status == Queued {
		j.status = Cancelled
	}
	j.mu.Unlock()
	j.cancel()
}

func (j *Job) transition(from, to Status) bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.status != from {
		return false
	}
	j.status = to
	return true
}

func (j *Job) setRunning() bool { return j.transition(Queued, Running) }

func (j *Job) setDone() { j.transition(Running, Done) }

func (j *Job) setCancelled() { j.transition(Running, Cancelled) }

func (j *Job) setFailed(err error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.status != Running {
		return
	}
	j.status = Failed
	j.err = err
}

func (j *Job) setPlan(files int, bytes int64, warnings []string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.filesTotal = files
	j.bytesTotal = bytes
	j.warnings = append(j.warnings, warnings...)
}

func (j *Job) startFile(name string) {
	j.mu.Lock()
	j.current = name
	j.mu.Unlock()
}

func (j *Job) finishFile() {
	j.mu.Lock()
	j.filesDone++
	j.mu.Unlock()
}

func (j *Job) addBytes(n int64) { j.bytesDone.Add(n) }
