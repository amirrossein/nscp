package profile

import (
	"net"
	"strconv"
	"time"

	"github.com/google/uuid"
)

type AuthMethod string

const (
	AuthPassword AuthMethod = "password"
	AuthKey      AuthMethod = "key"
	AuthAgent    AuthMethod = "agent"
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
	UseAgent        bool           `json:"useAgent,omitempty"`
	ProxyJump       string         `json:"proxyJump,omitempty"`
	SSHConfigAlias  string         `json:"sshConfigAlias,omitempty"`
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

func (p *Profile) Addr() string {
	port := p.Port
	if port == 0 {
		port = 22
	}
	return net.JoinHostPort(p.Host, strconv.Itoa(port))
}

func (p *Profile) Title() string {
	if p.DisplayName != "" {
		return p.DisplayName
	}
	if p.SSHConfigAlias != "" {
		return p.SSHConfigAlias
	}
	if p.Username != "" {
		return p.Username + "@" + p.Host
	}
	return p.Host
}
