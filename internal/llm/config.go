package llm

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// EnvAPIKey is the environment variable that overrides the API
// key from config.yaml. The spec calls this out by name
// (§3.2: "OPENCODE_ZEN_API_KEY is the only required env var").
const EnvAPIKey = "OPENCODE_ZEN_API_KEY"

// EnvEndpoint is the environment variable that overrides the
// endpoint from config.yaml. Useful for pointing the client at
// a local mock or a regional mirror.
const EnvEndpoint = "OPENCODE_ZEN_ENDPOINT"

// EnvModel is the environment variable that overrides the
// default model from config.yaml.
const EnvModel = "OPENCODE_ZEN_MODEL"

// Config is the resolved LLM client configuration. It is the
// output of LoadConfig and the input to NewClient. All four
// fields are required to be set; defaults are applied inside
// LoadConfig (not here) so callers that build a Config by
// hand see exactly what they passed.
type Config struct {
	// Endpoint is the API base URL, including the /v1 suffix.
	Endpoint string

	// APIKey is the bearer token. May be empty if the endpoint
	// is unauthenticated; in production, it should always be set.
	APIKey string

	// Model is the default chat model used when the caller
	// doesn't override it on a per-request basis.
	Model string

	// Timeout caps any single HTTP request. The client converts
	// this into an http.Client timeout during construction.
	Timeout time.Duration
}

// configFile is the on-disk shape of config.yaml. It mirrors
// Config but uses a string for Timeout (parsed via
// time.ParseDuration) so the YAML is human-friendly.
type configFile struct {
	Endpoint string `yaml:"endpoint"`
	APIKey   string `yaml:"api_key"`
	Model    string `yaml:"model"`
	Timeout  string `yaml:"timeout"`
}

// LoadConfig resolves the LLM client configuration from three
// sources, in priority order:
//
//  1. Environment variables (OPENCODE_ZEN_API_KEY,
//     OPENCODE_ZEN_ENDPOINT, OPENCODE_ZEN_MODEL) — the highest
//     priority. Per spec §3.2, env wins so operators can
//     override per-deploy without touching the worldpack.
//  2. A YAML file at configPath (typically <worldpack>/llm.yaml
//     or ~/.config/chronicle/llm.yaml). Loaded only if the file
//     exists; missing file is not an error.
//  3. Built-in defaults (DefaultEndpoint, DefaultModel,
//     DefaultTimeout). APIKey has no default — if none of the
//     above set it, the resulting Config will have APIKey="".
//
// The returned Config is always non-nil. If the YAML file
// exists but is malformed, LoadConfig returns a wrapped parse
// error so the user can fix the file rather than silently
// falling back to defaults (which would mask a typo).
func LoadConfig(configPath string) (*Config, error) {
	cfg := defaultConfig()

	// Step 2: YAML file, if present.
	if configPath != "" {
		fileCfg, err := loadConfigFile(configPath)
		switch {
		case err == nil:
			applyFileConfig(cfg, fileCfg)
		case errors.Is(err, os.ErrNotExist):
			// Missing file is fine — the user might be running
			// with env-var-only config.
		default:
			return nil, fmt.Errorf("llm: load %s: %w", configPath, err)
		}
	}

	// Step 1: env vars win. Applied last so they override
	// anything from the file.
	applyEnvOverrides(cfg)

	return cfg, nil
}

// defaultConfig returns a Config populated with the package
// defaults. Kept as a small helper so tests and the public
// LoadConfig share the same starting point.
func defaultConfig() *Config {
	return &Config{
		Endpoint: DefaultEndpoint,
		Model:    DefaultModel,
		Timeout:  DefaultTimeout,
	}
}

// loadConfigFile reads and parses a single YAML file. Exported
// as LoadConfigFromFile so tests can exercise the file parser
// directly without going through the env-var merge.
func LoadConfigFromFile(path string) (*Config, error) {
	cfg := defaultConfig()
	fileCfg, err := loadConfigFile(path)
	if err != nil {
		return nil, err
	}
	applyFileConfig(cfg, fileCfg)
	return cfg, nil
}

// LoadConfigFromEnv returns a Config populated from environment
// variables and defaults (no YAML lookup). Exported so tests can
// drive the env-var path with t.Setenv.
func LoadConfigFromEnv() *Config {
	cfg := defaultConfig()
	applyEnvOverrides(cfg)
	return cfg
}

func loadConfigFile(path string) (*configFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f configFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse %s: %w", filepath.Base(path), err)
	}
	return &f, nil
}

func applyFileConfig(dst *Config, f *configFile) {
	if f.Endpoint != "" {
		dst.Endpoint = f.Endpoint
	}
	if f.APIKey != "" {
		dst.APIKey = f.APIKey
	}
	if f.Model != "" {
		dst.Model = f.Model
	}
	if f.Timeout != "" {
		if d, err := time.ParseDuration(f.Timeout); err == nil && d > 0 {
			dst.Timeout = d
		}
		// If the user wrote a bad duration string, silently keep
		// the default. The YAML file is human-edited; we don't
		// want a typo to crash the engine.
	}
}

func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv(EnvAPIKey); v != "" {
		cfg.APIKey = v
	}
	if v := os.Getenv(EnvEndpoint); v != "" {
		cfg.Endpoint = v
	}
	if v := os.Getenv(EnvModel); v != "" {
		cfg.Model = v
	}
}
