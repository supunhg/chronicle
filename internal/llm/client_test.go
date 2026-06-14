package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestClient_Ping_Success verifies that Ping returns nil when the
// server returns 200 and the client attaches the bearer token
// to the Authorization header.
func TestClient_Ping_Success(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("ping hit %s, want /v1/models", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	// srv.URL is the server root (no /v1 prefix); the client
	// appends /models, so we set the endpoint to <root>/v1 to
	// land on /v1/models.
	c := NewClient(WithEndpoint(srv.URL+"/v1"), WithAPIKey("sk-test"))
	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if gotAuth != "Bearer sk-test" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer sk-test")
	}
}

// TestClient_Ping_Unauthorized verifies that Ping returns a
// clear "auth failed" error on 401 so the user can distinguish
// "no network" from "wrong key" without reading the status code.
func TestClient_Ping_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid api key"}`))
	}))
	defer srv.Close()

	c := NewClient(WithEndpoint(srv.URL), WithAPIKey("sk-bad"))
	err := c.Ping(context.Background())
	if err == nil {
		t.Fatal("Ping on 401: got nil error, want non-nil")
	}
	if !strings.Contains(err.Error(), "auth failed") {
		t.Errorf("Ping on 401: error = %q, want it to contain 'auth failed'", err.Error())
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("Ping on 401: error = %q, want it to contain '401'", err.Error())
	}
}

// TestClient_Ping_NetworkError verifies that Ping returns a
// wrapped error (not a panic, not a nil) when the endpoint is
// unreachable. Uses an unrouted 127.0.0.1 port so the OS
// rejects the connection immediately.
func TestClient_Ping_NetworkError(t *testing.T) {
	c := NewClient(
		WithEndpoint("http://127.0.0.1:1"), // port 1: nothing listens
		WithAPIKey("sk-test"),
		WithTimeout(500*time.Millisecond),
	)
	err := c.Ping(context.Background())
	if err == nil {
		t.Fatal("Ping against unrouted URL: got nil error, want non-nil")
	}
	if !strings.Contains(err.Error(), "ping") {
		t.Errorf("Ping network error: error = %q, want it to contain 'ping'", err.Error())
	}
}

// TestClient_Ping_Timeout verifies that Ping respects context
// cancellation. Uses a server that sleeps until the request
// context is cancelled, so the handler exits as soon as the
// client gives up (no goroutine leak).
func TestClient_Ping_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block on the request context: the moment the client
		// cancels (e.g., deadline exceeded), we return. This
		// keeps the handler's lifetime bounded by the test.
		<-r.Context().Done()
	}))
	defer srv.Close()

	c := NewClient(WithEndpoint(srv.URL), WithAPIKey("sk-test"))
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := c.Ping(ctx)
	if err == nil {
		t.Fatal("Ping with cancelled context: got nil error, want non-nil")
	}
	// Either "context deadline exceeded" or a wrapped "ping" error
	// is acceptable — the test only asserts that the call gave up.
	if !strings.Contains(err.Error(), "ping") && !strings.Contains(err.Error(), "context") {
		t.Errorf("Ping timeout: error = %q, want it to mention 'ping' or 'context'", err.Error())
	}
}

// TestClient_Chat_Success verifies the wire format of Chat:
// POST to /v1/chat/completions, JSON body with the right shape,
// bearer auth, and that the first choice's content is returned.
func TestClient_Chat_Success(t *testing.T) {
	var gotMethod, gotPath, gotAuth, gotContentType string
	var gotBody ChatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		if err := decodeJSON(r.Body, &gotBody); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"id": "chatcmpl-1",
			"model": "gpt-4o-mini",
			"choices": [
				{"message": {"role": "assistant", "content": "hello world"}}
			]
		}`))
	}))
	defer srv.Close()

	// srv.URL is the server root (no /v1 prefix); the client
	// appends /chat/completions, so we set the endpoint to
	// <root>/v1 to land on /v1/chat/completions.
	c := NewClient(WithEndpoint(srv.URL+"/v1"), WithAPIKey("sk-test"), WithModel("gpt-4o-mini"))
	out, err := c.Chat(context.Background(), []ChatMessage{
		{Role: "system", Content: "be terse"},
		{Role: "user", Content: "say hi"},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if out != "hello world" {
		t.Errorf("Chat content = %q, want %q", out, "hello world")
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/v1/chat/completions" {
		t.Errorf("path = %q, want /v1/chat/completions", gotPath)
	}
	if gotAuth != "Bearer sk-test" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer sk-test")
	}
	if !strings.HasPrefix(gotContentType, "application/json") {
		t.Errorf("Content-Type = %q, want application/json prefix", gotContentType)
	}
	if gotBody.Model != "gpt-4o-mini" {
		t.Errorf("request model = %q, want gpt-4o-mini", gotBody.Model)
	}
	if len(gotBody.Messages) != 2 {
		t.Errorf("request messages = %d, want 2", len(gotBody.Messages))
	}
}

// TestConfig_Load_PriorityChain verifies the env > yaml >
// default priority order. Writes a yaml file, sets an env var
// for a different field, and asserts the env var wins for its
// field while the yaml value wins for the others.
func TestConfig_Load_PriorityChain(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/llm.yaml"
	yaml := `endpoint: http://yaml-host/v1
api_key: yaml-key
model: yaml-model
timeout: 7s
`
	if err := writeFile(path, []byte(yaml)); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	t.Setenv(EnvAPIKey, "env-key")
	t.Setenv(EnvEndpoint, "")
	t.Setenv(EnvModel, "")

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	// env var wins for APIKey.
	if cfg.APIKey != "env-key" {
		t.Errorf("APIKey = %q, want env-key (env > yaml)", cfg.APIKey)
	}
	// yaml wins for fields with no env override.
	if cfg.Endpoint != "http://yaml-host/v1" {
		t.Errorf("Endpoint = %q, want http://yaml-host/v1 (yaml)", cfg.Endpoint)
	}
	if cfg.Model != "yaml-model" {
		t.Errorf("Model = %q, want yaml-model (yaml)", cfg.Model)
	}
	if cfg.Timeout != 7*time.Second {
		t.Errorf("Timeout = %v, want 7s (yaml)", cfg.Timeout)
	}
}

// TestConfig_Load_DefaultsWhenNothingSet verifies the
// pure-defaults path: no env vars, no yaml file. Used to
// confirm the package compiles and the default constants
// are sane.
func TestConfig_Load_DefaultsWhenNothingSet(t *testing.T) {
	t.Setenv(EnvAPIKey, "")
	t.Setenv(EnvEndpoint, "")
	t.Setenv(EnvModel, "")

	cfg, err := LoadConfig("") // no file
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Endpoint != DefaultEndpoint {
		t.Errorf("Endpoint = %q, want %q", cfg.Endpoint, DefaultEndpoint)
	}
	if cfg.Model != DefaultModel {
		t.Errorf("Model = %q, want %q", cfg.Model, DefaultModel)
	}
	if cfg.Timeout != DefaultTimeout {
		t.Errorf("Timeout = %v, want %v", cfg.Timeout, DefaultTimeout)
	}
	// APIKey has no default; should be empty.
	if cfg.APIKey != "" {
		t.Errorf("APIKey = %q, want empty", cfg.APIKey)
	}
}
