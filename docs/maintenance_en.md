# SW2.5 Omni-Master — Maintenance Procedures

> Version: 1.0 / Language: English

---

## 1. Session Procedures

### 1.1 Starting a Session

The LLMs are served by mlx-serve (`/Users/kf/AI/MLX`). Omni-Master's start script does not launch mlx-serve, so start these two profiles first:

```bash
pwsh /Users/kf/AI/MLX/scripts/Start-MlxServe.ps1 -Profile gemma4-31b-it-heretic-4bit-mtp   # :11441 GM/NPC/Support
pwsh /Users/kf/AI/MLX/scripts/Start-MlxServe.ps1 -Profile bge-m3-q8                        # :11455 embeddings
pwsh /Users/kf/AI/MLX/scripts/Get-MlxServeStatus.ps1                                       # check they are up
```

Then start Omni-Master (PowerShell 7):

```bash
# Run from the project root
cd /Users/kf/AI/SW2.5-Omni-Master
pwsh scripts/Start-Omni.ps1           # add -Follow to tail the logs
```

Startup order:
1. Check `/health` of `LLM_URL` / `EMBED_URL` from `config.env` (if either is down, print the `Start-MlxServe.ps1` command to run and exit)
2. Go orchestrator (built, then started)
3. Vite UI dev server
4. With `-Follow`, tail `logs/orchestrator.log` and `logs/ui.log`

PIDs are recorded in `.pids`. If `.pids` is left over, the script refuses to start to avoid a double launch (run `Stop-Omni.ps1` first).

> `start.sh` / `stop.sh` are legacy scripts from the old setup (which launched llama-server directly). They are deprecated and will be removed.

Verification:
- UI: `http://localhost:5173`
- API: `http://localhost:8080`

### 1.2 Stopping a Session

```bash
pwsh scripts/Stop-Omni.ps1
```

Identifies the processes listening on the UI port (5173) and the orchestrator port (from `ADDR`), sends SIGTERM, waits 1 second, then sends SIGKILL if needed. Any PIDs left in `.pids` are stopped too and the file is removed. mlx-serve profiles are not stopped. To stop mlx-serve as well:

```bash
pwsh /Users/kf/AI/MLX/scripts/Stop-MlxServe.ps1 -All
```

### 1.3 Viewing Logs

```bash
# Real-time (Start-Omni.ps1 -Follow shows these automatically)
tail -f logs/orchestrator.log logs/ui.log
```

LLM inference logs live on the mlx-serve side (see the mlx-serve README). Each stage's output and its re-thinking history (`[NG 1/3] ...`) are recorded in the `session_logs` table.

---

## 2. Database Management

### 2.1 Session Reset (Preserve Rule Data)

Reset between sessions or before a new campaign. NPC definitions and PC sheets are preserved.

```bash
./setup.sh
```

Tables cleared:
- `sessions` (cascades to `npc_sheets`, `session_logs`, `checkpoints`)
- `quests`

Tables preserved:
- `rag_chunks` (rulebook data)
- `rag_chunks_opt`
- `player_characters`
- `scenarios`, `scenario_images`

After running, session #1 is automatically recreated.

### 2.2 Full Reset (Erase All Data)

```bash
./setup.sh --full
```

Drops all tables including `rag_chunks`. Rulebook data must be re-imported afterward.

### 2.3 Direct Database Access

```bash
sqlite3 data/omni.db

# Useful queries
.tables                           -- list all tables
SELECT count(*) FROM rag_chunks WHERE enabled=1;
SELECT * FROM scenarios ORDER BY id DESC LIMIT 5;
SELECT COALESCE(opt_status,'unprocessed'), count(*)
  FROM rag_chunks GROUP BY opt_status;
.quit
```

### 2.4 Backup and Restore

```bash
# Backup (WAL mode makes plain cp unreliable — use sqlite3's built-in backup)
sqlite3 data/omni.db ".backup 'data/omni_backup_$(date +%Y%m%d).db'"

# Restore
cp data/omni_backup_YYYYMMDD.db data/omni.db
```

---

## 3. Rulebook Management

### 3.1 Importing a Rulebook

```bash
# Import Markdown or plain text
uv run ai-agents/ocr_rulebook.py \
    data/rulebook/swordworld2.5_rulebook1.md \
    --db data/omni.db \
    --source-type rulebook

# PDF import (uses pdfplumber)
uv run ai-agents/ocr_rulebook.py \
    data/rulebook/rulebook.pdf \
    --db data/omni.db
```

