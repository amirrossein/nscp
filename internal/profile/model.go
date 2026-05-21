package profile

import (
	"time"

	"github.com/google/uuid"
)

type AuthMethod string

const (
	AuthPassword AuthMethod = "password"
	AuthKey      AuthMethod = "key"
)

type HostKeyRecord struct {
	Algorithm   string `json:"algorithm"`
	Fingerprint string `json:"fingerprint"`
}

type Profile struct {
	ID              string         `json:"id"`
	DisplayName     string         `json:"displayName"`
	Host            string         `json:"host"`
	Port            int            `json:"port"`
	Username        string         `json:"username"`
	AuthMethod      AuthMethod     `json:"authMethod"`
	KeyPath         string         `json:"keyPath,omitempty"`
	SavePassword    bool           `json:"savePassword"`
	SavePassphrase  bool           `json:"savePassphrase"`
	LastUsedAt      time.Time      `json:"lastUsedAt,omitempty"`
	AcceptedHostKey *HostKeyRecord `json:"acceptedHostKey,omitempty"`
}

func NewProfile() Profile {
	return Profile{
		ID:   uuid.New().String(),
		Port: 22,
	}
}

func (p *Profile) KeyringService() string {
	return "nscp:" + p.ID
}
