# SW2.5 Omni-Master — Tool Usage Manual (English)

> Version: 1.0 / Language: English

This document describes the shell / PowerShell scripts and Python utilities bundled with the project.

The LLMs (chat and embeddings) are served by mlx-serve (`/Users/kf/AI/MLX`), a separate inference platform. The scripts in this project do not launch any LLM process.

---

## Table of Contents

1. [setup.sh — Initial Setup and Reset](#1-setupsh)
2. [Start-Omni.ps1 — Start Services](#2-start-omnips1)
3. [Stop-Omni.ps1 — Stop Services](#3-stop-omnips1)
4. [build_rulebook.py — Rulebook (Re)build](#4-build_rulebookpy)
5. [ocr_rulebook.py — House Rule / Correction Text Import](#5-ocr_rulebookpy)
6. [embed_chunks.py — Embedding Vector Generation](#6-embed_chunkspy)
7. [optimize_chunks.py — Chunk Optimization (Optional, Legacy)](#7-optimize_chunkspy)
8. [generate_scenarios.py — Scenario Batch Generation](#8-generate_scenariospy)

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

The `source` / `section` / `page_from` / `page_to` columns of `rag_chunks` are added automatically when the orchestrator starts (`db.Open`) and when `build_rulebook.py` / `embed_chunks.py` run.

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
uv run ai-agents/build_rulebook.py --apply
uv run ai-agents/embed_chunks.py --db data/omni.db --embed-url http://127.0.0.1:11455
```

---

## 2. Start-Omni.ps1

Starts the Go orchestrator and the Vite UI in the background (PowerShell 7). It does not start mlx-serve.

### Synopsis

```
pwsh scripts/Start-Omni.ps1 [-Follow]
```

| Option | Description |
|--------|-------------|
| `-Follow` | Tail the logs after startup (Ctrl+C stops only the tail; services keep running) |

All other settings are read from `config.env` (OS environment variable > config.env > default).

### Startup Sequence

1. Exit if `.pids` exists (prevents a double launch; run `Stop-Omni.ps1` first)
2. Check `/health` of `LLM_URL` and `EMBED_URL`. If a profile is not answering, print the command to start it and exit, e.g.:
   ```
   pwsh /Users/kf/AI/MLX/scripts/Start-MlxServe.ps1 -Profile gemma4-31b-it-heretic-4bit-mtp
   pwsh /Users/kf/AI/MLX/scripts/Start-MlxServe.ps1 -Profile bge-m3-q8
   ```
3. Build the Go orchestrator (`.bin/orchestrator`) and start it (`ADDR`, default 8080)
4. Start the Vite UI dev server (port 5173)
5. Wait up to 30 seconds for both ports to listen, then print the endpoints

### Prerequisites

- PowerShell 7 (`pwsh`)
- `config.env` must exist (copy it from `config.env.example`)
- The mlx-serve profiles named by `LLM_MODEL_ID` / `EMBED_MODEL_ID` must be running
- `.pids` must not exist

### Key config.env Values

```bash
# Orchestrator listen address
ADDR=:8080

# Database path
DB_PATH=/Users/kf/AI/SW2.5-Omni-Master/data/omni.db

# ComfyUI URL (leave empty to disable image generation)
COMFY_URL=http://192.168.1.10:8188

# Chat model (shared by GM / NPC / Support, on mlx-serve)
LLM_URL=http://127.0.0.1:11441
LLM_MODEL_ID=gemma4-31b-it-heretic-4bit-mtp

# Embeddings (RAG, on mlx-serve)
EMBED_URL=http://127.0.0.1:11455
EMBED_MODEL_ID=bge-m3-q8
```

### Log Files

| File | Service |
|------|---------|
| `logs/orchestrator.log` | Go orchestrator |
| `logs/ui.log` | Vite dev server |

LLM inference logs live on the mlx-serve side.

### PID Tracking

Process PIDs are written to `.pids`. `Stop-Omni.ps1` uses both this file and live port lookups to ensure all processes are stopped.

### Legacy Scripts

---

## 3. Stop-Omni.ps1

Stops the Go orchestrator and the Vite UI. mlx-serve profiles are not stopped.

### Synopsis

```
pwsh scripts/Stop-Omni.ps1
```

No arguments.

### Behavior

Finds the processes listening on port 5173 (UI) and the `ADDR` port (orchestrator) with `lsof`, sends SIGTERM, waits 1 second, then sends SIGKILL to any survivors.

PIDs recorded in `.pids` are also checked and stopped. The `.pids` file is removed at the end.

### Note

To stop mlx-serve as well, use mlx-serve's own script:

```bash
pwsh /Users/kf/AI/MLX/scripts/Stop-MlxServe.ps1 -All
```

---

## 4. build_rulebook.py

Rebuilds the rulebook rows of `rag_chunks` from the OCR'd rulebook Markdown. This is the way to import the rulebook. The chunking itself lives in `ai-agents/rulebook_chunker.py`.

### Synopsis

```
uv run ai-agents/build_rulebook.py [options]
```

| Option | Default | Description |
|--------|---------|-------------|
| `--md` | `data/rulebook/swordworld2.5_rulebook1.md` | Rulebook Markdown |
| `--db` | `data/omni.db` | Database file path |
| `--source` | `rulebook1` | Name stored in `rag_chunks.source` |
| `--skip-pages` | `1-4,242-245` | PDF pages to leave out (cover / table of contents, and colophon / cover / blank sheet) |
| `--preview` | `logs/rulebook-preview.jsonl` | Where to write the preview JSONL |
| `--apply` | — | Replace the rulebook rows in the DB. Without it, preview only |

### Preview (default)

Builds the chunks, prints the count, character lengths (min / median / max) and the count per tag, and writes every chunk to the `--preview` JSONL. The DB is not touched.

### --apply

1. Backs up the DB to `data/backup/omni.bak-<timestamp>.db` (via SQLite's backup API, consistent even in WAL mode)
2. Deletes `source_type='rulebook'` rows whose `source` is the `--source` value or empty (and their `rag_chunks_opt` rows)
3. Inserts the new chunks (`embedding` is NULL)
4. Keeps house rule and correction rows. If a correction or house rule points at a row to be deleted via `overrides_id`, it exits without changing anything (re-point it in the UI and run again)

Then run `embed_chunks.py` to create the embeddings.

### How Chunks Are Built

- `## ページ N` marks a PDF page. One PDF page holds a two-page spread of the book; its book pages are `2N-4` and `2N-3`
- The table of contents (PDF pages 3–4) is read to map book pages to part / chapter / item
- Text is split at headings and paragraphs, never mid-sentence (target ~600 characters; paragraphs over 900 characters are split at 「。」; tables are split by rows every 900 characters with the header row repeated)
- Ruby readings in the form 「漢字（かな）」 are removed, and full-width alphanumerics become half-width
- Left out: page-number-only lines, table-of-contents leader lines, illustration credits, ©GROUP SNE lines, checkbox rows, page layout elements (sidebars, navigation, footers, ability charts, etc.), blank form tables (half or more of the body rows empty or holding only the first-column label), the sample characters' filled sheets (their explanatory prose is kept), and the index / designer's notes / staff list chapters
- Runs of short headed entries (glossary style) are merged into one chunk with `【見出し】` labels. A short trailing piece is attached to the previous chunk and a short intro to the next. Identical text is deduplicated
- Each chunk gets:
  - `section`: part > chapter > item > heading path
  - `page_from` / `page_to`: book pages
  - `tag`: `intro` / `character` / `rule` / `combat` / `magic` / `item` / `world` / `gm` / `monster` / `scenario` (`general` for text the table of contents does not cover)
- The sample scenario 「蛮族を駆逐せよ」 is imported with tag `scenario` and `enabled=0`, so the GM does not pull the scenario's story into ordinary sessions. It can be enabled from the RULES screen in the UI

### Examples

```bash
# Preview only
uv run ai-agents/build_rulebook.py

# Look at the preview
head -3 logs/rulebook-preview.jsonl

# Apply to the DB and embed
uv run ai-agents/build_rulebook.py --apply
uv run ai-agents/embed_chunks.py
```

### Tests

```bash
cd ai-agents && uv run pytest
```

Unit tests for the chunker live in `ai-agents/tests/` (pytest is a dev dependency).

---

## 5. ocr_rulebook.py

Imports short house-rule or correction text files (PDF / Markdown / plain text) into the `rag_chunks` table.

> Do not use it for the rulebook itself. It cuts text mechanically every 500 characters, so page-number fragments, the table of contents and blank sheets end up in the chunks and ruby readings remain. Import the rulebook with [build_rulebook.py](#4-build_rulebookpy).

### Synopsis

```
uv run ai-agents/ocr_rulebook.py <file> [options]
```

| Option | Default | Description |
|--------|---------|-------------|
| `<file>` | (required) | File to import (.pdf / .md / .txt) |
| `--source-type` | `rulebook` | `rulebook` / `correction` / `houserule` (normally pass `correction` or `houserule`) |
| `--db` | `data/omni.db` | Database file path |

### Source Types and Priority

At equal similarity, higher-priority chunks are preferred during RAG retrieval.

| Source Type | Priority | Use Case |
|-------------|----------|----------|
| `houserule` | 20 | Custom rules (highest priority) |
| `correction` | 10 | Official errata / corrections |
| `rulebook` | 0 | Standard rulebook content |

### Supported Formats

**PDF (.pdf)**

Text is extracted using `pdfplumber`. Works for text-embedded PDFs; scanned image PDFs are not supported.

**Markdown (.md)**

Sections are split on `# / ## / ###` headings.

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
# Add house rules from a text file
uv run ai-agents/ocr_rulebook.py \
    data/houserules.txt \
    --source-type houserule \
    --db data/omni.db

# Add a corrections file (Markdown)
uv run ai-agents/ocr_rulebook.py \
    data/corrections.md \
    --source-type correction \
    --db data/omni.db
```

---

## 6. embed_chunks.py

Generates vector embeddings for rows in `rag_chunks` and `rag_chunks_opt` that do not have one yet.

### Synopsis

```
uv run ai-agents/embed_chunks.py [options]
```

| Option | Default | Description |
|--------|---------|-------------|
| `--db PATH` | `data/omni.db` | Database file path |
| `--embed-url URL` | `http://127.0.0.1:11455` | URL of the mlx-serve embedding profile |
| `--embed-model-id ID` | `bge-m3-q8` | Model ID of the embedding profile (sent as `model`) |
| `--batch N` | `32` | Rows per commit |
| `--reembed-all` | — | Clear all existing embeddings (`rag_chunks` / `rag_chunks_opt`) and rebuild them |

### Behavior

1. With `--reembed-all`, set every `embedding` in `rag_chunks` and `rag_chunks_opt` back to NULL
2. Fetch rows `WHERE embedding IS NULL` from each table, `--batch` at a time
3. POST each row's text to `{embed_url}/v1/embeddings` as `{"model": <embed-model-id>, "input": <text>}`. For `rag_chunks`, when `section` is set the text is the section, a newline, then the body (the same as the orchestrator's `rag.EmbedInput`); for `rag_chunks_opt` it is the body only
   - If the embedding server rejects the input as too large (mlx-serve's bge-m3-q8 takes up to 2048 tokens per input; normal chunks fit, so this is a safety net), the end of the text is cut off and the request retried
4. Store the returned vector (float32 array) as a BLOB in the `embedding` column, committing every `--batch` rows
5. Repeat until no pending rows remain (rows that fail stay NULL and a warning is printed)

At startup it checks `{embed_url}/health`; if the server does not answer, it prints the `Start-MlxServe.ps1` command to run and exits.

### Changing the Embedding Model

A different model produces vectors of a different dimension (e.g. 768 for nomic-embed → 1024 for bge-m3). Old and new vectors cannot be mixed, so rebuild them all:

```bash
uv run ai-agents/embed_chunks.py --db data/omni.db --reembed-all
```

### Check Status

```bash
sqlite3 data/omni.db \
  "SELECT count(*) FROM rag_chunks WHERE embedding IS NULL;"
sqlite3 data/omni.db \
  "SELECT count(*) FROM rag_chunks_opt WHERE embedding IS NULL;"
```

### Example

```bash
# Embed every pending row
uv run ai-agents/embed_chunks.py \
    --db data/omni.db \
    --embed-url http://127.0.0.1:11455 \
    --batch 100
```

---

## 7. optimize_chunks.py

Uses a local LLM to rewrite `rag_chunks`, fixing OCR errors, stripping furigana, adding classification tags, and generating one-line summaries. Results go into `rag_chunks_opt` and are preferred over raw chunks in RAG retrieval.

> This is an optional, legacy tool. `build_rulebook.py` removes ruby and noise deterministically, so it is normally not needed for the rulebook. `build_rulebook.py --apply` deletes the `rag_chunks_opt` rows of the rows it replaces.

### Synopsis

```
uv run ai-agents/optimize_chunks.py [options]
```

| Option | Default | Description |
|--------|---------|-------------|
| `--limit N` | `20` | Max chunks to process per run |
| `--model-url URL` | `http://127.0.0.1:11441` | Text optimization LLM URL (mlx-serve) |
| `--model-id ID` | `gemma4-31b-it-heretic-4bit-mtp` | Text optimization LLM model ID |
| `--embed-url URL` | `http://127.0.0.1:11455` | Embedding LLM URL (mlx-serve) |
| `--embed-model-id ID` | `bge-m3-q8` | Embedding LLM model ID |
| `--db PATH` | `data/omni.db` | Database file path |
| `--dry-run` | — | Print results without writing to DB |
| `--retry-failed` | — | Re-process chunks with `failed` status |
| `--status` | — | Show processing statistics and exit |
| `--chunk-id N` | — | Process only the specified chunk (debug) |

### Processing Pipeline

```
rag_chunks (raw text)
    ↓  LLM call (OPTIMIZE_PROMPT, /v1/chat/completions)
    ↓  Parse JSON response
rag_chunks_opt (optimized text + tag + summary)
    ↓  Re-embed
rag_chunks_opt.embedding
```

### Classification Tags

The LLM selects one of eight tags per chunk (`rag_chunks_opt.tag`, separate from the `rag_chunks.tag` set by `build_rulebook.py`):

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
    --model-url http://127.0.0.1:11441 \
    --embed-url http://127.0.0.1:11455

# Retry failed chunks
uv run ai-agents/optimize_chunks.py --retry-failed --limit 20

# Dry-run for a single chunk (debug)
uv run ai-agents/optimize_chunks.py --chunk-id 42 --dry-run

# Use a different mlx-serve profile (pass both URL and model ID)
uv run ai-agents/optimize_chunks.py \
    --model-url http://127.0.0.1:<port> \
    --model-id <profile> \
    --limit 100
```

### Notes

- Both the mlx-serve chat model (default port 11441) and embedding model (11455) must be running.
- Every request carries `model` (the model ID), a random `seed` and thinking-off flags (shared module `ai-agents/mlx_client.py`). If the model ID differs from the mlx-serve profile, mlx-serve swaps out its resident model.
- Running during an active game session may impact LLM response times. Prefer off-hours execution.
- Typical processing speed: 3–10 seconds per chunk, depending on model speed.

---

## 8. generate_scenarios.py

Batch-generates scenario text and ComfyUI images, saving everything to the database.

### Synopsis

```
uv run ai-agents/generate_scenarios.py [options]
```

| Option | Default | Description |
|--------|---------|-------------|
| `--count N` | `1` | Number of scenarios to generate |
| `--rank RANK` | `C` | Difficulty rank (A/B/C/D/E) |
| `--model-url URL` | `http://127.0.0.1:11441` | Text generation LLM URL (mlx-serve) |
| `--model-id ID` | `gemma4-31b-it-heretic-4bit-mtp` | Text generation LLM model ID |
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
0 2 * * * cd /Users/kf/AI/SW2.5-Omni-Master && \
    uv run ai-agents/generate_scenarios.py \
    --count 2 --rank C --no-images --publish-quest \
    >> logs/generate_scenarios.log 2>&1
```

### Notes

- The mlx-serve chat model (default port 11441, `/v1/chat/completions`) must be running for text generation.
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

Create it from `config.env.example` (`cp config.env.example config.env`). Main keys:

```bash
# API server
ADDR=:8080
DB_PATH=/Users/kf/AI/SW2.5-Omni-Master/data/omni.db

# ComfyUI (leave empty to disable image generation)
COMFY_URL=http://192.168.1.10:8188

# Chat model (mlx-serve, shared by GM / NPC / Support)
LLM_URL=http://127.0.0.1:11441
LLM_MODEL_ID=gemma4-31b-it-heretic-4bit-mtp
LLM_TIMEOUT_SEC=300
STAGE_RETRY_LIMIT=2

# Embeddings (mlx-serve, RAG)
EMBED_URL=http://127.0.0.1:11455
EMBED_MODEL_ID=bge-m3-q8
```