Verify after import:

```bash
sqlite3 data/omni.db \
  "SELECT count(*) FROM rag_chunks WHERE source_type='rulebook';"
```

### 3.2 Generating Embedding Vectors

Freshly imported chunks have no embeddings (embedding=NULL). RAG search requires embeddings.

```bash
# Make sure mlx-serve bge-m3-q8 is running on port 11455
uv run ai-agents/embed_chunks.py \
    --db data/omni.db \
    --embed-url http://127.0.0.1:11455 \
    --batch 50
```

Every row without an embedding in `rag_chunks` and `rag_chunks_opt` is processed.

Check progress:

```bash
sqlite3 data/omni.db \
  "SELECT count(*) FROM rag_chunks WHERE embedding IS NULL;"
```

When you change the embedding model, the vector dimension changes (e.g. 768 → 1024), so clear the existing embeddings and rebuild them all:

```bash
uv run ai-agents/embed_chunks.py --db data/omni.db --reembed-all
```

### 3.3 Chunk Optimization (Optional, Recommended)

Uses an LLM to rewrite chunks, fixing OCR errors, removing furigana, adding tags, and generating summaries. Optimized chunks are preferred in RAG search.

```bash
# Check current status
uv run ai-agents/optimize_chunks.py --status

# Run a batch (20 chunks at a time; best done during off-hours)
uv run ai-agents/optimize_chunks.py \
    --limit 50 \
    --model-url http://127.0.0.1:11441 \
    --embed-url http://127.0.0.1:11455

# Retry failed chunks
uv run ai-agents/optimize_chunks.py --retry-failed --limit 20

# Preview a single chunk (debug)
uv run ai-agents/optimize_chunks.py --chunk-id 42 --dry-run
```

### 3.4 Disabling Problem Chunks

Disable non-rule pages (covers, table of contents, colophons) to prevent noise in RAG results.

```bash
# Via UI (recommended)
# RULES tab → filter by source_type: rulebook
# → click a chunk → toggle Enable/Disable

# Or bulk-update with SQL
sqlite3 data/omni.db << 'EOF'
UPDATE rag_chunks
SET enabled = 0
WHERE source_type = 'rulebook'
  AND (
    text GLOB '*Sword World 2.5*Author*' OR
    text GLOB '*Table of Contents*' OR
    length(text) < 50
  );
EOF
```

### 3.5 Adding House Rules and Corrections

Add via the RULES screen or directly via the API.

```bash
# Via UI: RULES tab → "+ ADD" button (top right)

# Via API
curl -X POST http://localhost:8080/api/rules \
  -H 'Content-Type: application/json' \
  -d '{
    "source_type": "houserule",
    "tag": "combat",
    "text": "In this campaign, apply a +2 bonus to Willpower."
  }'
```

---

## 4. NPC Persona Management

### 4.1 Adding a New Persona

Create `data/personas/{id}.yaml`. The `id` field must match the filename (without extension).

```yaml
id: new_npc
name: Character Name
race: Human
classes:
  - name: Sorcerer
    level: 3
hp: 20
hp_max: 20
mp: 30
mp_max: 30
persona:
  personality: Calm and analytical
  motivation: Unraveling ancient magic
  speech: Formal, academic phrasing
  priorities:
    - Cast spells from range
  forbidden:
    - Engage in melee combat
```

After creating the file, add the NPC via **INFORMATION** tab → "PARTY MEMBERS" → "+ ADD".

### 4.2 Updating a Persona

Edit the YAML file directly, then restart the session (`pwsh scripts/Stop-Omni.ps1 && pwsh scripts/Start-Omni.ps1`).

Emergency in-session edits (DEV mode):
1. Enable **DEV MODE** in the UI header
2. Go to **PARTY** tab → select the NPC
3. Scroll to the bottom: "DEV — Raw YAML Editor"
4. Edit YAML → "Save YAML"

---

## 5. Scenario Batch Generation

### 5.1 Text-Only Scenario Generation

Works without ComfyUI.

```bash
uv run ai-agents/generate_scenarios.py \
    --count 3 \
    --rank C \
    --no-images \
    --publish-quest
```

