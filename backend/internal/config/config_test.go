package config

import (
	"strings"
	"testing"
)

func TestLoadNamesMissingEnvironmentVariable(t *testing.T) {
	t.Setenv("PORT", "5000")
	t.Setenv("MONGO_URI", "mongodb://mongo.example.invalid:27017")
	t.Setenv("REDIS_URL", "redis://redis.example.invalid:6379/0")
	t.Setenv("JWT_SECRET", "")
	t.Setenv("CLIENT_URL", "https://app.example.invalid")
	t.Setenv("COOKIE_SECURE", "false")
	t.Setenv("TRUST_PROXY_HEADERS", "false")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "JWT_SECRET") {
		t.Fatalf("Load error = %v, want error naming JWT_SECRET", err)
	}
}

func TestTrustProxyHeadersConfiguration(t *testing.T) {
	for key, value := range map[string]string{
		"PORT": "5000", "MONGO_URI": "mongodb://mongo.example.invalid:27017/live_polling",
		"REDIS_URL": "redis://redis.example.invalid:6379/0", "JWT_SECRET": strings.Repeat("x", 32),
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
