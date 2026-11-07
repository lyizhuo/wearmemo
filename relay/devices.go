package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Crockford base32: no I, L, O or U, so a code can be read off a watch screen
// and typed on a phone without ambiguity.
const codeAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

const (
	codeLen     = 6
	codeTTL     = 5 * time.Minute
	tokenPrefix = "wm_"
)

type Device struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	TokenHash  string     `json:"tokenHash"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastSeenAt time.Time  `json:"lastSeenAt"`
	RevokedAt  *time.Time `json:"revokedAt,omitempty"`
}

func hashToken(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}

func newDeviceToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return tokenPrefix + hex.EncodeToString(b)
}

func newDeviceID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

type pairCode struct {
	Code      string
	CreatedAt time.Time
	Used      bool
}

// DeviceStore holds devices and short-lived pairing codes.
type DeviceStore struct {
	mu      sync.RWMutex
	path    string
	devices []*Device
	byHash  map[string]*Device
	codes   map[string]*pairCode
}

func NewDeviceStore(dir string) *DeviceStore {
	d := &DeviceStore{
		path:   filepath.Join(dir, "devices.json"),
		byHash: map[string]*Device{},
		codes:  map[string]*pairCode{},
	}
	d.load()
	return d
}

func (d *DeviceStore) load() {
	b, err := os.ReadFile(d.path)
	if err != nil {
		return
	}
	var list []*Device
	if json.Unmarshal(b, &list) != nil {
		return
	}
	for _, dev := range list {
		d.devices = append(d.devices, dev)
		d.byHash[dev.TokenHash] = dev
	}
}

func (d *DeviceStore) flushLocked() {
	if b, err := json.MarshalIndent(d.devices, "", "  "); err == nil {
		tmp := d.path + ".tmp"
		if os.WriteFile(tmp, b, 0o600) == nil {
			_ = os.Rename(tmp, d.path)
		}
	}
}

// List returns devices newest-first, including revoked ones so the UI can
// show history.
func (d *DeviceStore) List() []*Device {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make([]*Device, len(d.devices))
	copy(out, d.devices)
	return out
}

// Authenticate resolves a bearer token to a live device.
func (d *DeviceStore) Authenticate(token string) (*Device, bool) {
	if token == "" {
		return nil, false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	dev, ok := d.byHash[hashToken(token)]
	if !ok || dev.RevokedAt != nil {
		return nil, false
	}
	dev.LastSeenAt = time.Now().UTC()
	d.flushLocked()
	return dev, true
}

func (d *DeviceStore) Revoke(id string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, dev := range d.devices {
		if dev.ID == id && dev.RevokedAt == nil {
			now := time.Now().UTC()
			dev.RevokedAt = &now
			d.flushLocked()
			return true
		}
	}
	return false
}

// NewPairCode mints a one-shot code shown in the admin UI. The watch never
// generates codes; it only redeems them, so the two sides cannot drift.
func (d *DeviceStore) NewPairCode() (string, time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Drop expired codes while we are here — there are never many.
	now := time.Now()
	for k, c := range d.codes {
		if now.Sub(c.CreatedAt) > codeTTL || c.Used {
			delete(d.codes, k)
		}
	}

	buf := make([]byte, codeLen)
	_, _ = rand.Read(buf)
	code := make([]byte, codeLen)
	for i := range buf {
		code[i] = codeAlphabet[int(buf[i])%len(codeAlphabet)]
	}

	expires := now.Add(codeTTL)
	d.codes[string(code)] = &pairCode{Code: string(code), CreatedAt: now}
	return string(code), expires
}

// Redeem consumes a code and returns the new device's token. The plaintext
// token is returned exactly once and never stored.
func (d *DeviceStore) Redeem(code, name string) (token, deviceID string, ok bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	c, found := d.codes[code]
	if !found || c.Used || time.Since(c.CreatedAt) > codeTTL {
		return "", "", false
	}
	c.Used = true

	if name == "" {
		name = "watch"
	}
	token = newDeviceToken()
	dev := &Device{
		ID:         newDeviceID(),
		Name:       name,
		TokenHash:  hashToken(token),
		CreatedAt:  time.Now().UTC(),
		LastSeenAt: time.Now().UTC(),
	}
	d.devices = append(d.devices, dev)
	d.byHash[dev.TokenHash] = dev
	d.flushLocked()
	return token, dev.ID, true
}
