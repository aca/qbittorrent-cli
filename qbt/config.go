package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Instance is a single qBittorrent endpoint as named in the config file.
type Instance struct {
	Name          string `json:"name"`
	Host          string `json:"host"`
	Username      string `json:"username"`
	Password      string `json:"password"`
	TLSSkipVerify bool   `json:"tls_skip_verify"`
	BasicUser     string `json:"basic_user"`
	BasicPass     string `json:"basic_pass"`
}

// Config is the on-disk configuration: a list of instances to multiplex.
type Config struct {
	Instances []Instance `json:"instances"`
}

// defaultConfigPath resolves $XDG_CONFIG_HOME/qbt/config.json, falling back to
// ~/.config/qbt/config.json.
func defaultConfigPath() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "qbt", "config.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "qbt", "config.json"), nil
}

// loadConfig reads and validates the config file at path. If path is empty the
// default location is used.
func loadConfig(path string) (*Config, error) {
	if path == "" {
		p, err := defaultConfigPath()
		if err != nil {
			return nil, err
		}
		path = p
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}

	if len(cfg.Instances) == 0 {
		return nil, fmt.Errorf("config %s has no instances", path)
	}

	seen := map[string]bool{}
	for _, in := range cfg.Instances {
		if in.Name == "" {
			return nil, fmt.Errorf("config has an instance with an empty name")
		}
		if in.Host == "" {
			return nil, fmt.Errorf("instance %q has an empty host", in.Name)
		}
		if seen[in.Name] {
			return nil, fmt.Errorf("duplicate instance name %q", in.Name)
		}
		seen[in.Name] = true
	}

	return &cfg, nil
}
