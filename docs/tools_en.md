# SW2.5 Omni-Master — Tool Usage Manual (English)

> Version: 1.0 / Language: English

This document describes the shell scripts and Python utilities bundled with the project.

---

## Table of Contents

1. [setup.sh — Initial Setup and Reset](#1-setupsh)
2. [start.sh — Start All Services](#2-startsh)
3. [stop.sh — Stop All Services](#3-stopsh)
4. [ocr_rulebook.py — Rulebook Import](#4-ocr_rulebookpy)
5. [embed_chunks.py — Embedding Vector Generation](#5-embed_chunkspy)
6. [optimize_chunks.py — Chunk Optimization](#6-optimize_chunkspy)
7. [generate_scenarios.py — Scenario Batch Generation](#7-generate_scenariospy)

---

## 1. setup.sh

Used for first-time setup and between-session data resets.

### Synopsis

```
./setup.sh [options]
```

| Option | Description |
|--------|-------------|
| (none) | Standard reset — clears game data while preserving rulebook data |
| `--full` | Full wipe, including `rag_chunks` (rulebook) |
| `-h`, `--help` | Show help |

### Behavior

**Standard reset (no arguments)**

Cleared:
- `sessions` (cascades to `npc_sheets`, `session_logs`, `checkpoints`)
- `quests`

Preserved:
- `rag_chunks` (rulebook data)
- `rag_chunks_opt` (optimized chunks)
- `player_characters` (PC sheets)
- `scenarios`, `scenario_images` (pre-generated scenarios)
- `data/personas/*.yaml` (persona definitions)

After the reset, session #1 is automatically recreated (the app assumes `SESSION_ID=1`).

**Full reset (--full)**

Drops all tables including `rag_chunks`. Rulebook data must be re-imported. Use this for a clean first-time install or to rebuild from scratch.

### Schema Application

If the DB file does not exist, `orchestrator/pkg/db/schema.sql` is applied to create it. For an existing DB, idempotent `CREATE TABLE IF NOT EXISTS` statements are re-applied. The following migrations are also attempted (errors are silently ignored if columns already exist):

```sql
ALTER TABLE rag_chunks ADD COLUMN tag TEXT NOT NULL DEFAULT '';
ALTER TABLE rag_chunks ADD COLUMN enabled INTEGER NOT NULL DEFAULT 1;
ALTER TABLE rag_chunks ADD COLUMN overrides_id INTEGER REFERENCES rag_chunks(id);
```

### Configuration

Reads `DB_PATH` from `config.env`. Defaults to `data/omni.db` if not set.

### Examples

```bash
# Standard pre-session reset
./setup.sh

# Full wipe and rebuild
./setup.sh --full

# After a full reset, re-import the rulebook
./setup.sh --full
uv run ai-agents/ocr_rulebook.py data/rulebook/swordworld2.5_rulebook1.md --db data/omni.db
uv run ai-agents/embed_chunks.py --db data/omni.db --embed-url http://localhost:11435
```

---

## 2. start.sh

Starts all services in the background and tails logs in real time.

### Synopsis

```
./start.sh
```

No arguments. All configuration is read from `config.env`.

### Startup Sequence

1. Launch `llama-server` ×6 sequentially:
   - `gm` (port 11430, uses `MODEL_GM`)
   - `support` (port 11431, uses `MODEL_NPC`)
   - `npc-a` (port 11432, uses `MODEL_NPC`)
   - `npc-b` (port 11433, uses `MODEL_NPC`)
   - `npc-c` (port 11434, uses `MODEL_NPC`)
   - `embed` (port 11435, uses `MODEL_EMBED`)
2. Wait 10 seconds for LLM initialization
3. Build and launch the Go orchestrator (port 8080)
4. Launch the Vite UI dev server (port 5173)
5. Tail all logs

### Prerequisites

- `config.env` must exist
- `.pids` must not exist (run `stop.sh` first if it does)
- DB is auto-initialized if missing

### Key config.env Settings

```bash
# LLM model file paths
MODEL_GM=/path/to/gm_model.gguf
MODEL_NPC=/path/to/npc_model.gguf
MODEL_EMBED=/path/to/embed_model.gguf

# Full path to llama-server (not needed if on PATH)
LLAMA_SERVER=/usr/local/bin/llama-server

# Orchestrator listen address
ADDR=:8080

# Database path
DB_PATH=/Users/kf/Work/sw25-omni-master/data/omni.db

# ComfyUI URL (image generation — leave empty to disable)
COMFY_URL=http://192.168.1.10:8188

# LLM endpoints (read by the orchestrator)
MODEL_GM_URL=http://localhost:11430
MODEL_SUPPORT_URL=http://localhost:11431
```

### When a Model Is Not Found

Servers with missing model files are skipped. API calls to those roles will fail. If `llama-server` is not on `PATH`, all LLM servers are skipped (useful for debugging the UI without models).

### Log Files

| File | Service |
|------|---------|
| `logs/orchestrator.log` | Go orchestrator |
| `logs/ui.log` | Vite dev server |
| `logs/gm.log` | GM LLM |
| `logs/support.log` | Support LLM |
| `logs/npc-a.log` | NPC-A LLM |
| `logs/npc-b.log` | NPC-B LLM |
| `logs/npc-c.log` | NPC-C LLM |
| `logs/embed.log` | Embed LLM |

### PID Tracking

Process PIDs are written to `.pids`. `stop.sh` uses both this file and live port lookups to ensure all processes are stopped.

---

## 3. stop.sh

Stops all services.

### Synopsis

```
./stop.sh
```

No arguments.

### Behavior

For each managed port, uses `lsof -ti:<port>` to find the listening PID, sends SIGTERM, waits 1 second, then sends SIGKILL if the process is still alive.

Managed ports: 5173, 8080, 11430, 11431, 11432, 11433, 11434, 11435

Also checks any remaining PIDs in `.pids` and terminates them. Deletes `.pids` when done.

### Notes

If any server becomes unresponsive, run `stop.sh` and then `start.sh` to restart cleanly.

---

## 4. ocr_rulebook.py

Imports rulebook text (PDF / Markdown / plain text) into the `rag_chunks` table.

### Synopsis

```
uv run ai-agents/ocr_rulebook.py <file> [options]
```

| Option | Default | Description |
|--------|---------|-------------|
| `<file>` | (required) | File to import (.pdf / .md / .txt) |
| `--source-type` | `rulebook` | `rulebook` / `correction` / `houserule` |
| `--db` | `data/omni.db` | Database file path |

### Source Types and Priority

Higher-priority chunks are preferred during RAG retrieval.

| Source Type | Priority | Use Case |
|-------------|----------|----------|
| `houserule` | 20 | Custom rules (highest priority) |
| `correction` | 10 | Official errata / corrections |
| `rulebook` | 0 | Standard rulebook content |

### Supported Formats

**PDF (.pdf)**

Text is extracted using `pdfplumber`. Works for text-embedded PDFs; scanned image PDFs are not supported.

```bash
uv run ai-agents/ocr_rulebook.py data/rulebook/rulebook.pdf --db data/omni.db
```

**Markdown (.md)**

Sections are split on `# / ## / ###` headings to preserve semantic groupings.

```bash
uv run ai-agents/ocr_rulebook.py data/rulebook/swordworld2.5_rulebook1.md \
    --db data/omni.db \
    --source-type rulebook
```

**Plain Text (.txt)**

Split into 500-character chunks.

### Chunk Size

Default: 500 characters per chunk. Markdown is split at heading boundaries to keep sections intact.

### Verify After Import

```bash
sqlite3 data/omni.db \
  "SELECT source_type, count(*) FROM rag_chunks GROUP BY source_type;"
```

Freshly imported chunks have `embedding IS NULL`. Run `embed_chunks.py` to generate embeddings for RAG search.

### Examples

```bash
# Import Markdown rulebook
uv run ai-agents/ocr_rulebook.py \
    data/rulebook/swordworld2.5_rulebook1.md \
    --db data/omni.db

# Add house rules from a text file
uv run ai-agents/ocr_rulebook.py \
    data/houserules.txt \
    --source-type houserule \
    --db data/omni.db

# Import PDF supplement
uv run ai-agents/ocr_rulebook.py \
    data/rulebook/supplement.pdf \
    --db data/omni.db \
    --source-type rulebook
```

---

## 5. embed_chunks.py

Generates embedding vectors for chunks in `rag_chunks` that have no embedding yet.

### Synopsis

```
uv run ai-agents/embed_chunks.py [options]
```

| Option | Default | Description |
|--------|---------|-------------|
| `--db` | `data/omni.db` | Database file path |
| `--embed-url` | `http://localhost:11435` | Embedding LLM URL |
| `--batch` | `32` | Number of chunks to process per run |
| `--status` | — | Show processing statistics and exit |

### Behavior

1. Fetches up to `--batch` rows where `embedding IS NULL`
2. POSTs each chunk's text to `{embed_url}/embedding`
3. Stores the returned float32 vector as a BLOB in the `embedding` column

The embed server (port 11435) must be running. Start the system with `start.sh`, or launch `llama-server` separately with the embed model.

### Check Status

```bash
uv run ai-agents/embed_chunks.py --status
```

Sample output:
```
[embed] Status:
  Done:    1234
  Pending:   56
  Total:   1290
```

### Processing All Chunks

Run repeatedly with a large batch size, or use a loop:

```bash
while uv run ai-agents/embed_chunks.py \
    --db data/omni.db \
    --embed-url http://localhost:11435 \
    --batch 50 | grep -q "処理します"; do
  sleep 1
done
echo "All chunks embedded"
```

---

## 6. optimize_chunks.py

Uses a local LLM to rewrite `rag_chunks`, fixing OCR errors, stripping furigana, adding classification tags, and generating one-line summaries. Results go into `rag_chunks_opt` and are preferred over raw chunks in RAG retrieval.

### Synopsis

```
uv run ai-agents/optimize_chunks.py [options]
```

| Option | Default | Description |
|--------|---------|-------------|
| `--limit N` | `20` | Max chunks to process per run |
| `--model-url URL` | `http://localhost:11430` | Text optimization LLM URL |
| `--embed-url URL` | `http://localhost:11435` | Embedding LLM URL |
| `--db PATH` | `data/omni.db` | Database file path |
| `--dry-run` | — | Print results without writing to DB |
| `--retry-failed` | — | Re-process chunks with `failed` status |
| `--status` | — | Show processing statistics and exit |
| `--chunk-id N` | — | Process only the specified chunk (debug) |

### Processing Pipeline

```
rag_chunks (raw text)
    ↓  LLM call (OPTIMIZE_PROMPT)
    ↓  Parse JSON response
rag_chunks_opt (optimized text + tag + summary)
    ↓  Re-embed
rag_chunks_opt.embedding
```

### Classification Tags

The LLM selects one of eight tags per chunk:

| Tag | Content |
|-----|---------|
| `combat` | Combat rules |
| `magic` | Spells and magic |
| `character` | Character creation and attributes |
| `item` | Weapons, armor, items |
| `world` | World-building and lore |
| `general` | General rules |
| `status` | Status effects, buffs, debuffs |
| `rule` | Other rule content |

### Check Status

```bash
uv run ai-agents/optimize_chunks.py --status
```

Sample output:
```
Processing status:
  Done (done):      892
  Failed (failed):   12
  Pending:          386
  Total:           1290
```

### Examples

```bash
# Check status
uv run ai-agents/optimize_chunks.py --status

# Run 50-chunk batch (best during off-hours)
uv run ai-agents/optimize_chunks.py \
    --limit 50 \
    --model-url http://localhost:11430 \
    --embed-url http://localhost:11435

# Retry failed chunks
uv run ai-agents/optimize_chunks.py --retry-failed --limit 20

# Dry-run for a single chunk (debug)
uv run ai-agents/optimize_chunks.py --chunk-id 42 --dry-run

# Use a remote LLM server
uv run ai-agents/optimize_chunks.py \
    --model-url http://192.168.1.10:11430 \
    --limit 100
```

### Notes

- Both the GM LLM (default port 11430) and Embed LLM (11435) must be running.
- Running during an active game session may impact LLM response times. Prefer off-hours execution.
- Typical processing speed: 3–10 seconds per chunk, depending on model speed.

---

## 7. generate_scenarios.py

Batch-generates scenario text and ComfyUI images, saving everything to the database.

### Synopsis

```
uv run ai-agents/generate_scenarios.py [options]
```

| Option | Default | Description |
|--------|---------|-------------|
| `--count N` | `1` | Number of scenarios to generate |
| `--rank RANK` | `C` | Difficulty rank (A/B/C/D/E) |
| `--model-url URL` | `http://localhost:11430` | Text generation LLM URL |
| `--comfy-url URL` | (none) | ComfyUI URL — omit to skip image generation |
| `--comfy-model NAME` | (auto) | Checkpoint model name |
| `--db PATH` | `data/omni.db` | Database file path |
| `--images-dir PATH` | `data/images` | Directory for saved images |
| `--no-images` | — | Skip image generation |
| `--publish-quest` | — | Register generated scenarios in the quest board |
| `--backgrounds` | — | Generate shared background images only (no new scenarios) |
| `--scenario-id N` | — | Regenerate images for an existing scenario |
| `--dry-run` | — | Do not write to DB or disk |
| `--status` | — | List generated scenarios and exit |

### Rank → Level Range

| Rank | Adventurer Level |
|------|-----------------|
| E | 1–2 |
| D | 2–3 |
| C | 3–5 |
| B | 5–7 |
| A | 7–10 |

### Generated Content per Scenario

| Category | Content | Image Size |
|----------|---------|------------|
| `scene` | Location background | 768×512 (landscape) |
| `portrait` | Enemy / NPC portrait | 512×768 (portrait) |

Text fields:
- Title, rank, summary, description
- Client, reward, objective
- Locations (name, description, image prompt)
- Enemies (name, description, stats, image prompt)
- Plot hooks and events

### Shared Background Images

Generates 8 reusable scene types that can be shared across multiple scenarios:

| Label | Scene |
|-------|-------|
| `tavern_interior` | Inn interior |
| `guild_hall` | Adventurers guild hall |
| `forest_path` | Enchanted forest path |
| `dungeon_entrance` | Dungeon entrance |
| `village_square` | Village square |
| `mountain_pass` | Mountain pass |
| `ancient_ruins` | Ancient ruins |
| `castle_courtyard` | Castle courtyard |

Saved to: `data/images/backgrounds/`  
In the DB, these have `scenario_images.scenario_id = NULL`.

### ComfyUI Image Generation Pipeline

1. Auto-detect available models via `/object_info/CheckpointLoaderSimple`
2. Select model (or use `--comfy-model` to specify)
3. Build a 7-node txt2img workflow:
   - CheckpointLoaderSimple → CLIPTextEncode ×2 → EmptyLatentImage → KSampler → VAEDecode → SaveImage
   - KSampler settings: 25 steps, Euler Ancestral sampler, Karras scheduler, CFG 7.5
4. Queue via `/prompt` → get `prompt_id`
5. Poll `/history/{prompt_id}` every 2 seconds (up to 120 seconds)
6. Download completed image via `/view?filename=...&type=output`

### Database Schema

```
scenarios table:
  id, title, rank, summary, description, client, reward,
  target, level, locations_json, enemies_json, plot_hooks_json,
  events_json, status, generated_at

scenario_images table:
  id, scenario_id (*NULL = shared background), category, label,
  file_path, width, height, generated_at
```

### Examples

```bash
# Basic: generate 1 rank-C scenario (text only)
uv run ai-agents/generate_scenarios.py

# Generate 3 scenarios and register in quest board
uv run ai-agents/generate_scenarios.py \
    --count 3 \
    --rank C \
    --no-images \
    --publish-quest

# Generate 2 rank-B scenarios with images
uv run ai-agents/generate_scenarios.py \
    --count 2 \
    --rank B \
    --comfy-url http://192.168.1.10:8188 \
    --publish-quest

# Specify model manually
uv run ai-agents/generate_scenarios.py \
    --comfy-url http://192.168.1.10:8188 \
    --comfy-model "dreamshaper_8.safetensors" \
    --count 1

# Pre-generate shared backgrounds
uv run ai-agents/generate_scenarios.py \
    --backgrounds \
    --comfy-url http://192.168.1.10:8188

# Add images to an existing scenario (ID=3)
uv run ai-agents/generate_scenarios.py \
    --scenario-id 3 \
    --comfy-url http://192.168.1.10:8188

# Check generation status
uv run ai-agents/generate_scenarios.py --status

# Dry run (no writes)
uv run ai-agents/generate_scenarios.py \
    --count 1 \
    --dry-run
```

### Scheduled Execution with cron

```bash
# Add to crontab -e
# Generate 2 rank-C scenarios at 2am every night
0 2 * * * cd /Users/kf/Work/sw25-omni-master && \
    uv run ai-agents/generate_scenarios.py \
    --count 2 --rank C --no-images --publish-quest \
    >> logs/generate_scenarios.log 2>&1
```

### Notes

- The GM LLM (default port 11430) must be running for text generation.
- For image generation, ensure the Windows-side ComfyUI is running and reachable.
- Use `--no-images` to run without ComfyUI.
- Scenarios registered with `--publish-quest` appear immediately in the UI QUEST BOARD.

---

## Dependency Management

```bash
# Verify installed packages
cd ai-agents
uv pip list

# Install uv if not available
pip install uv

# Install all declared dependencies
uv sync
```

Dependencies are declared in `ai-agents/pyproject.toml` and managed automatically by `uv run`.

---

## config.env Template

```bash
# Model paths
MODEL_GM=/path/to/models/gemma-4b-q4.gguf
MODEL_NPC=/path/to/models/gemma-4b-q4.gguf
MODEL_EMBED=/path/to/models/nomic-embed.gguf

# llama-server path (omit if it's on PATH)
# LLAMA_SERVER=/usr/local/bin/llama-server

# API server
ADDR=:8080
DB_PATH=/Users/kf/Work/sw25-omni-master/data/omni.db

# LLM endpoints (read by the orchestrator)
MODEL_GM_URL=http://localhost:11430
MODEL_SUPPORT_URL=http://localhost:11431
MODEL_NPC_A_URL=http://localhost:11432
MODEL_NPC_B_URL=http://localhost:11433
MODEL_NPC_C_URL=http://localhost:11434
MODEL_EMBED_URL=http://localhost:11435

# ComfyUI (leave empty to disable image generation)
COMFY_URL=http://192.168.1.10:8188
```
