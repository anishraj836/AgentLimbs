package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ExecutionMode     string
	DatabaseURL       string
	KafkaBrokers      []string
	AgentAPIKey       string
	DefaultTTL        time.Duration
	JanitorInterval   time.Duration
	RateLimitRequests float64
	RateLimitBurst    int
}

// LoadAndValidate parses environment variables and enforces strict startup validation.
func LoadAndValidate() (*Config, error) {
	cfg := &Config{
		ExecutionMode:     os.Getenv("EXECUTION_MODE"),
		DatabaseURL:       os.Getenv("DATABASE_URL"),
		AgentAPIKey:       os.Getenv("AGENT_API_KEY"),
		RateLimitRequests: 50.0,
		RateLimitBurst:    100,
	}

	if cfg.ExecutionMode == "" {
		cfg.ExecutionMode = "local"
	}
	switch strings.ToLower(cfg.ExecutionMode) {
	case "local", "distributed", "embedded":
		// valid
	default:
		return nil, fmt.Errorf("invalid EXECUTION_MODE '%s': must be one of ['local', 'distributed', 'embedded']", cfg.ExecutionMode)
	}

	if cfg.DatabaseURL != "" {
		parsed, err := url.Parse(cfg.DatabaseURL)
		if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
			return nil, fmt.Errorf("invalid DATABASE_URL '%s': must be a valid postgres connection string (postgres://user:pass@host:port/dbname)", cfg.DatabaseURL)
		}
	}

	if kb := os.Getenv("KAFKA_BROKERS"); kb != "" {
		brokers := strings.Split(kb, ",")
		for i, b := range brokers {
			brokers[i] = strings.TrimSpace(b)
		}
		cfg.KafkaBrokers = brokers
	}

	ttlStr := os.Getenv("DEFAULT_TTL_SECONDS")
	if ttlStr == "" {
		cfg.DefaultTTL = 7 * 24 * time.Hour // 604800s default
	} else {
		secs, err := strconv.Atoi(ttlStr)
		if err != nil || secs <= 0 {
			return nil, fmt.Errorf("invalid DEFAULT_TTL_SECONDS '%s': must be a positive integer", ttlStr)
		}
		cfg.DefaultTTL = time.Duration(secs) * time.Second
	}

	janitorStr := os.Getenv("JANITOR_INTERVAL")
	if janitorStr == "" {
		cfg.JanitorInterval = 1 * time.Hour
	} else {
		dur, err := time.ParseDuration(janitorStr)
		if err != nil {
			secs, errInt := strconv.Atoi(janitorStr)
			if errInt != nil || secs <= 0 {
				return nil, fmt.Errorf("invalid JANITOR_INTERVAL '%s': must be a valid duration (e.g. '1h') or positive seconds", janitorStr)
			}
			dur = time.Duration(secs) * time.Second
		}
		cfg.JanitorInterval = dur
	}

	if rpsStr := os.Getenv("RATE_LIMIT_RPS"); rpsStr != "" {
		rps, err := strconv.ParseFloat(rpsStr, 64)
		if err != nil || rps <= 0 {
			return nil, fmt.Errorf("invalid RATE_LIMIT_RPS '%s': must be a positive number", rpsStr)
		}
		cfg.RateLimitRequests = rps
	}

	if burstStr := os.Getenv("RATE_LIMIT_BURST"); burstStr != "" {
		burst, err := strconv.Atoi(burstStr)
		if err != nil || burst <= 0 {
			return nil, fmt.Errorf("invalid RATE_LIMIT_BURST '%s': must be a positive integer", burstStr)
		}
		cfg.RateLimitBurst = burst
	}

	return cfg, nil
}
