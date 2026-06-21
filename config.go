package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// Config is persisted to ~/.chithub.json. A "collection" is a parent folder that
// contains project repositories; the user can track several and switch between
// them.
type Config struct {
	Collections []string `json:"collections"`
	Active      string   `json:"active"`
	Port        string   `json:"port"`

	// Root is the legacy single-folder field; migrated into Collections on load.
	Root string `json:"root,omitempty"`
}

func configPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".chithub.json"
	}
	return filepath.Join(home, ".chithub.json")
}

func loadConfig() Config {
	var c Config
	if b, err := os.ReadFile(configPath()); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	if c.Root != "" { // migrate legacy field
		c.AddCollection(c.Root)
		c.Root = ""
	}
	return c
}

func saveConfig(c Config) {
	if b, err := json.MarshalIndent(c, "", "  "); err == nil {
		_ = os.WriteFile(configPath(), b, 0o644)
	}
}

// AddCollection adds (if new) an absolute parent folder and makes it active.
func (c *Config) AddCollection(p string) {
	if p == "" {
		return
	}
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	for _, e := range c.Collections {
		if e == p {
			c.Active = p
			return
		}
	}
	c.Collections = append(c.Collections, p)
	sort.Strings(c.Collections)
	c.Active = p
}

// RemoveCollection drops a folder from the tracked list (it is NOT deleted on
// disk). If it was active, the active folder falls back to the first remaining.
func (c *Config) RemoveCollection(p string) {
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	out := c.Collections[:0]
	for _, e := range c.Collections {
		if e != p {
			out = append(out, e)
		}
	}
	c.Collections = out
	if c.Active == p {
		c.Active = ""
		if len(c.Collections) > 0 {
			c.Active = c.Collections[0]
		}
	}
}

// SetActive switches the active collection (adding it if untracked).
func (c *Config) SetActive(p string) {
	c.AddCollection(p)
}
