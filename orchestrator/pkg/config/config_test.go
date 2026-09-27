package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeEnv(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.env")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.env"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.LLMURL != "http://127.0.0.1:11441" || cfg.LLMModelID != "gemma4-31b-it-heretic-4bit-mtp" {
		t.Errorf("LLM = %s / %s", cfg.LLMURL, cfg.LLMModelID)
	}
	if cfg.EmbedURL != "http://127.0.0.1:11455" || cfg.EmbedModelID != "bge-m3-q8" {
		t.Errorf("Embed = %s / %s", cfg.EmbedURL, cfg.EmbedModelID)
	}
	if cfg.LLMTimeout != 300*time.Second || cfg.StageRetryLimit != 2 {
		t.Errorf("timeout=%v retry=%d", cfg.LLMTimeout, cfg.StageRetryLimit)
	}
}

func TestLoadFromFile(t *testing.T) {
	p := writeEnv(t, "# comment\nLLM_URL=http://host:1\nLLM_MODEL_ID=m\nLLM_TIMEOUT_SEC=60\nSTAGE_RETRY_LIMIT=0\n")
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.LLMURL != "http://host:1" || cfg.LLMModelID != "m" || cfg.LLMTimeout != time.Minute || cfg.StageRetryLimit != 0 {
		t.Errorf("cfg = %+v", cfg)
	}
}

func TestLoadRejectsBadNumber(t *testing.T) {
	if _, err := Load(writeEnv(t, "LLM_TIMEOUT_SEC=abc\n")); err == nil {
		t.Error("expected error for non-numeric LLM_TIMEOUT_SEC")
	}
	if _, err := Load(writeEnv(t, "STAGE_RETRY_LIMIT=-1\n")); err == nil {
		t.Error("expected error for negative STAGE_RETRY_LIMIT")
	}
}
