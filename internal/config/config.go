// Package config loads service configuration from the environment.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all runtime configuration. All fields have safe defaults except
// APIKeys (required) and ModelPath (required when running with the real engine).
type Config struct {
	ListenAddr   string        // REDACTOR_LISTEN_ADDR (default ":8080")
	APIKeys      []string      // REDACTOR_API_KEYS (comma-separated, required)
	MaxBodyBytes int64         // REDACTOR_MAX_BODY_BYTES (default 1 MiB)
	ReadTimeout  time.Duration // REDACTOR_READ_TIMEOUT (default 15s)
	WriteTimeout time.Duration // REDACTOR_WRITE_TIMEOUT (default 30s)
	IdleTimeout  time.Duration // REDACTOR_IDLE_TIMEOUT (default 60s)

	// Engine
	ModelPath string // REDACTOR_MODEL_PATH (path to the GGUF)
	Device    string // REDACTOR_DEVICE (default "cpu")
	NThreads  int    // REDACTOR_N_THREADS (default 0 = engine default)
	Window    int    // REDACTOR_WINDOW (default 0 = engine default)
	PoolSize  int    // REDACTOR_POOL_SIZE (default 1; each ctx is a full model copy)

	// Redaction defaults
	DefaultThreshold float32 // REDACTOR_DEFAULT_THRESHOLD (default 0.5)
	MaskString       string  // REDACTOR_MASK_STRING (default "[REDACTED]")
	HashSecret       []byte  // REDACTOR_HASH_SECRET (keys ModeHash)
}

// Load reads configuration from the environment, applying defaults.
func Load() (*Config, error) {
	c := &Config{
		ListenAddr:       getStr("REDACTOR_LISTEN_ADDR", ":8080"),
		MaxBodyBytes:     getInt64("REDACTOR_MAX_BODY_BYTES", 1<<20),
		ReadTimeout:      getDur("REDACTOR_READ_TIMEOUT", 15*time.Second),
		WriteTimeout:     getDur("REDACTOR_WRITE_TIMEOUT", 30*time.Second),
		IdleTimeout:      getDur("REDACTOR_IDLE_TIMEOUT", 60*time.Second),
		ModelPath:        getStr("REDACTOR_MODEL_PATH", ""),
		Device:           getStr("REDACTOR_DEVICE", "cpu"),
		NThreads:         int(getInt64("REDACTOR_N_THREADS", 0)),
		Window:           int(getInt64("REDACTOR_WINDOW", 0)),
		PoolSize:         int(getInt64("REDACTOR_POOL_SIZE", 1)),
		DefaultThreshold: float32(getFloat("REDACTOR_DEFAULT_THRESHOLD", 0.5)),
		MaskString:       getStr("REDACTOR_MASK_STRING", "[REDACTED]"),
		HashSecret:       []byte(getStr("REDACTOR_HASH_SECRET", "")),
	}

	for _, k := range strings.Split(getStr("REDACTOR_API_KEYS", ""), ",") {
		if k = strings.TrimSpace(k); k != "" {
			c.APIKeys = append(c.APIKeys, k)
		}
	}
	if len(c.APIKeys) == 0 {
		return nil, fmt.Errorf("REDACTOR_API_KEYS is required (comma-separated keys)")
	}
	if c.PoolSize < 1 {
		c.PoolSize = 1
	}
	return c, nil
}

func getStr(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}

func getInt64(key string, def int64) int64 {
	if v, ok := os.LookupEnv(key); ok {
		if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
			return n
		}
	}
	return def
}

func getFloat(key string, def float64) float64 {
	if v, ok := os.LookupEnv(key); ok {
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			return f
		}
	}
	return def
}

func getDur(key string, def time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok {
		if d, err := time.ParseDuration(strings.TrimSpace(v)); err == nil {
			return d
		}
	}
	return def
}
