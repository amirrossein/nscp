package sshclient

import (
	"context"
	"sync"

	"github.com/amirrossein/nscp/internal/profile"
)

type Manager struct {
	dialer *Dialer
	mu     sync.Mutex
	conns  map[string]*managed
}

type managed struct {
	ready  chan struct{}
	client *Client
	err    error
	refs   int
}

func NewManager(d *Dialer) *Manager {
	return &Manager{dialer: d, conns: map[string]*managed{}}
}

// Acquire returns a shared connection for the profile, dialing it if needed.
// The returned release func must be called exactly once when done.
func (m *Manager) Acquire(ctx context.Context, p profile.Profile) (*Client, func(), error) {
	m.mu.Lock()
	e := m.conns[p.ID]
	if e != nil {
		select {
		case <-e.ready:
			if e.err == nil && isClosed(e.client) {
				delete(m.conns, p.ID)
				e = nil
			}
		default:
		}
	}
	if e != nil {
		e.refs++
		m.mu.Unlock()
		select {
		case <-e.ready:
		case <-ctx.Done():
			m.release(p.ID, e)
			return nil, nil, ctx.Err()
		}
		if e.err != nil {
			return nil, nil, e.err
		}
		return e.client, m.releaser(p.ID, e), nil
	}
	e = &managed{ready: make(chan struct{}), refs: 1}
	m.conns[p.ID] = e
	m.mu.Unlock()

	c, err := m.dialer.Connect(ctx, p)
	m.mu.Lock()
	e.client, e.err = c, err
	m.mu.Unlock()
	close(e.ready)
	if err != nil {
		m.mu.Lock()
		if m.conns[p.ID] == e {
			delete(m.conns, p.ID)
		}
		m.mu.Unlock()
		return nil, nil, err
	}
	return c, m.releaser(p.ID, e), nil
}

func (m *Manager) releaser(id string, e *managed) func() {
	var once sync.Once
	return func() { once.Do(func() { m.release(id, e) }) }
}

func (m *Manager) release(id string, e *managed) {
	m.mu.Lock()
	e.refs--
	last := e.refs <= 0
	if last && m.conns[id] == e {
		delete(m.conns, id)
	}
	m.mu.Unlock()
	if last && e.client != nil {
		e.client.Close()
	}
}

// Retain adds a reference to an already-connected client so it outlives the
// pane that opened it, e.g. while a queued transfer still needs it.
func (m *Manager) Retain(c *Client) (func(), bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, e := range m.conns {
		if e.client == c && !isClosed(c) {
			e.refs++
			return m.releaser(id, e), true
		}
	}
	return nil, false
}

func (m *Manager) CloseAll() {
	m.mu.Lock()
	conns := m.conns
	m.conns = map[string]*managed{}
	m.mu.Unlock()
	for _, e := range conns {
		select {
		case <-e.ready:
			if e.client != nil {
				e.client.Close()
			}
		default:
		}
	}
}

func isClosed(c *Client) bool {
	select {
	case <-c.Done():
		return true
	default:
		return false
	}
}
