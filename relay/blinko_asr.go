package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ASR calls a vLLM-served Qwen3-ASR through the OpenAI transcription API, so
// swapping 1.7B for 0.6B is a model-name change and nothing else.
type ASR struct {
	base  string
	model string
	http  *http.Client
}

func NewASR(base, model string) *ASR {
	return &ASR{
		base:  strings.TrimRight(base, "/"),
		model: model,
		// Generous: a 1.7B model on an ARM CPU host is slow, and a timeout here
		// silently turns into a lost memo.
		http: &http.Client{Timeout: 10 * time.Minute},
	}
}

func (a *ASR) Transcribe(audioPath string) (string, error) {
	f, err := os.Open(audioPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var body bytes.Buffer
	w := multipart.NewWriter(&body)

	_ = w.WriteField("model", a.model)
	_ = w.WriteField("response_format", "json")

	part, err := w.CreateFormFile("file", filepath.Base(audioPath))
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(part, f); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", a.base+"/v1/audio/transcriptions", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := a.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("asr %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}

	var out struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("asr decode: %w (%s)", err, truncate(string(raw), 200))
	}
	return strings.TrimSpace(out.Text), nil
}

// Blinko writes the note. Auth is a long-lived JWT (apiToken) that only ever
// lives here — never on the watch.
type Blinko struct {
	base  string
	token string
	http  *http.Client
}

func NewBlinko(base, token string) *Blinko {
	return &Blinko{
		base:  strings.TrimRight(base, "/"),
		token: token,
		http:  &http.Client{Timeout: 60 * time.Second},
	}
}

type noteUpsert struct {
	Content  string         `json:"content"`
	Type     int            `json:"type"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

func (b *Blinko) Upsert(content string, meta map[string]any) (int64, error) {
	payload, _ := json.Marshal(noteUpsert{
		Content:  content,
		Type:     1, // NoteType.NOTE
		Metadata: meta,
	})

	req, err := http.NewRequest("POST", b.base+"/api/v1/note/upsert", bytes.NewReader(payload))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+b.token)

	resp, err := b.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return 0, fmt.Errorf("blinko %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}

	// tRPC-to-openapi wraps the procedure result in { "result": { "data": ... } }.
	var wrapped struct {
		Result struct {
			Data struct {
				ID int64 `json:"id"`
			} `json:"data"`
		} `json:"result"`
	}
	if json.Unmarshal(raw, &wrapped) == nil && wrapped.Result.Data.ID != 0 {
		return wrapped.Result.Data.ID, nil
	}

	var flat struct {
		ID int64 `json:"id"`
	}
	if json.Unmarshal(raw, &flat) == nil {
		return flat.ID, nil
	}
	return 0, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