- `--count` number of scenarios to generate
- `--rank` difficulty rank (A/B/C/D/E)
- `--no-images` skip image generation
- `--publish-quest` automatically register in the quest board

### 5.2 Full Generation with Images (ComfyUI Required)

```bash
# Specify the Windows-side ComfyUI address
uv run ai-agents/generate_scenarios.py \
    --count 2 \
    --rank B \
    --comfy-url http://192.168.1.10:8188 \
    --publish-quest
```

The model is auto-detected. To specify manually:

```bash
uv run ai-agents/generate_scenarios.py \
    --comfy-url http://192.168.1.10:8188 \
    --comfy-model "v1-5-pruned-emaonly.safetensors"
```

### 5.3 Pre-generating Shared Background Images

Generates 8 reusable background types (inn, guild, forest, etc.) shared across scenarios.

```bash
uv run ai-agents/generate_scenarios.py \
    --backgrounds \
    --comfy-url http://192.168.1.10:8188
```

Output: `data/images/backgrounds/`

### 5.4 Regenerating Images for an Existing Scenario

Add images to a scenario that was generated without them:

```bash
# Check scenario IDs
uv run ai-agents/generate_scenarios.py --status

# Regenerate images for a specific scenario
uv run ai-agents/generate_scenarios.py \
    --scenario-id 3 \
    --comfy-url http://192.168.1.10:8188
```

### 5.5 Checking Generation Status

```bash
uv run ai-agents/generate_scenarios.py --status
```

---

## 6. Player Character Management

### 6.1 Creating and Updating PCs

Use **INFORMATION** tab → "PLAYER CHARACTER" → "+ NEW".

PC sheet JSON format (`json_blob` column):

```json
{
  "name": "Lina",
  "race": "Elf",
  "adventurerLevel": 4,
  "classes": [
    {"name": "Wizard", "level": 4, "baseSkill": 6}
  ],
  "attrs": {
    "dex": {"base": 12, "growth": 2},
    "agi": {"base": 10, "growth": 1},
    "str": {"base": 8, "growth": 0},
    "vit": {"base": 9, "growth": 1},
    "int": {"base": 16, "growth": 3},
    "spr": {"base": 14, "growth": 2}
  },
  "hpMax": 18, "hpCurrent": 18,
  "mpMax": 32, "mpCurrent": 32,
  "gold": 2500,
  "languages": "Common, Elvish, Ancient",
  "combatFeats": "Arcane Knowledge"
}
```

### 6.2 Managing PCs via API

```bash
# List all PCs
curl http://localhost:8080/api/player-characters

# Create a PC
curl -X POST http://localhost:8080/api/player-characters \
  -H 'Content-Type: application/json' \
  -d '{"name": "Lina", "json_blob": "{...}"}'

# Activate a PC for the current session
curl -X PATCH http://localhost:8080/api/player-characters/1/activate
```

---

## 7. DEV Mode

DEV mode is an emergency tool for correcting broken game states. **Use carefully during live sessions.**

### 7.1 Enabling

Click **DEV MODE** in the upper-right corner of the UI header. An amber warning banner appears.

### 7.2 Checkpoint Management (INFORMATION Tab)

All checkpoints are listed with a "✕" delete button when DEV mode is on.

```bash
# Also available via API
curl -X DELETE http://localhost:8080/api/checkpoint/5
```

### 7.3 Turn Log Deletion (SESSION Tab)

A red "✕" button appears at the start of each turn. Clicking it deletes all log entries for that turn from both the local view and the database.

```bash
# Also available via API
curl -X DELETE "http://localhost:8080/api/session-log/turn/3?session_id=1"
```

### 7.4 NPC YAML Editor (PARTY Tab)

The "DEV — Raw YAML Editor" section appears at the bottom of each NPC detail page. Edits are validated for syntax before saving. Invalid YAML is rejected with an error message.

---

## 8. Troubleshooting

### UI Not Loading

```bash
# Check if Vite is running
tail -20 logs/ui.log

# Check if the port is in use
lsof -i :5173

# Restart UI only
cd ui && npm run dev -- --host
```

### LLM Not Responding

```bash
# Check mlx-serve
curl http://127.0.0.1:11441/health    # chat model
curl http://127.0.0.1:11455/health    # embeddings
pwsh /Users/kf/AI/MLX/scripts/Get-MlxServeStatus.ps1

# Start a profile that is down
pwsh /Users/kf/AI/MLX/scripts/Start-MlxServe.ps1 -Profile gemma4-31b-it-heretic-4bit-mtp
pwsh /Users/kf/AI/MLX/scripts/Start-MlxServe.ps1 -Profile bge-m3-q8

# The orchestrator's startup check
grep mlx-serve logs/orchestrator.log
```

