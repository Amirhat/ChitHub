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
	Settings    Settings `json:"settings"`

	// Root is the legacy single-folder field; migrated into Collections on load.
	Root string `json:"root,omitempty"`
}

// Settings holds user preferences exposed in the Settings panel.
type Settings struct {
	Theme          string `json:"theme"`          // "dark" | "light"
	Lang           string `json:"lang"`           // "en" | "fa"
	DefaultPull    string `json:"defaultPull"`    // "ff" | "rebase" | "merge"
	AutoFetchMin   int    `json:"autoFetchMin"`   // background fetch interval, 0 = off
	FontSize       int    `json:"fontSize"`       // base UI font size in px
	WarnMainPush   bool   `json:"warnMainPush"`   // confirm before pushing to main/master
	DiscardToStash bool   `json:"discardToStash"` // discard moves changes to a stash instead of deleting
}

func defaultSettings() Settings {
	return Settings{
		Theme: "dark", Lang: "en", DefaultPull: "ff",
		AutoFetchMin: 0, FontSize: 14, WarnMainPush: true, DiscardToStash: true,
	}
}

// normalize fills any zero-valued setting with its default.
func (s *Settings) normalize() {
	d := defaultSettings()
	if s.Theme == "" {
		s.Theme = d.Theme
	}
	if s.Lang == "" {
		s.Lang = d.Lang
	}
	if s.DefaultPull == "" {
		s.DefaultPull = d.DefaultPull
	}
	if s.FontSize == 0 {
		s.FontSize = d.FontSize
	}
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
	fresh := true
	if b, err := os.ReadFile(configPath()); err == nil {
		_ = json.Unmarshal(b, &c)
		fresh = false
	}
	if c.Root != "" { // migrate legacy field
		c.AddCollection(c.Root)
		c.Root = ""
	}
	if fresh || c.Settings.Theme == "" {
		// First run (or a pre-settings config): seed the full default set so the
		// boolean toggles get their intended `true` defaults.
		s := defaultSettings()
		if c.Settings.DefaultPull != "" {
			s.DefaultPull = c.Settings.DefaultPull
		}
		c.Settings = s
	}
	c.Settings.normalize()
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
