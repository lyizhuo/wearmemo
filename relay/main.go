package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Addr       string
	AdminToken string
	DataDir    string
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envFloat(key string, def float64) float64 {
	if v := os.Getenv(key); v != "" {
		return envFloatValue(v, def)
	}
	return def
}

func envFloatValue(raw string, def float64) float64 {
	if f, err := strconv.ParseFloat(strings.TrimSpace(raw), 64); err == nil {
		return f
	}
	return def
}

func loadConfig() Config {
	return Config{
		Addr:       env("RELAY_ADDR", ":8080"),
		AdminToken: env("RELAY_ADMIN_TOKEN", ""),
		DataDir:    env("RELAY_DATA", "./data"),
	}
}

func newID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("[relay] ")

	cfg := loadConfig()

	// Refusing to boot without an admin token is deliberate: an open-source
	// service that falls back to admin/admin is how boxes get owned.
	if len(cfg.AdminToken) < 12 {
		log.Fatal("RELAY_ADMIN_TOKEN must be set to at least 12 characters")
	}
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		log.Fatalf("data dir: %v", err)
	}

	store, err := NewStore(cfg.DataDir)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	settings := NewSettingsStore(cfg.DataDir)
	devices := NewDeviceStore(cfg.DataDir)
	w := &web{settings: settings, devices: devices, admin: cfg.AdminToken}

	queue := make(chan string, 64)
	go worker(queue, store, settings)

	for _, j := range store.Pending() {
		queue <- j.ID
	}

	mux := http.NewServeMux()

	// Web UI
	mux.HandleFunc("/", w.home)
	mux.HandleFunc("/login", func(rw http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.loginSubmit(rw, r)
			return
		}
		w.loginPage(rw, r)
	})
	mux.HandleFunc("/setup", func(rw http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.setupSubmit(rw, r)
			return
		}
		w.setupPage(rw, r)
	})
	mux.HandleFunc("/admin", w.requireAdmin(w.adminPage))
	mux.HandleFunc("POST /admin/pair", w.requireAdmin(w.mintCode))
	mux.HandleFunc("POST /admin/revoke", w.requireAdmin(w.revoke))

	// Device API
	mux.HandleFunc("POST /v1/pair", w.pairRedeem)
	mux.HandleFunc("POST /v1/memos", deviceAuth(devices, createHandler(store, queue, cfg)))
	mux.HandleFunc("GET /v1/memos/{id}", deviceAuth(devices, getHandler(store)))
	mux.HandleFunc("GET /healthz", func(rw http.ResponseWriter, _ *http.Request) {
		_, _ = rw.Write([]byte("ok"))
	})

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           withLogging(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}

	s := settings.Get()
	log.Printf("listening on %s (configured=%v asr=%s model=%s)", cfg.Addr, s.Configured, s.ASR.Base, s.ASR.Model)
	log.Fatal(srv.ListenAndServe())
}

func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(rw, r)
		if strings.HasPrefix(r.URL.Path, "/v1/") || strings.HasPrefix(r.URL.Path, "/admin") {
			log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
		}
	})
}

// deviceAuth resolves the bearer token to a live device. Revoked devices fail
// on their next request, which is the whole point of per-device tokens.
func deviceAuth(devices *DeviceStore, next http.HandlerFunc) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if _, ok := devices.Authenticate(token); !ok {
			http.Error(rw, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(rw, r)
	}
}

func writeJSON(rw http.ResponseWriter, code int, v any) {
	rw.Header().Set("Content-Type", "application/json; charset=utf-8")
	rw.WriteHeader(code)
	_ = json.NewEncoder(rw).Encode(v)
}

