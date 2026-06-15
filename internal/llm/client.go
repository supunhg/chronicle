// Package llm provides a thin HTTP client for the OpenCode Zen
// chat-completions endpoint, plus a config loader.
//
// Scope of Phase 17.1:
//
//   - Client.Ping verifies the configured API key by hitting the
//     /v1/models endpoint. This is the only operation `chronicle
//     doctor` actually needs to confirm reachability + auth.
//   - Client.Chat is a stub for Phase 17.4 (the Narrator LLM
//     integration). It compiles and returns a clear "not
//     implemented" error so callers can wire it without surprises.
//
// Scope of later phases (out of scope here):
//
//   - Intent parsing (Phase 17.2) — lives in a separate package.
//   - Narrator LLM (Phase 17.4) — replaces Chat's stub body.
//   - World AI (Phase 17.5) — different prompt contract, also
//     routed through this client.
//
// The client is intentionally tiny: net/http + a small typed
// wrapper. No third-party SDK, no retries (the spec says "no
// hidden automation"). The LLM is only allowed to narrate, not
// to mutate world state, so the blast radius of a transient
// failure is small — callers can decide whether to retry, fall
// back to a template, or surface the error.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultEndpoint is the OpenCode Zen base URL. It is OpenAI-
// compatible: the same /v1/models and /v1/chat/completions
// paths work for any OpenAI-shaped API.
const DefaultEndpoint = "https://opencode.ai/zen/v1"

// DefaultModel is the default chat model. The Narrator LLM
// (Phase 17.4) will use this unless overridden in config.yaml
// or the OPENCODE_ZEN_MODEL env var. Pinned to a model that
// the typical opencode.ai/zen account has access to; users
// on other accounts can override per the env-var contract.
const DefaultModel = "deepseek-v4-flash-free"

// DefaultTimeout caps any single HTTP request. Ping is fast
// (a small JSON response); Chat may need more time once it is
// implemented, so the per-call context can override this.
const DefaultTimeout = 10 * time.Second

// ChatMessage is one entry in a chat-completions request. Role
// is one of "system", "user", "assistant". Content is plain
// text. The struct is intentionally minimal — the LLM is for
// narration, not tool-calling or vision, so we don't expose
// the multi-content-part shape that the OpenAI API supports.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest is the request body for /v1/chat/completions.
// Mirrors the OpenAI shape so any compatible endpoint accepts
// it without translation.
type ChatRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
}

// ChatResponse is the response from /v1/chat/completions. We
// only decode the fields we actually consume; the API returns
// more (usage, logprobs, etc.) and we ignore them.
type ChatResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Message ChatMessage `json:"message"`
	} `json:"choices"`
}

// Client is the typed wrapper around net/http. The zero value
// is not useful; always construct via NewClient. The HTTPClient
// and Endpoint fields are exported so tests can swap them.
type Client struct {
	// Endpoint is the API base URL, including the /v1 suffix.
	// Default: DefaultEndpoint.
	Endpoint string

	// APIKey is the bearer token sent in the Authorization
	// header. Empty is allowed for Ping against an unauthenticated
	// /v1/models endpoint, but most production endpoints will
	// reject the request with 401.
	APIKey string

	// Model is the default model used by Chat when the caller
	// doesn't override it.
	Model string

	// HTTPClient is the underlying transport. Tests inject an
	// httptest.Server's client; production uses http.DefaultClient
	// (with a per-call timeout via context).
	HTTPClient *http.Client
}

// Option is a functional option for NewClient. Keeps the
// constructor stable as we add more knobs (retries, custom
// transport, structured logging) in later phases.
type Option func(*Client)

// WithEndpoint overrides the API base URL.
func WithEndpoint(endpoint string) Option {
	return func(c *Client) { c.Endpoint = endpoint }
}

// WithAPIKey sets the bearer token.
func WithAPIKey(key string) Option {
	return func(c *Client) { c.APIKey = key }
}

// WithModel overrides the default chat model.
func WithModel(model string) Option {
	return func(c *Client) { c.Model = model }
}

// WithHTTPClient replaces the underlying transport. Useful for
// tests (point at httptest.NewServer) and for callers that need
// a custom timeout, proxy, or TLS config.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.HTTPClient = hc }
}

