package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"

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

	pool := game.LLMPool{
		GM:      llm.New(cfg.GMURL),
		NPCs:    [3]*llm.Client{llm.New(cfg.NPCAURL), llm.New(cfg.NPCBURL), llm.New(cfg.NPCCURL)},
		Support: llm.New(cfg.SupportURL),
	}
	ragEngine := rag.New(database, llm.New(cfg.EmbedURL))
	comfy := game.NewComfyUIClient(cfg.ComfyURL) // nil if COMFY_URL is empty
	engine := game.NewEngine(database, pool, ragEngine)

	handler := api.New(database, engine, comfy, cfg.PersonasDir)
	mux := http.NewServeMux()
	handler.Routes(mux)

	comfyStatus := "disabled"
	if comfy != nil {
		comfyStatus = cfg.ComfyURL
	}
	log.Printf("Omni-Master listening on %s  (ComfyUI: %s)", cfg.Addr, comfyStatus)
	log.Fatal(http.ListenAndServe(cfg.Addr, mux))
}
