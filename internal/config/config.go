package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Config struct {
	Theme      string `json:"theme"`
	LastSubnet string `json:"last_subnet"`
	LastPorts  string `json:"last_ports"`
	NVDAPIKey  string `json:"nvd_api_key,omitempty"`
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
