package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port                  string
	MongoURI              string
	RedisURL              string
	JWTSecret             string
	ClientURL             string
	CookieSecure          bool
	TrustProxyHeaders     bool
	WSPingInterval        time.Duration
	WSPongWait            time.Duration
	WSWriteWait           time.Duration
	WSMaxMessageBytes     int64
	WSMaxConnectionsPerIP int
}

func Load() (Config, error) {
	var cfg Config
	var err error
	values := []struct {
		name string
		dest *string
	}{
		{"PORT", &cfg.Port},
		{"MONGO_URI", &cfg.MongoURI},
		{"REDIS_URL", &cfg.RedisURL},
		{"JWT_SECRET", &cfg.JWTSecret},
		{"CLIENT_URL", &cfg.ClientURL},
	}
	for _, value := range values {
		*value.dest = strings.TrimSpace(os.Getenv(value.name))
		if *value.dest == "" {
			return Config{}, fmt.Errorf("required environment variable %s is missing", value.name)
		}
	}
	if len(cfg.JWTSecret) < 32 {
		return Config{}, fmt.Errorf("JWT_SECRET must contain at least 32 characters")
	}
	cookieSecure := strings.TrimSpace(os.Getenv("COOKIE_SECURE"))
	switch strings.ToLower(cookieSecure) {
	case "", "false":
		cfg.CookieSecure = false
	case "true":
		cfg.CookieSecure = true
	default:
		return Config{}, fmt.Errorf("COOKIE_SECURE must be true or false")
	}
	trustProxyHeaders := strings.TrimSpace(os.Getenv("TRUST_PROXY_HEADERS"))
	switch strings.ToLower(trustProxyHeaders) {
	case "", "false":
		cfg.TrustProxyHeaders = false
	case "true":
		cfg.TrustProxyHeaders = true
	default:
		return Config{}, fmt.Errorf("TRUST_PROXY_HEADERS must be true or false")
	}
	cfg.WSPingInterval, err = durationEnv("WS_PING_INTERVAL", 30*time.Second)
	if err != nil {
		return Config{}, err
	}
	cfg.WSPongWait, err = durationEnv("WS_PONG_WAIT", 60*time.Second)
	if err != nil {
		return Config{}, err
	}
	cfg.WSWriteWait, err = durationEnv("WS_WRITE_WAIT", 10*time.Second)
	if err != nil {
		return Config{}, err
	}
	cfg.WSMaxMessageBytes, err = int64Env("WS_MAX_MESSAGE_BYTES", 1024)
	if err != nil {
		return Config{}, err
	}
	cfg.WSMaxConnectionsPerIP, err = intEnv("WS_MAX_CONNECTIONS_PER_IP", 20)
	if err != nil {
		return Config{}, err
	}
	if cfg.WSPingInterval <= 0 || cfg.WSPongWait <= cfg.WSPingInterval || cfg.WSWriteWait <= 0 || cfg.WSMaxMessageBytes < 1 || cfg.WSMaxConnectionsPerIP < 1 {
		return Config{}, fmt.Errorf("WebSocket settings must be positive and WS_PONG_WAIT must exceed WS_PING_INTERVAL")
	}
	return cfg, nil
}

func durationEnv(name string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be a duration such as 30s: %w", name, err)
	}
	return parsed, nil
}

func intEnv(name string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", name)
	}
	return parsed, nil
}

func int64Env(name string, fallback int64) (int64, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", name)
	}
	return parsed, nil
}
