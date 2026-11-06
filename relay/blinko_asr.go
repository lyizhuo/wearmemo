package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// ASR talks to Ollama, which serves frozenlab/qwen3-asr as a multimodal chat
// model: the audio goes in the `images` field, base64, with an empty prompt.
//
// Measured on the target host (Q8_0, 1.7B): 6.4 s audio -> 4.1 s warm,
// 24 s audio -> 18.6 s. Cold start costs ~9 s, hence keep_alive.
type ASR struct {
	base      string
	model     string
	keepAlive string
	langs     []string
	http      *http.Client
}

func NewASR(base, model, keepAlive, langs string) *ASR {
	var allow []string
	for _, l := range strings.Split(langs, ",") {
		if l = strings.TrimSpace(l); l != "" {
			allow = append(allow, l)
		}
	}
	return &ASR{
		base:      strings.TrimRight(base, "/"),
		model:     model,
		keepAlive: keepAlive,
		langs:     allow,
		http:      &http.Client{Timeout: 10 * time.Minute},
	}
}

type ollamaChatRequest struct {
	Model     string           `json:"model"`
	Stream    bool             `json:"stream"`
	KeepAlive string           `json:"keep_alive,omitempty"`
	Messages  []ollamaMessage  `json:"messages"`
}

type ollamaMessage struct {
	Role    string   `json:"role"`
	Content string   `json:"content"`
	Images  []string `json:"images,omitempty"`
}

type ollamaChatResponse struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
	Done    bool  `json:"done"`
	TotalNs int64 `json:"total_duration"`
	LoadNs  int64 `json:"load_duration"`
}

func (a *ASR) Transcribe(audioPath string) (lang, text string, err error) {
	raw, err := os.ReadFile(audioPath)
	if err != nil {
		return "", "", err
	}

	payload, err := json.Marshal(ollamaChatRequest{
		Model:     a.model,
		Stream:    false,
		KeepAlive: a.keepAlive,
		Messages: []ollamaMessage{
			{Role: "user", Content: "", Images: []string{base64.StdEncoding.EncodeToString(raw)}},
		},
	})
	if err != nil {
		return "", "", err
	}

	req, err := http.NewRequest("POST", a.base+"/api/chat", bytes.NewReader(payload))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.http.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode/100 != 2 {
		return "", "", fmt.Errorf("asr %d: %s", resp.StatusCode, truncate(string(body), 300))
	}

	var out ollamaChatResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return "", "", fmt.Errorf("asr decode: %w (%s)", err, truncate(string(body), 200))
	}

	lang, text = parseASROutput(out.Message.Content)
	if text == "" {
		return lang, "", fmt.Errorf("asr returned empty text from %q", truncate(out.Message.Content, 120))
	}
	return lang, text, nil
}

// parseASROutput unwraps the model's own framing:
//
//	language Chinese<asr_text>明天下午三点…
//
// The language tag matters: on silence the model does not return nothing, it
// returns a fluent sentence in whatever language it settles on, so an
// unexpected language is a strong signal that there was no speech at all.
func parseASROutput(s string) (lang, text string) {
	rest := s
	if i := strings.Index(s, "<asr_text>"); i >= 0 {
		head := strings.TrimSpace(s[:i])
		head = strings.TrimSpace(strings.TrimPrefix(head, "language"))
		lang = head
		rest = s[i+len("<asr_text>"):]
	}
	if i := strings.Index(rest, "</asr_text>"); i >= 0 {
		rest = rest[:i]
	}
	return lang, strings.TrimSpace(rest)
}

// languageAllowed reports whether a detected language is in the allowlist.
// The model names languages in English ("Chinese", "English", …).
func (a *ASR) languageAllowed(lang string) bool {
	if lang == "" || len(a.langs) == 0 {
		return true
	}
	for _, want := range a.langs {
		if strings.EqualFold(strings.TrimSpace(want), lang) {
			return true
		}
	}
	return false
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

	// Accept both a bare note object and a {result:{data:{...}}} envelope.
	var flat struct {
		ID int64 `json:"id"`
	}
	if json.Unmarshal(raw, &flat) == nil && flat.ID != 0 {
		return flat.ID, nil
	}
	var wrapped struct {
		Result struct {
			Data struct {
				ID int64 `json:"id"`
			} `json:"data"`
		} `json:"result"`
	}
	if json.Unmarshal(raw, &wrapped) == nil {
		return wrapped.Result.Data.ID, nil
	}
	return 0, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
