# SW2.5 Omni-Master — Maintenance Procedures

> Version: 1.0 / Language: English

---

## 1. Session Procedures

### 1.1 Starting a Session

```bash
# Run from the project root
cd /Users/kf/Work/sw25-omni-master
./start.sh
```

Startup order:
1. `llama-server` ×6 (GM / Support / NPC×3 / Embed)
2. 10-second initialization wait
3. Go orchestrator (auto-built)
4. Vite UI dev server
5. All logs tailed via `tail -f`

Verification:
- UI: `http://localhost:5173`
- API: `http://localhost:8080`

### 1.2 Stopping a Session

```bash
./stop.sh
```

Identifies processes listening on the managed ports, sends SIGTERM, waits 1 second, then sends SIGKILL if needed. Cleans up `.pids` files.

### 1.3 Viewing Logs

```bash
# Real-time (start.sh shows these automatically)
tail -f logs/orchestrator.log
tail -f logs/gm.log
tail -f logs/ui.log

# All LLM logs at once
tail -f logs/gm.log logs/support.log \
        logs/npc-a.log logs/npc-b.log logs/npc-c.log \
        logs/embed.log
```

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
# Make sure the embed server is running on port 11435
uv run ai-agents/embed_chunks.py \
    --db data/omni.db \
    --embed-url http://localhost:11435 \
    --batch 50
```

Check progress:

```bash
uv run ai-agents/embed_chunks.py --db data/omni.db --status
```

### 3.3 Chunk Optimization (Optional, Recommended)

Uses an LLM to rewrite chunks, fixing OCR errors, removing furigana, adding tags, and generating summaries. Optimized chunks are preferred in RAG search.

```bash
# Check current status
uv run ai-agents/optimize_chunks.py --status

# Run a batch (20 chunks at a time; best done during off-hours)
uv run ai-agents/optimize_chunks.py \
    --limit 50 \
    --model-url http://localhost:11430 \
    --embed-url http://localhost:11435

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
port: 11432         # one of 11432–11434; must not overlap existing personas
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

Edit the YAML file directly, then restart the session (`./stop.sh && ./start.sh`).

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
# Check individual server health
curl http://localhost:11430/health
curl http://localhost:11435/health

# Check logs
tail -50 logs/gm.log
tail -50 logs/embed.log

# Manual restart
llama-server \
  --model /path/to/model.gguf \
  --port 11430 --host 127.0.0.1 \
  --ctx-size 4096 --n-gpu-layers 99
```

### RAG Search Not Working (No Embeddings)

```bash
# Count chunks without embeddings
sqlite3 data/omni.db \
  "SELECT count(*) FROM rag_chunks WHERE embedding IS NULL AND enabled=1;"

# Generate missing embeddings
uv run ai-agents/embed_chunks.py \
    --embed-url http://localhost:11435
```

### Turn Processing Timeout

- UI timeout is 180 seconds (`AbortSignal.timeout(180000)` in `SessionView.tsx`)
- Try a smaller model for `MODEL_GM`
- Reduce `--ctx-size` to `2048`

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
./stop.sh
rm -f data/omni.db-shm data/omni.db-wal
./start.sh
```

### llama-server Not Found

```bash
# Check PATH
which llama-server

# Set full path in config.env
echo "LLAMA_SERVER=/usr/local/bin/llama-server" >> config.env

# For LM Studio's bundled binary
echo "LLAMA_SERVER=/Applications/LM Studio.app/Contents/Resources/llama-server" >> config.env
```

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
| GM LLM | 11430 | GM role (Gemma-4B equivalent) |
| Support LLM | 11431 | Validation and resource calculation |
| NPC-A LLM | 11432 | First NPC |
| NPC-B LLM | 11433 | Second NPC |
| NPC-C LLM | 11434 | Third NPC |
| Embed LLM | 11435 | Embedding vector generation |
| ComfyUI | 8188 | Image generation (Windows side) |
