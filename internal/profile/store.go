package profile

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var ErrNotFound = errors.New("profile not found")

type Store struct {
	mu       sync.Mutex
	path     string
	profiles []Profile
}

func DefaultDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "nscp"), nil
}

func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create config dir: %w", err)
	}
	s := &Store{path: filepath.Join(dir, "profiles.json")}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read profiles: %w", err)
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &s.profiles); err != nil {
			return nil, fmt.Errorf("parse %s: %w", s.path, err)
		}
	}
	return s, nil
}

func (s *Store) List() []Profile {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Profile(nil), s.profiles...)
}

func (s *Store) Get(id string) (Profile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.profiles {
		if p.ID == id {
			return p, nil
		}
	}
	return Profile{}, ErrNotFound
}

func (s *Store) FindByAlias(alias string) (Profile, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.profiles {
		if p.SSHConfigAlias != "" && p.SSHConfigAlias == alias {
			return p, true
		}
	}
	return Profile{}, false
}

func (s *Store) Upsert(p Profile) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := append([]Profile(nil), s.profiles...)
	replaced := false
	for i := range next {
		if next[i].ID == p.ID {
			next[i] = p
			replaced = true
			break
		}
	}
	if !replaced {
		next = append(next, p)
	}
	return s.commit(next)
}

func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := make([]Profile, 0, len(s.profiles))
	for _, p := range s.profiles {
		if p.ID != id {
			next = append(next, p)
		}
	}
	return s.commit(next)
}

func (s *Store) Touch(id string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := append([]Profile(nil), s.profiles...)
	for i := range next {
		if next[i].ID == id {
			next[i].LastUsedAt = at.UTC()
			return s.commit(next)
		}
	}
	return ErrNotFound
}

func (s *Store) commit(next []Profile) error {
	data, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), "profiles-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), s.path); err != nil {
		return err
	}
	s.profiles = next
	return nil
}
