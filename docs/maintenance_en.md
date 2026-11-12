# SW2.5 Omni-Master — Maintenance Procedures

> Version: 1.0 / Language: English

---

## 1. Session Procedures

### 1.1 Starting a Session

The LLMs are served by mlx-serve (`/Users/kf/AI/MLX`). Omni-Master's start script does not launch mlx-serve, so start these two profiles first, from the MLX dashboard (http://127.0.0.1:11470/) or with the scripts. Only one chat LLM can be resident at a time, so if another chat model is running, switch to Gemma.

```bash
pwsh /Users/kf/AI/MLX/scripts/Start-MlxServe.ps1 -Profile gemma4-31b-it-heretic-4bit-mtp   # :11441 GM/NPC/Support/summary
pwsh /Users/kf/AI/MLX/scripts/Start-MlxServe.ps1 -Profile bge-m3-q8                        # :11455 embeddings
pwsh /Users/kf/AI/MLX/scripts/Get-MlxServeStatus.ps1                                       # check they are up
```

- Setting `APC_CHECKPOINT_GUARD_TOKENS=8` and `APC_CHECKPOINT_ENTRIES=4` in the Gemma profile's environment is recommended, so the prefix cache keeps working across stages (details in [mlx-serve-client-notes.md](mlx-serve-client-notes.md))
- For scene images, also start the MLX image studio mlx-studio-web (:11462). It uses Gemma to pick the composition, so Gemma must be running too. If you do not want images, set `IMAGE_EVERY=0` in `config.env`

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

Verification:
- UI: `http://localhost:5173` (the header shows whether the LLM, embedding model and image studio are up; a banner says what is down)
- API: `http://localhost:8080` (`curl http://localhost:8080/api/system` returns the same status)

In the UI, create a session with 新しく on the start screen, or open an earlier one from 続きから.

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

Deletes every session (with its turns, checkpoints and summary) and the quests. NPC definitions and PC sheets are preserved.

```bash
./setup.sh
```

Tables cleared:
- `sessions` (cascades to `npc_sheets`, `session_logs`, `checkpoints`, `turn_results`, `session_summaries`)
- `quests`

Tables preserved:
- `rag_chunks` (rulebook data)
- `rag_chunks_opt`
- `player_characters`
- `scenarios`, `scenario_images`

To delete a single session, do not use `setup.sh`; use the 🗑 button under 続きから on the start screen.

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

Keep backups in `data/backup/` (ignored by git). `build_rulebook.py --apply` also writes `data/backup/omni.bak-<timestamp>.db` automatically before it changes anything.

```bash
# Backup (WAL mode makes plain cp unreliable — use sqlite3's built-in backup)
mkdir -p data/backup
sqlite3 data/omni.db ".backup 'data/backup/omni_backup_$(date +%Y%m%d).db'"
```

Restore with the services stopped (while the orchestrator holds the DB open, an overwrite does not take effect):

```bash
pwsh scripts/Stop-Omni.ps1
cp data/backup/<file>.db data/omni.db
rm -f data/omni.db-shm data/omni.db-wal
pwsh scripts/Start-Omni.ps1
```

---

## 3. Rulebook Management

### 3.1 (Re)importing the Rulebook

The rulebook itself is rebuilt with `build_rulebook.py` from the OCR'd Markdown (default `data/rulebook/swordworld2.5_rulebook1.md`). It chunks by headings and paragraphs without cutting sentences, strips noise such as ruby readings, page numbers, the table of contents, layout elements and blank character-sheet forms, and labels each chunk with its part > chapter > heading and book pages. See [tools_en.md](tools_en.md#4-build_rulebookpy) for details.

The procedure has three steps: preview, apply, embed.

```bash
# 1. Preview only (DB untouched). Prints chunk counts per tag and writes logs/rulebook-preview.jsonl
uv run ai-agents/build_rulebook.py

# 2. Replace the rulebook rows in the DB (backs up to data/backup/ first)
uv run ai-agents/build_rulebook.py --apply

# 3. Generate embeddings (requires mlx-serve bge-m3-q8)
uv run ai-agents/embed_chunks.py
```

What `--apply` does:
- Writes a backup to `data/backup/omni.bak-<timestamp>.db`
- Deletes `source_type='rulebook'` rows whose `source` is `rulebook1` (the `--source` value) or empty, then inserts the new chunks (rows you disabled by hand are replaced too)
- Keeps house rule and correction rows (`houserule` / `correction`)
- Aborts without changing anything if a correction or house rule points at a row to be deleted via `overrides_id`. Re-point it in the UI and run again

> `ocr_rulebook.py` cuts text mechanically every 500 characters, so do not use it for the rulebook itself. Use it only to add short house-rule or correction text files.

