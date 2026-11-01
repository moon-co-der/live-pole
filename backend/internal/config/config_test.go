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
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "JWT_SECRET") {
		t.Fatalf("Load error = %v, want error naming JWT_SECRET", err)
	}
}
