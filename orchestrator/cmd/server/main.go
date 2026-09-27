package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/kf/sw25-omni-master/pkg/api"
	"github.com/kf/sw25-omni-master/pkg/config"
	"github.com/kf/sw25-omni-master/pkg/db"
	"github.com/kf/sw25-omni-master/pkg/game"
	"github.com/kf/sw25-omni-master/pkg/llm"
	"github.com/kf/sw25-omni-master/pkg/rag"
)

// configPath resolves config.env relative to the running binary.
// The binary lives at .bin/orchestrator, so ../config.env is the project root.
func resolveConfigPath() string {
	exe, err := os.Executable()
	if err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "..", "config.env")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	// Fallback: config.env in the current working directory
	return "config.env"
}

func main() {
	cfg, err := config.Load(resolveConfigPath())
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	database, err := db.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("db.Open: %v", err)
	}
	defer database.Close()

	if err := db.SeedQuests(database); err != nil {
		log.Printf("warn: seed quests: %v", err)
	}

	checkMlxServe(cfg.LLMURL, cfg.LLMModelID)
	checkMlxServe(cfg.EmbedURL, cfg.EmbedModelID)

	chat := llm.New(cfg.LLMURL, cfg.LLMModelID, cfg.LLMTimeout)
	ragEngine := rag.New(database, llm.New(cfg.EmbedURL, cfg.EmbedModelID, cfg.LLMTimeout))
	comfy := game.NewComfyUIClient(cfg.ComfyURL) // nil if COMFY_URL is empty
	engine := game.NewEngine(database, chat, ragEngine, cfg.StageRetryLimit)

	handler := api.New(database, engine, ragEngine, comfy, cfg.PersonasDir)
	mux := http.NewServeMux()
	handler.Routes(mux)

	comfyStatus := "disabled"
	if comfy != nil {
		comfyStatus = cfg.ComfyURL
	}
	log.Printf("Omni-Master listening on %s  (ComfyUI: %s)", cfg.Addr, comfyStatus)
	log.Fatal(http.ListenAndServe(cfg.Addr, mux))
}

// checkMlxServe warns when an mlx-serve profile is not answering. The server
// still starts: Omni-Master never launches models itself, and the profile may
// be started later.
func checkMlxServe(baseURL, modelID string) {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(baseURL + "/health")
	if err == nil {
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			log.Printf("mlx-serve %s: ready (%s)", modelID, baseURL)
			return
		}
		err = fmt.Errorf("status %d", resp.StatusCode)
	}
	log.Printf("warn: mlx-serve %s is not ready at %s (%v)", modelID, baseURL, err)
	log.Printf("      start it: pwsh /Users/kf/AI/MLX/scripts/Start-MlxServe.ps1 -Profile %s", modelID)
}
