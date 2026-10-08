package transfer

import "sync"

type Queue struct {
	onFinished func(*Job)

	mu      sync.Mutex
	jobs    []*Job
	pending []*Job
	wake    chan struct{}
	closed  chan struct{}
	done    chan struct{}
}

// NewQueue starts the single worker goroutine. onFinished runs on that
// goroutine after each job reaches a terminal state.
func NewQueue(onFinished func(*Job)) *Queue {
	q := &Queue{
		onFinished: onFinished,
		wake:       make(chan struct{}, 1),
		closed:     make(chan struct{}),
		done:       make(chan struct{}),
	}
	go q.loop()
	return q
}

func (q *Queue) Submit(j *Job) {
	q.mu.Lock()
	q.jobs = append(q.jobs, j)
	q.pending = append(q.pending, j)
	q.mu.Unlock()
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func (q *Queue) Jobs() []*Job {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]*Job(nil), q.jobs...)
}

func (q *Queue) ClearFinished() {
	q.mu.Lock()
	defer q.mu.Unlock()
	kept := q.jobs[:0]
	for _, j := range q.jobs {
		if !j.Status().Terminal() {
			kept = append(kept, j)
		}
	}
	q.jobs = kept
}

func (q *Queue) Close() {
	q.mu.Lock()
	for _, j := range q.jobs {
		j.Cancel()
	}
	q.mu.Unlock()
	close(q.closed)
	<-q.done
}

func (q *Queue) next() *Job {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.pending) == 0 {
		return nil
	}
	j := q.pending[0]
	q.pending = q.pending[1:]
	return j
}

func (q *Queue) loop() {
	defer close(q.done)
	for {
		for j := q.next(); j != nil; j = q.next() {
			select {
			case <-q.closed:
				return
			default:
			}
			Run(j)
			if q.onFinished != nil {
				q.onFinished(j)
			}
		}
		select {
		case <-q.wake:
		case <-q.closed:
			return
		}
	}
}
