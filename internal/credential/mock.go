package credential

import "sync"

var _ Store = (*MemStore)(nil)

type MemStore struct {
	mu      sync.Mutex
	secrets map[[2]string]string
}

func NewMemStore() *MemStore {
	return &MemStore{secrets: map[[2]string]string{}}
}

func (m *MemStore) set(service, account, secret string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.secrets[[2]string{service, account}] = secret
	return nil
}

func (m *MemStore) get(service, account string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.secrets[[2]string{service, account}], nil
}

func (m *MemStore) SavePassword(service, secret string) error {
	return m.set(service, accountPassword, secret)
}

func (m *MemStore) LoadPassword(service string) (string, error) {
	return m.get(service, accountPassword)
}

func (m *MemStore) SavePassphrase(service, secret string) error {
	return m.set(service, accountPassphrase, secret)
}

func (m *MemStore) LoadPassphrase(service string) (string, error) {
	return m.get(service, accountPassphrase)
}

func (m *MemStore) Delete(service string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.secrets, [2]string{service, accountPassword})
	delete(m.secrets, [2]string{service, accountPassphrase})
	return nil
}