- Make sure `LLM_MODEL_ID` / `EMBED_MODEL_ID` match the mlx-serve profile names. mlx-vlm returns 422 when `model` is missing and reloads the resident model when a different name is sent.

### An NPC Does Not Act / HP and MP Are Applied As-Is

When a stage never passes validation, the reason is added to `warnings` in the `TurnResult` (the NPC sits out that turn; if Support fails, the NPCs' own `hp_delta` / `mp_cost` are used). `stage_attempts` shows how many calls each stage took.

```bash
# Show rejected outputs and their errors
sqlite3 data/omni.db \
  "SELECT turn, role, substr(content,1,200) FROM session_logs WHERE content LIKE '[NG %' ORDER BY id DESC LIMIT 20;"
```

The number of re-thinks is set by `STAGE_RETRY_LIMIT` in `config.env` (default 2).

### RAG Search Not Working (No Embeddings / Dimension Mismatch)

```bash
# Count chunks without embeddings
sqlite3 data/omni.db \
  "SELECT count(*) FROM rag_chunks WHERE embedding IS NULL AND enabled=1;"

# Generate missing embeddings
uv run ai-agents/embed_chunks.py \
    --embed-url http://127.0.0.1:11455

# After changing the embedding model (old vectors have a different dimension), rebuild all
uv run ai-agents/embed_chunks.py --reembed-all
```

### Turn Processing Timeout

- UI timeout is 180 seconds (`AbortSignal.timeout(180000)` in `SessionView.tsx`)
- Timeout per LLM request: `LLM_TIMEOUT_SEC` in `config.env` (default 300 seconds)
- All stages run serially on one model, so a turn takes GM + each NPC + Support. Re-thinking adds to that (check `stage_attempts`)
- To favor speed, lower `STAGE_RETRY_LIMIT`

### ComfyUI Images Not Generating

```bash
# Test connectivity
curl http://192.168.1.10:8188/system_stats

# Check config.env
grep COMFY_URL config.env

# To skip image generation and keep the game running
# Set COMFY_URL= (empty string) in config.env
```

### Database Lock Error

```bash
# Run WAL checkpoint
sqlite3 data/omni.db "PRAGMA wal_checkpoint(FULL);"

# Delete WAL files (all services must be stopped first)
pwsh scripts/Stop-Omni.ps1
rm -f data/omni.db-shm data/omni.db-wal
pwsh scripts/Start-Omni.ps1
```

### Start-Omni.ps1 Exits Asking You to Start mlx-serve Profiles

`/health` of `LLM_URL` or `EMBED_URL` is not answering. Start the profile with the `Start-MlxServe.ps1` command it prints, then run it again.

### Start-Omni.ps1 Exits Saying a Previous .pids Was Found

The previous stop did not complete. Run `pwsh scripts/Stop-Omni.ps1`, then start again.

---

## 9. Recommended Maintenance Schedule

| Frequency | Task | Command |
|-----------|------|---------|
| Before each session | DB reset | `./setup.sh` |
| Before each session | Review available quests | QUEST BOARD in UI |
| Monthly | Chunk optimization batch | `optimize_chunks.py --limit 200` |
| Monthly | Pre-generate scenarios | `generate_scenarios.py --count 5 --publish-quest` |
| Monthly | Database backup | `sqlite3 data/omni.db ".backup 'backup.db'"` |
| When rulebook updates | Re-import chunks | `ocr_rulebook.py` → `embed_chunks.py` |
| As needed | Add house rules | RULES tab → "+ ADD" |

---

## 10. Service Port Reference

| Service | Port | Description |
|---------|------|-------------|
| Vite UI | 5173 | Frontend development server |
| Go Orchestrator | 8080 | REST API + WebSocket |
| mlx-serve `gemma4-31b-it-heretic-4bit-mtp` | 11441 | Chat model shared by GM, NPC and Support (managed by mlx-serve) |
| mlx-serve `bge-m3-q8` | 11455 | Embedding vector generation (managed by mlx-serve) |
| ComfyUI | 8188 | Image generation (Windows side) |
