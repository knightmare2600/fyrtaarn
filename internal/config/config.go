package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// RecentConnection is one entry in the last-N-connected-to list shown under
// File > Recent Connections. Only IP and username are persisted — passwords
// are never written to disk.
type RecentConnection struct {
	IP       string `json:"ip"`
	Username string `json:"username,omitempty"`
}

// Favourite is one user-pinned host shown under File > Favourites. Only the
// display name, IP and username are persisted — passwords are never written
// to disk. Name is optional; entries saved before it existed have none.
type Favourite struct {
	Name     string `json:"name,omitempty"`
	IP       string `json:"ip"`
	Username string `json:"username,omitempty"`
}

type Config struct {
	Theme             string             `json:"theme"`
	LastSubnet        string             `json:"last_subnet"`
	LastPorts         string             `json:"last_ports"`
	NVDAPIKey         string             `json:"nvd_api_key,omitempty"`
	RecentConnections []RecentConnection `json:"recent_connections,omitempty"`
	Favourites        []Favourite        `json:"favourites,omitempty"`
}

func path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "fyrtaarn", "config.json"), nil
}

func Load() Config {
	p, err := path()
	if err != nil {
		return Config{}
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return Config{}
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}
	}
	return cfg
}

func Save(cfg Config) error {
	p, err := path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600)
}

// Update loads the config as currently on disk, applies mutate to it, and
// writes the result back. Any field mutate doesn't touch is preserved
// exactly as it was on disk — callers only need to know about the field(s)
// they're changing, not reconstruct the whole struct. This is the only
// correct way to persist a single setting: building a fresh Config{...}
// literal at the call site silently drops every other saved field back to
// its zero value.
func Update(mutate func(*Config)) error {
	cfg := Load()
	mutate(&cfg)
	return Save(cfg)
}