// POST /v1/memos — accepts the audio, answers 202 immediately.
func createHandler(store *Store, queue chan<- string, cfg Config) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(rw, r.Body, 32<<20)
		if err := r.ParseMultipartForm(8 << 20); err != nil {
			http.Error(rw, "bad multipart", http.StatusBadRequest)
			return
		}

		clientID := r.FormValue("clientId")
		if clientID == "" {
			clientID = newID()
		}

		// Idempotency: the same clientId always maps to the same job, so a
		// retry after a flaky upload can never produce two notes.
		if j, ok := store.ByClientID(clientID); ok {
			writeJSON(rw, http.StatusAccepted, map[string]any{"id": j.ID, "status": j.Status})
			return
		}

		file, header, err := r.FormFile("audio")
		if err != nil {
			http.Error(rw, "missing audio", http.StatusBadRequest)
			return
		}
		defer file.Close()

		id := newID()
		path := filepath.Join(cfg.DataDir, "audio", id+extensionOf(header.Filename))

		dst, err := os.Create(path)
		if err != nil {
			http.Error(rw, "storage", http.StatusInternalServerError)
			return
		}
		if _, err := dst.ReadFrom(file); err != nil {
			dst.Close()
			_ = os.Remove(path)
			http.Error(rw, "storage", http.StatusInternalServerError)
			return
		}
		dst.Close()

		job := &Job{
			ID:        id,
			ClientID:  clientID,
			Status:    StatusQueued,
			Created:   time.Now().UTC(),
			AudioPath: path,
		}
		store.Put(job)
		queue <- id

		writeJSON(rw, http.StatusAccepted, map[string]any{"id": id, "status": job.Status})
	}
}

func extensionOf(name string) string {
	if i := strings.LastIndexByte(name, '.'); i >= 0 && len(name)-i <= 6 {
		return name[i:]
	}
	return ".wav"
}

// GET /v1/memos/{id} — what the watch polls while it says "正在转写".
func getHandler(store *Store) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		j, ok := store.Get(r.PathValue("id"))
		if !ok {
			http.Error(rw, "not found", http.StatusNotFound)
			return
		}
		writeJSON(rw, http.StatusOK, j)
	}
}

func worker(queue <-chan string, store *Store, settings *SettingsStore) {
	for id := range queue {
		process(id, store, settings)
	}
}

func process(id string, store *Store, settings *SettingsStore) {
	j, ok := store.Get(id)
	if !ok || j.Status == StatusDone {
		return
	}

	fail := func(msg string) {
		store.Update(id, func(j *Job) {
			j.Status = StatusFailed
			j.Error = msg
		})
		log.Printf("job %s failed: %s", id, msg)
	}

	s := settings.Get()
	if !s.Configured {
		fail("relay is not configured yet")
		return
	}

	store.Update(id, func(j *Job) { j.Status = StatusTranscribing })
	started := time.Now()

	// Guard 1: near-silence never reaches the model. A mis-tap in a pocket
	// costs nothing instead of ten seconds of ASR and a junk note.
	if rms, err := rmsOfWAV(j.AudioPath); err == nil && rms < s.ASR.MinRMS {
		fail(fmt.Sprintf("silence (rms=%.0f < %.0f)", rms, s.ASR.MinRMS))
		return
	}

	asr := NewASR(s.ASR.Base, s.ASR.Model, s.ASR.KeepAlive, strings.Join(s.ASR.Langs, ","))
	lang, text, err := asr.Transcribe(j.AudioPath)
	if err != nil {
		fail(err.Error())
		return
	}
	text = strings.TrimSpace(text)
	if text == "" {
		// Keep the audio: an empty transcript is exactly the case where the
		// user needs to listen again.
		fail("empty transcript")
		return
	}

	// Guard 2: the model answers silence with a fluent sentence in some
	// arbitrary language. Language tags we never speak mean there was no speech.
	//
	// Qwen3-ASR tags Cantonese as "Cantonese", not "Chinese" — an allowlist
	// that only says Chinese silently drops every Cantonese memo.
	if !asr.languageAllowed(lang) {
		fail("unexpected language: " + lang + " (allowed: " + strings.Join(asr.langs, ",") + ")")
		return
	}
	log.Printf("job %s detected language: %s", id, lang)

	store.Update(id, func(j *Job) {
		j.Text = text
		j.Language = lang
	})
	store.Update(id, func(j *Job) { j.Status = StatusWriting })

	blinko := NewBlinko(s.Blinko.Base, s.Blinko.Token)
	noteID, err := blinko.Upsert(text, map[string]any{
		"source":    "wearmemo",
		"clientId":  j.ClientID,
		"asrModel":  s.ASR.Model,
		"language":  lang,
		"latencyMs": time.Since(started).Milliseconds(),
	})
	if err != nil {
		fail(err.Error())
		return
	}

	store.Update(id, func(j *Job) {
		j.Status = StatusDone
		j.NoteID = noteID
	})

	// Done means the audio has served its purpose; only failures keep it.
	_ = os.Remove(j.AudioPath)
	log.Printf("job %s done in %s (note %d)", id, time.Since(started).Round(time.Millisecond), noteID)
}
