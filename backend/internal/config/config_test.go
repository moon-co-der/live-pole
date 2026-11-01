package config

import (
	"strings"
	"testing"
)

func TestLoadNamesMissingEnvironmentVariable(t *testing.T) {
	values := map[string]string{
		"PORT": "5000", "MONGO_URI": "mongodb" + "://mongo.example.invalid:27017/live_polling",
		"REDIS_URL": "redis" + "://redis.example.invalid:6379/0", "JWT_SECRET": strings.Repeat("x", 32),
		"CLIENT_URL": "https://app.example.invalid", "COOKIE_SECURE": "false", "TRUST_PROXY_HEADERS": "false",
	}
	for name := range values {
		t.Setenv(name, values[name])
	}
	for _, name := range []string{"PORT", "MONGO_URI", "REDIS_URL", "JWT_SECRET", "CLIENT_URL"} {
		t.Setenv(name, "")
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("Load with missing %s error=%v", name, err)
		}
		t.Setenv(name, values[name])
	}
	t.Setenv("JWT_SECRET", strings.Repeat("x", 31))
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "JWT_SECRET must contain at least 32 characters") {
		t.Errorf("Load with short JWT_SECRET error=%v", err)
	}
}

func TestTrustProxyHeadersConfiguration(t *testing.T) {
	for key, value := range map[string]string{
		"PORT": "5000", "MONGO_URI": "mongodb" + "://mongo.example.invalid:27017/live_polling",
		"REDIS_URL": "redis" + "://redis.example.invalid:6379/0", "JWT_SECRET": strings.Repeat("x", 32),
		"CLIENT_URL": "https://app.example.invalid", "COOKIE_SECURE": "false",
	} {
		t.Setenv(key, value)
	}
	t.Setenv("TRUST_PROXY_HEADERS", "")
	cfg, err := Load()
	if err != nil || cfg.TrustProxyHeaders {
		t.Fatalf("default TrustProxyHeaders=%t, error=%v", cfg.TrustProxyHeaders, err)
	}
	t.Setenv("TRUST_PROXY_HEADERS", "true")
	cfg, err = Load()
	if err != nil || !cfg.TrustProxyHeaders {
		t.Fatalf("true TrustProxyHeaders=%t, error=%v", cfg.TrustProxyHeaders, err)
	}
	t.Setenv("TRUST_PROXY_HEADERS", "yes")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "TRUST_PROXY_HEADERS") {
		t.Fatalf("invalid TRUST_PROXY_HEADERS error=%v", err)
	}
}
