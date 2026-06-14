package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestDoctor_MissingAPIKey verifies that runDoctor reports a
// clear failure (and returns an error) when the env var is
// unset. Uses t.Setenv to clear the var for the test.
func TestDoctor_MissingAPIKey(t *testing.T) {
	t.Setenv("OPENCODE_ZEN_API_KEY", "")
	t.Setenv("OPENCODE_ZEN_ENDPOINT", "")
	t.Setenv("OPENCODE_ZEN_MODEL", "")

	err := runDoctor("")
	if err == nil {
		t.Fatal("runDoctor with no API key: got nil error, want non-nil")
	}
	if !strings.Contains(err.Error(), "OPENCODE_ZEN_API_KEY") {
		t.Errorf("runDoctor with no API key: error = %q, want it to mention 'OPENCODE_ZEN_API_KEY'", err.Error())
	}
}

// TestDoctor_EndpointUnreachable verifies that runDoctor
// surfaces a network failure (returns non-nil) when the
// configured endpoint can't be reached. Uses an unrouted
// 127.0.0.1 port so the OS rejects the connection quickly.
func TestDoctor_EndpointUnreachable(t *testing.T) {
	t.Setenv("OPENCODE_ZEN_API_KEY", "sk-test")
	t.Setenv("OPENCODE_ZEN_ENDPOINT", "http://127.0.0.1:1")
	t.Setenv("OPENCODE_ZEN_MODEL", "")

	// LoadConfig doesn't read timeout from env, so the default
	// DefaultTimeout (10s) is in effect — still short enough for
	// a 127.0.0.1:1 connection-refused (sub-second).
	err := runDoctor("")
	if err == nil {
		t.Fatal("runDoctor with unrouted endpoint: got nil error, want non-nil")
	}
}

// TestDoctor_HappyPath verifies the full success path: a real
// httptest.NewServer that returns 200 on /v1/models should
// produce a nil error from runDoctor. Confirms the wiring
// (config + client + ping) works end-to-end inside the CLI.
func TestDoctor_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("doctor ping hit %s, want /v1/models", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	t.Setenv("OPENCODE_ZEN_API_KEY", "sk-test-1234567890")
	// srv.URL is the server root; set the endpoint to <root>/v1
	// so the client's appended /models lands on /v1/models.
	t.Setenv("OPENCODE_ZEN_ENDPOINT", srv.URL+"/v1")
	t.Setenv("OPENCODE_ZEN_MODEL", "")

	if err := runDoctor(""); err != nil {
		t.Fatalf("runDoctor with valid endpoint: %v", err)
	}
}

// TestDoctor_Unauthorized verifies that an endpoint returning
// 401 propagates as a non-nil error so the CLI exit code
// reflects the failure.
func TestDoctor_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad key"}`))
	}))
	defer srv.Close()

	t.Setenv("OPENCODE_ZEN_API_KEY", "sk-bad")
	// srv.URL is the server root; set the endpoint to <root>/v1
	// so the client's appended /models lands on /v1/models.
	t.Setenv("OPENCODE_ZEN_ENDPOINT", srv.URL+"/v1")
	t.Setenv("OPENCODE_ZEN_MODEL", "")

	err := runDoctor("")
	if err == nil {
		t.Fatal("runDoctor with 401 endpoint: got nil error, want non-nil")
	}
	if !strings.Contains(err.Error(), "auth") {
		t.Errorf("runDoctor with 401: error = %q, want it to contain 'auth'", err.Error())
	}
}

// TestMaskAPIKey verifies the masking helper: long keys get
// first4...last4, short keys get fully masked. This is a
// pure unit test, no I/O.
func TestMaskAPIKey(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", "***"},
		{"sk-test", "***"}, // exactly 7 chars: fully masked
		{"sk-12345678", "sk-1...5678"},
		{"short", "***"},
	}
	for _, c := range cases {
		got := maskAPIKey(c.in)
		if got != c.want {
			t.Errorf("maskAPIKey(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
