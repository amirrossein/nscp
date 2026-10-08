package credential

import (
	"errors"

	"github.com/zalando/go-keyring"
)

const (
	accountPassword   = "password"
	accountPassphrase = "passphrase"
)

type Store interface {
	SavePassword(service, secret string) error
	LoadPassword(service string) (string, error)
	SavePassphrase(service, secret string) error
	LoadPassphrase(service string) (string, error)
	Delete(service string) error
}

var _ Store = (*KeyringStore)(nil)

type KeyringStore struct{}

func NewKeyringStore() *KeyringStore { return &KeyringStore{} }

func (KeyringStore) SavePassword(service, secret string) error {
	return keyring.Set(service, accountPassword, secret)
}

func (KeyringStore) LoadPassword(service string) (string, error) {
	return load(service, accountPassword)
}

func (KeyringStore) SavePassphrase(service, secret string) error {
	return keyring.Set(service, accountPassphrase, secret)
}

func (KeyringStore) LoadPassphrase(service string) (string, error) {
	return load(service, accountPassphrase)
}

func (KeyringStore) Delete(service string) error {
	for _, acct := range []string{accountPassword, accountPassphrase} {
		if err := keyring.Delete(service, acct); err != nil && !errors.Is(err, keyring.ErrNotFound) {
			return err
		}
	}
	return nil
}

func load(service, account string) (string, error) {
	s, err := keyring.Get(service, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", nil
	}
	return s, err
}
