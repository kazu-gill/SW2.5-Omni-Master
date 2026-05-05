package config

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// Config holds all runtime configuration loaded from config.env.
type Config struct {
	DBPath      string
	Addr        string
	ComfyURL    string
	GMURL       string
	SupportURL  string
	NPCAURL     string
	NPCBURL     string
	NPCCURL     string
	EmbedURL    string
	PersonasDir string
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

	return &Config{
		DBPath:      get("DB_PATH", "data/omni.db"),
		Addr:        get("ADDR", ":8080"),
		ComfyURL:    get("COMFY_URL", ""),
		GMURL:       get("GM_URL", "http://localhost:11430"),
		SupportURL:  get("SUPPORT_URL", "http://localhost:11431"),
		NPCAURL:     get("NPC_A_URL", "http://localhost:11432"),
		NPCBURL:     get("NPC_B_URL", "http://localhost:11433"),
		NPCCURL:     get("NPC_C_URL", "http://localhost:11434"),
		EmbedURL:    get("EMBED_URL", "http://localhost:11435"),
		PersonasDir: get("PERSONAS_DIR", "data/personas"),
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
