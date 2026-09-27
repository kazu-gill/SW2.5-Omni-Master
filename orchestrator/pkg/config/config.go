package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all runtime configuration loaded from config.env.
type Config struct {
	DBPath      string
	Addr        string
	ComfyURL    string
	PersonasDir string

	// All turn stages (GM / NPC / Support) share one chat model on mlx-serve.
	LLMURL     string
	LLMModelID string
	LLMTimeout time.Duration
	// StageRetryLimit is how many times a stage is re-thought after its
	// output fails validation.
	StageRetryLimit int

	EmbedURL     string
	EmbedModelID string
}

// Load reads config.env at envPath, then overrides with any OS environment
// variables that are already set. Missing keys fall back to defaults.
func Load(envPath string) (*Config, error) {
	vals, err := parseEnvFile(envPath)
	if err != nil {
		return nil, fmt.Errorf("config.Load: %w", err)
	}

	get := func(key, fallback string) string {
		// OS env takes highest priority, then file, then fallback
		if v := os.Getenv(key); v != "" {
			return v
		}
		if v, ok := vals[key]; ok && v != "" {
			return v
		}
		return fallback
	}

	getInt := func(key string, fallback int) (int, error) {
		v := get(key, strconv.Itoa(fallback))
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return 0, fmt.Errorf("config.Load: %s=%q は 0 以上の整数で指定してください", key, v)
		}
		return n, nil
	}
	timeoutSec, err := getInt("LLM_TIMEOUT_SEC", 300)
	if err != nil {
		return nil, err
	}
	retryLimit, err := getInt("STAGE_RETRY_LIMIT", 2)
	if err != nil {
		return nil, err
	}

	return &Config{
		DBPath:          get("DB_PATH", "data/omni.db"),
		Addr:            get("ADDR", ":8080"),
		ComfyURL:        get("COMFY_URL", ""),
		PersonasDir:     get("PERSONAS_DIR", "data/personas"),
		LLMURL:          get("LLM_URL", "http://127.0.0.1:11441"),
		LLMModelID:      get("LLM_MODEL_ID", "gemma4-31b-it-heretic-4bit-mtp"),
		LLMTimeout:      time.Duration(timeoutSec) * time.Second,
		StageRetryLimit: retryLimit,
		EmbedURL:        get("EMBED_URL", "http://127.0.0.1:11455"),
		EmbedModelID:    get("EMBED_MODEL_ID", "bge-m3-q8"),
	}, nil
}

// parseEnvFile reads KEY=VALUE lines from path, ignoring comments and blanks.
func parseEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil // config.env がなくてもデフォルト値で動く
		}
		return nil, err
	}
	defer f.Close()

	vals := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		vals[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return vals, scanner.Err()
}
