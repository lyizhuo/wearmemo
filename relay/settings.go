package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ASRConfig and BlinkoConfig live in the database, not the environment:
// they are edited from the web UI, and changing them must not require a
// container restart.
type ASRConfig struct {
	Base      string   `json:"base"`
	Model     string   `json:"model"`
	KeepAlive string   `json:"keepAlive"`
	Langs     []string `json:"langs"`
	MinRMS    float64  `json:"minRms"`
}

type BlinkoConfig struct {
	Base  string `json:"base"`
	Token string `json:"token"`
}

type Settings struct {
	ASR        ASRConfig    `json:"asr"`
	Blinko     BlinkoConfig `json:"blinko"`
	Configured bool         `json:"configured"`
}

// SettingsStore persists Settings and hands out a snapshot per request.
type SettingsStore struct {
	mu   sync.RWMutex
	path string
	cur  Settings
}

func NewSettingsStore(dir string) *SettingsStore {
	s := &SettingsStore{path: filepath.Join(dir, "settings.json")}
	s.load()
	return s
}

func (s *SettingsStore) load() {
	b, err := os.ReadFile(s.path)
	if err == nil {
		var v Settings
		if json.Unmarshal(b, &v) == nil {
			s.cur = v
			return
		}
	}
	// First boot: seed from env so an existing deployment keeps working,
	// but anything the operator typed in the UI wins from then on.
	s.cur = Settings{
		ASR: ASRConfig{
			Base:      env("ASR_BASE", "http://127.0.0.1:11434"),
			Model:     env("ASR_MODEL", "frozenlab/qwen3-asr:1.7b"),
			KeepAlive: env("ASR_KEEP_ALIVE", "30m"),
			Langs:     splitLangs(env("ASR_LANGS", "Chinese,Cantonese,English")),
			MinRMS:    envFloat("ASR_MIN_RMS", 200),
		},
		Blinko: BlinkoConfig{
			Base:  env("BLINKO_BASE", ""),
			Token: env("BLINKO_TOKEN", ""),
		},
	}
	s.cur.Configured = s.cur.Blinko.Base != "" && s.cur.Blinko.Token != ""
	s.flushLocked()
}

func splitLangs(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (s *SettingsStore) Get() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cur
}

func (s *SettingsStore) Save(v Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	v.Configured = v.Blinko.Base != "" && v.Blinko.Token != ""
	s.cur = v
	return s.flushLocked()
}

func (s *SettingsStore) flushLocked() error {
	b, err := json.MarshalIndent(s.cur, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// ASR builds a client from the current settings, so a model swap in the UI
// takes effect on the next memo.
func (s *SettingsStore) ASRLangs() string { return strings.Join(s.Get().ASR.Langs, ",") }
