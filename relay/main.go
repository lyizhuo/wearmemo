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
	Addr        string
	DeviceToken string
	BlinkoBase  string
	BlinkoToken string
	ASRBase     string
	ASRModel    string
	ASRKeep     string
	ASRLangs    string
	MinRMS      float64
	DataDir     string
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envFloat(key string, def float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

func loadConfig() Config {
	return Config{
		Addr:        env("RELAY_ADDR", ":8080"),
		DeviceToken: env("RELAY_DEVICE_TOKEN", "dev-token-change-me"),
		BlinkoBase:  env("BLINKO_BASE", "http://127.0.0.1:1111"),
		BlinkoToken: env("BLINKO_TOKEN", ""),
		ASRBase:     env("ASR_BASE", "http://127.0.0.1:11434"),
		ASRModel:    env("ASR_MODEL", "frozenlab/qwen3-asr:1.7b"),
		ASRKeep:     env("ASR_KEEP_ALIVE", "30m"),
		ASRLangs:    env("ASR_LANGS", "Chinese,English"),
		MinRMS:      envFloat("ASR_MIN_RMS", 200),
		DataDir:     env("RELAY_DATA", "./data"),
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
	if cfg.BlinkoToken == "" {
		log.Println("warning: BLINKO_TOKEN is empty, writes will be rejected")
	}

	store, err := NewStore(cfg.DataDir)
	if err != nil {
		log.Fatalf("store: %v", err)
	}

	asr := NewASR(cfg.ASRBase, cfg.ASRModel, cfg.ASRKeep, cfg.ASRLangs)
	blinko := NewBlinko(cfg.BlinkoBase, cfg.BlinkoToken)

	// One worker on purpose: the ASR host is shared and CPU-bound, and a
	// backlog of 3 memos is far better than 3 memos that each take 3x longer.
	queue := make(chan string, 64)
	go worker(queue, store, asr, blinko, cfg.MinRMS)

	for _, j := range store.Pending() {
		queue <- j.ID
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/memos", auth(cfg, createHandler(store, queue, cfg)))
	mux.HandleFunc("GET /v1/memos/{id}", auth(cfg, getHandler(store)))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           withLogging(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("listening on %s (asr=%s model=%s blinko=%s)", cfg.Addr, cfg.ASRBase, cfg.ASRModel, cfg.BlinkoBase)
	log.Fatal(srv.ListenAndServe())
}

func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
	})
}

func auth(cfg Config, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if got == "" || got != cfg.DeviceToken {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// POST /v1/memos — accepts the audio, answers 202 immediately.
func createHandler(store *Store, queue chan<- string, cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 32<<20) // 32 MiB ceiling
		if err := r.ParseMultipartForm(8 << 20); err != nil {
			http.Error(w, "bad multipart", http.StatusBadRequest)
			return
		}

		clientID := r.FormValue("clientId")
		if clientID == "" {
			clientID = newID()
		}

		// Idempotency: the same clientId always maps to the same job, so a
		// retry after a flaky upload can never produce two notes.
		if j, ok := store.ByClientID(clientID); ok {
			writeJSON(w, http.StatusAccepted, map[string]any{"id": j.ID, "status": j.Status})
			return
		}

		file, header, err := r.FormFile("audio")
		if err != nil {
			http.Error(w, "missing audio", http.StatusBadRequest)
			return
		}
		defer file.Close()

		id := newID()
		name := id + extensionOf(header.Filename)
		path := filepath.Join(cfg.DataDir, "audio", name)

		dst, err := os.Create(path)
		if err != nil {
			http.Error(w, "storage", http.StatusInternalServerError)
			return
		}
		if _, err := dst.ReadFrom(file); err != nil {
			dst.Close()
			_ = os.Remove(path)
			http.Error(w, "storage", http.StatusInternalServerError)
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

		writeJSON(w, http.StatusAccepted, map[string]any{"id": id, "status": job.Status})
	}
}

func extensionOf(name string) string {
	if i := strings.LastIndexByte(name, '.'); i >= 0 && len(name)-i <= 6 {
		return name[i:]
	}
	return ".m4a"
}

// GET /v1/memos/{id} — what the watch polls while it says "正在转写".
func getHandler(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		j, ok := store.Get(id)
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, j)
	}
}

func worker(queue <-chan string, store *Store, asr *ASR, blinko *Blinko, minRMS float64) {
	for id := range queue {
		process(id, store, asr, blinko, minRMS)
	}
}

func process(id string, store *Store, asr *ASR, blinko *Blinko, minRMS float64) {
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

	store.Update(id, func(j *Job) { j.Status = StatusTranscribing })
	started := time.Now()

	// Guard 1: near-silence never reaches the model. A mis-tap in a pocket
	// costs nothing instead of ten seconds of ASR and a junk note.
	if rms, err := rmsOfWAV(j.AudioPath); err == nil && rms < minRMS {
		fail(fmt.Sprintf("silence (rms=%.0f < %.0f)", rms, minRMS))
		return
	}

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
	// arbitrary language. A language we never speak means there was no speech.
	if !asr.languageAllowed(lang) {
		fail("unexpected language: " + lang)
		return
	}

	store.Update(id, func(j *Job) { j.Text = text })
	store.Update(id, func(j *Job) { j.Status = StatusWriting })

	noteID, err := blinko.Upsert(text, map[string]any{
		"source":    "wearmemo",
		"clientId":  j.ClientID,
		"asrModel":  asr.model,
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