After `--apply`, restart the orchestrator (`pwsh scripts/Stop-Omni.ps1 && pwsh scripts/Start-Omni.ps1`). The term index (which looks up feats, spells and so on by reading) is built on the first search after startup and is not rebuilt when the DB is changed outside the UI / API.

Verify after import:

```bash
sqlite3 data/omni.db \
  "SELECT tag, enabled, count(*) FROM rag_chunks WHERE source_type='rulebook' GROUP BY tag, enabled;"
```

If something goes wrong, restore a file from `data/backup/` as described in [2.4](#24-backup-and-restore).

### 3.2 Generating Embedding Vectors

Freshly imported chunks have no embeddings (embedding=NULL). RAG search requires embeddings.

```bash
# Make sure mlx-serve bge-m3-q8 is running on port 11455
uv run ai-agents/embed_chunks.py \
    --db data/omni.db \
    --embed-url http://127.0.0.1:11455 \
    --batch 50
```

Every row without an embedding in `rag_chunks` and `rag_chunks_opt` is processed. For `rag_chunks` the embedded text is the section (part > chapter > heading), a newline, then the body. When a row (typically a table) exceeds the embedding server's input limit (2048 tokens per input for bge-m3-q8), it is retried with the text shortened.

Check progress:

```bash
sqlite3 data/omni.db \
  "SELECT count(*) FROM rag_chunks WHERE embedding IS NULL;"
```

When you change the embedding model, the vector dimension changes (e.g. 768 → 1024), so clear the existing embeddings and rebuild them all:

```bash
uv run ai-agents/embed_chunks.py --db data/omni.db --reembed-all
```

### 3.3 Chunk Optimization (Optional, Legacy)

`optimize_chunks.py` uses an LLM to rewrite chunks into `rag_chunks_opt` (fixing OCR errors, removing furigana, adding tags, generating summaries). Optimized chunks, when present, are preferred in RAG search. Since `build_rulebook.py` removes ruby and noise deterministically, it is normally not needed for the rulebook. `--apply` also deletes the `rag_chunks_opt` rows of the rows it replaces.

```bash
# Check current status
uv run ai-agents/optimize_chunks.py --status

# Run a batch (best done during off-hours)
uv run ai-agents/optimize_chunks.py \
    --limit 50 \
    --model-url http://127.0.0.1:11441 \
    --embed-url http://127.0.0.1:11455
```

### 3.4 Disabling and Enabling Chunks

`build_rulebook.py` leaves out covers, the table of contents, colophons, blank forms and similar noise at import time, so manual disabling is normally unnecessary. Disable a chunk only when you want to keep it out of RAG search.

- In the UI: Rules screen → the chunk's Enable/Disable toggle (`enabled`). Each row shows its part > chapter > heading and book pages
- The sample scenario 「蛮族を駆逐せよ」 (tag `scenario`) is imported disabled so the GM does not pull the scenario's story into a live session. Enable it in the Rules screen when you play that scenario

```bash
# With SQL (e.g. enable the whole sample scenario)
sqlite3 data/omni.db \
  "UPDATE rag_chunks SET enabled = 1 WHERE source_type='rulebook' AND tag='scenario';"
```

> The enabled flag is stored on the chunk rows, so rebuilding with `build_rulebook.py --apply` resets it to the default (only the sample scenario disabled).

### 3.5 Adding House Rules and Corrections

Add via the Rules screen or directly via the API. Rules added or edited through the UI or API are embedded right away and become searchable immediately. House rules and corrections get a priority bonus in ranking, so they come above a similar rulebook passage. If embedding fails the rule is still saved (the `POST` response has `"embedded": false`); start mlx-serve and run `embed_chunks.py` later.

```bash
# Via UI: Rules screen → 「＋ ハウスルール・訂正を足す」, or 「訂正を作る」 on a row

# Via API
curl -X POST http://localhost:8080/api/rules \
  -H 'Content-Type: application/json' \
  -d '{
    "source_type": "houserule",
    "tag": "combat",
    "text": "In this campaign, apply a +2 bonus to Willpower."
  }'
```

While a correction or house rule with `overrides_id` set to an original chunk's ID is enabled, the original chunk is left out of RAG search and only the fix is used. Disabling the correction makes the original searchable again.

To see which rules the game will pick, use the search on the Rules screen (`GET /api/rules/search?q=...`, the same search the game uses). Chunks found by term reading show the matched term.

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
personality:
  tone: Calm and analytical
  speech_style: Formal, academic phrasing
  values: Unraveling ancient magic
backstory: A researcher at the mages' guild.
action_rules:
  - Cast spells from range
  - Do not engage in melee combat
```

See [spec_en.md](spec_en.md#84-session-memory) for the keys that reach the LLM (`personality`'s `tone` / `speech_style` / `values` / `fears` / `quirks`, `backstory`, `action_rules`, `equipment`, `skills`, …) and `data/personas/example_gard.yaml` for the full format.

After creating the file, open a session, pick the persona on the **Party** screen and press 「仲間に加える」 (up to 3 NPCs per session).

### 4.2 Updating a Persona

A persona YAML is read when the NPC is added to a session and copied into the NPC sheet. Editing the file affects NPCs added afterwards (no restart needed); NPCs already in a session do not change.

To fix an NPC already in a session:
1. **Party** screen → select the NPC → 「✎ 編集」. Only the values change; the other YAML keys are kept
2. To change the keys themselves, turn on **DEV** in the header and use 「YAML を直接編集 (DEV)」 (removing keys loses the persona, so take care)

NPC personas are part of the fixed session prefix, so edits apply from the next turn.

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

Use the **Party** screen → 「＋ PC を作る」. The sheet summary of the PC set to 「使う」 (one at a time, across all sessions) goes into the fixed session prefix.

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

Click **DEV** at the right end of the UI header. A warning banner appears.

### 7.2 NPC YAML Editor (Party Screen)

With DEV on, each NPC's details show a 「YAML を直接編集 (DEV)」 button. Edits are saved to the DB; invalid YAML is rejected with an error message.

### 7.3 Undoing Turns and Deleting Checkpoints

Undoing turns does not need DEV mode; it is done on the Session screen:

- ⟲ このターンの前に戻す (or 「戻す」 on a checkpoint in the side panel): go back to before that turn and delete the later turns. NPC HP/MP and placement go back too
- ↻ 作り直す / ✎ 入力欄に戻して直す: redo the last turn
- ⏹ 止める: cancel the running turn (it is as if it never happened)

Only the latest 10 checkpoints per session are kept. There is no UI to delete one, but the API can:

```bash
curl -X DELETE http://localhost:8080/api/checkpoint/5
```

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

### Turn Processing Timeout / "Turn in Progress"

- Sending a turn returns 202 right away and the UI waits on WebSocket progress events. There is no UI-side timeout
- Cap per turn: `TURN_TIMEOUT_SEC` in `config.env` (default 900 seconds). A turn that runs over goes back to its start and an error is shown
- Timeout per LLM request: `LLM_TIMEOUT_SEC` (default 300 seconds)
- All stages run serially on one model, so a turn takes GM + each NPC + Support. Re-thinking adds to that (check `stage_attempts`), and every few turns a summary update follows
- To favor speed, lower `STAGE_RETRY_LIMIT`. Also check that the Gemma profile has the recommended APC settings (§1.1)
- Only one turn runs per session. Sending while one runs (including the summary update) returns 409 「このセッションではターンを処理中です」; wait for it to finish or cancel it with ⏹

### Scene Images Not Generating

```bash
# Status (images.ok)
curl http://localhost:8080/api/system

# Check the image studio
curl http://127.0.0.1:11462/api/health

# Why it failed
grep "\[image\]" logs/orchestrator.log
```

- Both the image studio (mlx-studio-web) and Gemma (mlx-serve), which picks the composition, must be running
- Images are drawn only on action turns: turn 1 and turns that are multiples of `IMAGE_EVERY` (default 3)
- To play without images, set `IMAGE_EVERY=0` in `config.env` (an empty `IMAGES_URL` falls back to the default, so it does not disable them)

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
| As needed | Delete sessions you no longer need | 🗑 under 続きから on the start screen |
| When rebuilding all data | DB reset | `./setup.sh` |
| Before each session | Review available quests | Quests screen in UI |
| Monthly | Pre-generate scenarios | `generate_scenarios.py --count 5 --publish-quest` |
| Monthly | Database backup | `sqlite3 data/omni.db ".backup 'data/backup/omni_backup.db'"` |
| When rulebook updates | Rebuild chunks | `build_rulebook.py` → `build_rulebook.py --apply` → `embed_chunks.py` |
| As needed | Add house rules | Rules screen → 「＋ ハウスルール・訂正を足す」 |

---

## 10. Service Port Reference

| Service | Port | Description |
|---------|------|-------------|
| Vite UI | 5173 | Frontend development server |
| Go Orchestrator | 8080 | REST API + WebSocket |
| mlx-serve `gemma4-31b-it-heretic-4bit-mtp` | 11441 | Chat model shared by GM, NPC and Support (managed by mlx-serve) |
| mlx-serve `bge-m3-q8` | 11455 | Embedding vector generation (managed by mlx-serve) |
| mlx-studio-web | 11462 | MLX image studio. Scene images (optional, managed on the MLX side) |
| MLX dashboard | 11470 | Starts and stops mlx-serve profiles |
| ComfyUI | 8188 | Image generation for `generate_scenarios.py` (Windows side) |