// WithTimeout is a convenience that wraps WithHTTPClient with a
// client that has the given timeout. It is not additive: if you
// call it after WithHTTPClient, the custom client wins.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) {
		if c.HTTPClient == nil || c.HTTPClient == http.DefaultClient {
			c.HTTPClient = &http.Client{Timeout: d}
		}
	}
}

// NewClient constructs a Client with sensible defaults applied
// before the caller's options. The defaults are:
//
//	Endpoint   = DefaultEndpoint
//	Model      = DefaultModel
//	HTTPClient = http.DefaultClient (or a timeout-wrapped one
//	             if WithTimeout is supplied)
func NewClient(opts ...Option) *Client {
	c := &Client{
		Endpoint:   DefaultEndpoint,
		Model:      DefaultModel,
		HTTPClient: http.DefaultClient,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Ping verifies the endpoint is reachable and the API key is
// accepted. It hits GET <Endpoint>/models and considers the
// call successful if the server returns a 2xx status. A 401
// or 403 is reported as an "auth failed" error so the user
// can distinguish "no network" from "wrong key".
//
// The context controls cancellation and per-call deadlines. If
// the context has no deadline, the client's DefaultTimeout is
// not used; callers that want a default timeout should set
// one on the context (e.g., context.WithTimeout(ctx, 5*time.Second))
// or pass WithTimeout to NewClient.
func (c *Client) Ping(ctx context.Context) error {
	if strings.TrimSpace(c.Endpoint) == "" {
		return fmt.Errorf("llm: endpoint is empty")
	}
	url := strings.TrimRight(c.Endpoint, "/") + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("llm: build request: %w", err)
	}
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("llm: ping %s: %w", url, err)
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		return fmt.Errorf("llm: auth failed (status %d) — check OPENCODE_ZEN_API_KEY", resp.StatusCode)
	default:
		return fmt.Errorf("llm: ping %s: unexpected status %d", url, resp.StatusCode)
	}
}

// Chat is a stub for Phase 17.4. It builds the request body and
// POSTs to /v1/chat/completions, then decodes the first choice
// from the response. This is enough for the Narrator LLM to use
// in the next phase; tests can also drive it against a mock
// server to verify the wire format is correct.
//
// Phase 17.4 will add: rate limiting, prompt caching, a
// structured response type (narrative + suggested followups),
// and a hard cap on max_tokens so a runaway prompt can't
// explode the bill.
func (c *Client) Chat(ctx context.Context, messages []ChatMessage) (string, error) {
	if strings.TrimSpace(c.Endpoint) == "" {
		return "", fmt.Errorf("llm: endpoint is empty")
	}
	if len(messages) == 0 {
		return "", fmt.Errorf("llm: chat requires at least one message")
	}
	url := strings.TrimRight(c.Endpoint, "/") + "/chat/completions"
	body := ChatRequest{Model: c.Model, Messages: messages}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("llm: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("llm: build request: %w", err)
	}
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("llm: chat %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Read a bounded amount of the body for diagnostics.
		const maxErrBody = 4 << 10 // 4 KiB
		buf := make([]byte, maxErrBody)
		n, _ := io.ReadFull(resp.Body, buf)
		body := strings.TrimSpace(string(buf[:n]))
		// When the endpoint rejects the configured model
		// (e.g. opencode.ai/zen returns 401 "Model X is not
		// supported"), surface a hint about the env var so the
		// user knows the fix is a one-line shell change. The
		// shape of the hint matches what `chronicle doctor`
		// prints on a missing/wrong API key.
		if strings.Contains(body, "Model") && strings.Contains(body, "not supported") {
			return "", fmt.Errorf("llm: chat %s: status %d: %s (hint: set %s to a model your account supports)",
				url, resp.StatusCode, body, EnvModel)
		}
		return "", fmt.Errorf("llm: chat %s: status %d: %s", url, resp.StatusCode, body)
	}

	var decoded ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return "", fmt.Errorf("llm: decode response: %w", err)
	}
	if len(decoded.Choices) == 0 {
		return "", fmt.Errorf("llm: response had no choices")
	}
	return decoded.Choices[0].Message.Content, nil
}
