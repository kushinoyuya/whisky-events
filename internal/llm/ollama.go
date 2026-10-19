// Package llm is a minimal client for a local Ollama server.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"time"
)

// Generator produces a completion for a prompt. Extraction code depends on
// this interface so tests can swap in a fake instead of a real model.
type Generator interface {
	Generate(ctx context.Context, prompt string) (string, error)
}

// Ollama talks to the /api/generate endpoint of an Ollama server.
type Ollama struct {
	BaseURL string // e.g. http://127.0.0.1:11434
	Model   string // e.g. qwen2.5:3b
	NumCtx  int    // context window; Ollama's default (2048) is too small for pages
	HTTP    *http.Client
}

// NewOllama returns a client with defaults suited to CPU-only inference:
// a long timeout, since a 3B model on a dual-core CPU can take minutes.
func NewOllama(baseURL, model string) *Ollama {
	return &Ollama{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Model:   model,
		NumCtx:  4096,
		HTTP:    &http.Client{Timeout: 10 * time.Minute},
	}
}

type generateRequest struct {
	Model   string                 `json:"model"`
	Prompt  string                 `json:"prompt"`
	Format  string                 `json:"format,omitempty"`
	Stream  bool                   `json:"stream"`
	Options map[string]interface{} `json:"options,omitempty"`
}

type generateResponse struct {
	Response string `json:"response"`
	Error    string `json:"error"`
}

// Generate asks the model for a JSON-formatted answer. Temperature is 0 so
// that extraction results are reproducible across runs.
func (o *Ollama) Generate(ctx context.Context, prompt string) (string, error) {
	body, err := json.Marshal(generateRequest{
		Model:  o.Model,
		Prompt: prompt,
		Format: "json",
		Stream: false,
		Options: map[string]interface{}{
			"temperature": 0,
			"num_ctx":     o.NumCtx,
		},
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.BaseURL+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("ollama request failed (is `ollama serve` running?): %w", err)
	}
	defer resp.Body.Close()

	raw, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var out generateResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("ollama: unexpected response (status %d): %.200s", resp.StatusCode, raw)
	}
	if resp.StatusCode != http.StatusOK || out.Error != "" {
		return "", fmt.Errorf("ollama: status %d: %s", resp.StatusCode, out.Error)
	}
	return out.Response, nil
}
