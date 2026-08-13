package config

import (
	"os"
	"testing"
	"time"
)

func TestLoadAndValidateDefault(t *testing.T) {
	os.Unsetenv("EXECUTION_MODE")
	os.Unsetenv("DATABASE_URL")
	os.Unsetenv("DEFAULT_TTL_SECONDS")
	os.Unsetenv("JANITOR_INTERVAL")

	cfg, err := LoadAndValidate()
	if err != nil {
		t.Fatalf("LoadAndValidate failed: %v", err)
	}

	if cfg.ExecutionMode != "local" {
		t.Errorf("expected default ExecutionMode 'local', got '%s'", cfg.ExecutionMode)
	}
	if cfg.DefaultTTL != 7*24*time.Hour {
		t.Errorf("expected default DefaultTTL 7 days, got %v", cfg.DefaultTTL)
	}
	if cfg.JanitorInterval != 1*time.Hour {
		t.Errorf("expected default JanitorInterval 1h, got %v", cfg.JanitorInterval)
	}
}

func TestLoadAndValidateInvalidExecutionMode(t *testing.T) {
	os.Setenv("EXECUTION_MODE", "invalid_mode")
	defer os.Unsetenv("EXECUTION_MODE")

	_, err := LoadAndValidate()
	if err == nil {
		t.Error("expected error for invalid EXECUTION_MODE, got nil")
	}
}

func TestLoadAndValidateInvalidDatabaseURL(t *testing.T) {
	os.Setenv("DATABASE_URL", "http://invalid-db-scheme")
	defer os.Unsetenv("DATABASE_URL")

	_, err := LoadAndValidate()
	if err == nil {
		t.Error("expected error for invalid DATABASE_URL scheme, got nil")
	}
}
