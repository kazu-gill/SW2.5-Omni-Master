# SW2.5 Omni-Master — System Specification

> Version: 1.0 / Language: English

---

## 1. System Overview

**SW2.5 Omni-Master** is an AI-driven orchestration system for the TRPG "Sword World 2.5". A single local chat LLM handles the Game Master (GM), NPC and Support (resource validation) roles one after another, generating responses to player action declarations. The LLMs are served by mlx-serve (`/Users/kf/AI/MLX`), a separate inference platform; Omni-Master itself does not launch any inference process. Rule consistency is maintained by retrieving relevant rulebook passages via vector search (RAG), and optional integration with ComfyUI enables automated scene image generation.

### Key Features

| Feature | Description |
|---------|-------------|
| **Autonomous GM** | LLM acts as GM, generating situational narration and rulings for each player action |
| **NPC Multi-Agent** | NPCs take turns on the same model, each with an individual personality. Every stage's output is validated and re-thought if invalid |
| **RAG Rule Lookup** | Imported rulebook is searched by embedding vector and injected into LLM prompts |
| **Image Generation** | ComfyUI (Stable Diffusion) generates scene images and NPC portraits |
| **Real-time UI** | WebSocket broadcasts turn results to all connected clients |
| **Checkpoints** | Session state is auto-saved and restorable (up to 10 per session) |
| **Batch Maintenance** | Rulebook chunking, embedding generation and scenario pre-generation run offline |

---

## 2. Architecture

```
┌──────────────────────────────────────────────────────┐
│                 Browser (React UI)                   │
│  INFORMATION │ SESSION │ QUEST BOARD │ PARTY │ RULES │
└──────────────────────┬───────────────────────────────┘
                       │ HTTP / WebSocket
┌──────────────────────▼──────────────────────────────┐
│           Go Orchestrator (:8080)                   │
│  ┌──────────┐  ┌────────────┐  ┌────────────────┐   │
│  │ REST API │  │ Game Engine │  │ WebSocket Hub  │  │
│  └──────────┘  └─────┬──────┘  └────────────────┘   │
│                      │ HTTP (OpenAI-compatible)     │
└──────────────────────┼──────────────────────────────┘
                       │
┌──────────────────────▼──────────────────────────────┐
│  mlx-serve (/Users/kf/AI/MLX, separate repo)        │
│  ┌─────────────────────────────┐ ┌────────────────┐ │
│  │ Chat LLM :11441              │ │ Embed :11455   │ │
│  │ gemma4-31b-it-heretic-4bit-  │ │ bge-m3-q8      │ │
│  │ mtp (GM / NPC / Support)     │ │ (1024-dim)     │ │
│  └─────────────────────────────┘ └────────────────┘ │
└─────────────────────────────────────────────────────┘
        ▲
        │ RAG Context
  ┌─────┴──────────────────────┐
  │     SQLite DB (omni.db)    │
  │  rag_chunks / sessions /   │
  │  npc_sheets / quests / ... │
  └────────────────────────────┘

  ┌──────────────────────────┐
  │  ComfyUI (Windows host)  │  ← Optional
  │  http://192.168.x.x:8188 │
  └──────────────────────────┘

  ┌──────────────────────────┐
  │  Python Batch Tools      │  ← Maintenance only
  │  ai-agents/*.py          │
  └──────────────────────────┘
```

---

## 3. Technology Stack

| Layer | Technology | Role |
|-------|-----------|------|
| **Frontend** | React 19 + TypeScript + Vite | Game UI |
| **Backend** | Go 1.26+ | REST API and game engine |
| **Database** | SQLite (modernc/sqlite) | All persistent data |
| **LLM Runtime** | mlx-serve (separate repo) | Local LLM inference (OpenAI-compatible API) |
| **Chat Model** | Gemma 4 31B-it (`gemma4-31b-it-heretic-4bit-mtp`, mlx-vlm + MTP) | GM, NPC and Support roles, run serially on one model |
| **Embed Model** | BGE-M3 (`bge-m3-q8`, 1024-dim) | RAG vector generation |
| **Image Gen** | ComfyUI + Stable Diffusion | Scene and portrait images |
| **Batch Tools** | Python 3.14+ + uv | Rulebook processing, scenario generation |

---

## 4. Directory Structure

```
sw25-omni-master/
├── orchestrator/               # Go backend
│   ├── cmd/server/main.go      # Entry point
│   └── pkg/
│       ├── api/handler.go      # HTTP routes and WebSocket
│       ├── config/config.go    # Configuration loader
│       ├── db/                 # SQLite access layer
│       │   ├── schema.sql      # Table definitions
│       │   ├── sessions.go
│       │   ├── checkpoints.go
│       │   ├── rag.go
│       │   ├── scenarios.go
│       │   ├── quests.go
│       │   └── player_characters.go
│       ├── game/               # Game logic
│       │   ├── turn.go         # Turn orchestration
│       │   ├── types.go        # Data type definitions
│       │   ├── stage.go        # Per-stage call, validation and re-thinking
│       │   ├── validate.go     # GM / NPC / Support output validation
│       │   └── comfyui.go      # Image generation client
│       ├── llm/                # LLM HTTP client (for mlx-serve)
│       └── rag/                # Vector search engine
├── ui/                         # React frontend
│   └── src/
│       ├── App.tsx             # Root, navigation
│       ├── views/              # Screen components
│       ├── hooks/              # useGameSocket.ts
│       └── utils/              # questEligibility.ts
├── ai-agents/                  # Python batch tools
│   ├── mlx_client.py           # Shared mlx-serve helpers
│   ├── build_rulebook.py       # Rulebook (re)builder
│   ├── rulebook_chunker.py     # Rulebook Markdown chunker
│   ├── ocr_rulebook.py         # House rule / correction text importer
│   ├── embed_chunks.py         # Embedding generator
│   ├── optimize_chunks.py      # Chunk optimizer (optional, legacy)
│   ├── tests/                  # pytest (chunker unit tests)
│   └── generate_scenarios.py   # Scenario batch generator
├── data/
│   ├── omni.db                 # SQLite database
│   ├── backup/                 # DB backups (ignored by git)
│   ├── personas/               # NPC persona YAML files
│   ├── rulebook/               # Source rulebook files
│   └── images/                 # Generated images
│       ├── backgrounds/        # Shared background images
│       └── scenarios/{id}/     # Per-scenario images
├── docs/                       # Documentation
├── logs/                       # Service logs
├── config.env                  # Configuration file
├── scripts/
│   ├── Start-Omni.ps1          # Start orchestrator + UI
│   └── Stop-Omni.ps1           # Stop orchestrator + UI
├── setup.sh                    # DB initialization / reset
├── start.sh                    # Legacy start script (deprecated, to be removed)
└── stop.sh                     # Legacy stop script (deprecated, to be removed)
```

---

## 5. Configuration (config.env)

Values in `config.env` are loaded by `scripts/Start-Omni.ps1` and the Go server at startup. Priority: OS environment variable > config.env > built-in default.

| Key | Default | Description |
|-----|---------|-------------|
| `DB_PATH` | `data/omni.db` | SQLite database file path |
| `ADDR` | `:8080` | Go server listen address |
| `COMFY_URL` | `""` (empty = disabled) | ComfyUI endpoint URL |
| `LLM_URL` | `http://127.0.0.1:11441` | Chat model endpoint (mlx-serve profile), shared by GM, NPC and Support |
| `LLM_MODEL_ID` | `gemma4-31b-it-heretic-4bit-mtp` | `model` sent with every request. Must match the mlx-serve profile name |
| `LLM_TIMEOUT_SEC` | `300` | Timeout per LLM request (seconds) |
| `STAGE_RETRY_LIMIT` | `2` | How many times a stage is re-thought after its output fails validation (0 = no retry) |
| `EMBED_URL` | `http://127.0.0.1:11455` | Embedding model endpoint (mlx-serve profile) |
| `EMBED_MODEL_ID` | `bge-m3-q8` | `model` sent with embedding requests |
| `PERSONAS_DIR` | `data/personas` | Directory containing NPC persona YAML files |

> `LLM_MODEL_ID` / `EMBED_MODEL_ID` must match the `model_id` of the mlx-serve profile. mlx-vlm returns 422 when `model` is missing, and reloads the resident model when a different name is sent.

---

## 6. Database Schema

### 6.1 sessions
Session-level metadata. Currently assumes session ID=1.

| Column | Type | Description |
|--------|------|-------------|
| `id` | INTEGER PK | Session ID |
| `quest_id` | TEXT | Active quest ID |
| `created_at` / `updated_at` | DATETIME | Timestamps |

### 6.2 npc_sheets
Real-time state of NPCs active in the session.

| Column | Type | Description |
|--------|------|-------------|
| `id` | INTEGER PK | Sheet ID |
| `session_id` | INTEGER FK | Session ID |
| `name` | TEXT | NPC name (unique per session) |
| `hp` / `mp` | INTEGER | Current HP/MP |
| `position_x` / `position_y` | INTEGER | Battle map coordinates (Y: 0-2=enemy / 3-5=front / 6-7=party rear) |
| `yaml_blob` | TEXT | Full NPC YAML including persona |
| `updated_at` | DATETIME | Last update timestamp |

### 6.3 session_logs
Full audit trail of all turn communications.

| Column | Type | Description |
|--------|------|-------------|
| `session_id` | INTEGER FK | Session ID |
| `turn` | INTEGER | Turn number |
| `role` | TEXT | `player` / `gm` / `npc` / `support` |
| `content` | TEXT | Message content |
| `validated_at` | DATETIME | Validation timestamp |

### 6.4 checkpoints
Turn-level snapshots for session restore (max 10 per session).

| Column | Type | Description |
|--------|------|-------------|
| `session_id` | INTEGER FK | Session ID |
| `turn` | INTEGER | Turn number when saved |
| `snapshot_json` | TEXT | Complete game state as JSON |
| `created_at` | DATETIME | Save timestamp |

### 6.5 rag_chunks
Rulebook text split into searchable chunks, plus house rules and corrections.

| Column | Type | Description |
|--------|------|-------------|
| `source_type` | TEXT | `rulebook` / `correction` / `houserule` |
| `priority` | INTEGER | `20`=houserule / `10`=correction / `0`=rulebook |
| `tag` | TEXT | Rulebook rows: `intro` / `character` / `rule` / `combat` / `magic` / `item` / `world` / `gm` / `monster` / `scenario` / `general`, set by `build_rulebook.py`. Rows added in the UI: any string |
| `text` | TEXT | Chunk text |
| `enabled` | INTEGER | 1=active for RAG / 0=excluded |
| `overrides_id` | INTEGER FK | ID of the chunk this corrects. While this row is enabled, the referenced chunk is left out of RAG search |
| `embedding` | BLOB | float32 vector (little-endian) |
| `opt_status` | TEXT | `NULL`=unprocessed / `done` / `failed` / `skip` |
| `source` | TEXT | Import source (e.g. `rulebook1`). Empty for rows added through the UI / API |
| `section` | TEXT | Part > chapter > item > heading. Prepended to the body when embedding. May be empty |
| `page_from` | INTEGER | Book page (start of range). NULL if unknown |
| `page_to` | INTEGER | Book page (end of range). NULL if unknown |

On an existing DB, `source` / `section` / `page_from` / `page_to` are added automatically when the orchestrator starts (`db.Open`) and when `build_rulebook.py` / `embed_chunks.py` run.

### 6.6 rag_chunks_opt
LLM-optimized variants of chunks (preferred in RAG search).

| Column | Type | Description |
|--------|------|-------------|
| `chunk_id` | INTEGER FK UNIQUE | Source chunk ID |
| `text` | TEXT | Optimized text |
| `tag` | TEXT | LLM-assigned tag |
| `summary` | TEXT | One-line summary (≤30 chars) |
| `embedding` | BLOB | Optimized text embedding |
| `model` | TEXT | Model URL used |
| `optimized_at` | DATETIME | Optimization timestamp |

### 6.7 player_characters
Player character sheets.

| Column | Type | Description |
|--------|------|-------------|
| `name` | TEXT | Character name |
| `json_blob` | TEXT | JSON sheet (stats, skills, HP, etc.) |
| `is_active` | INTEGER | 1=currently selected (only one at a time) |

### 6.8 quests
Quest board entries.

| Column | Type | Description |
|--------|------|-------------|
| `rank` | TEXT | `A` / `B` / `C` / `D` / `E` |
| `title` | TEXT | Quest name |
| `description` | TEXT | Quest overview |
| `client` | TEXT | Client NPC |
| `reward` | TEXT | Reward description |
| `target` | TEXT | Objective |
| `level` | TEXT | Recommended level (e.g. `Adventurer Level 3-5`) |
| `tags` | TEXT | Comma-separated: `combat,explore,dungeon,social` |
| `status` | TEXT | `available` / `active` / `completed` |

### 6.9 scenarios
Batch-generated adventure scenario materials.

| Column | Type | Description |
|--------|------|-------------|
| `title` | TEXT | Scenario title |
| `rank` | TEXT | Difficulty rank |
| `summary` | TEXT | One-line summary |
| `description` | TEXT | GM-facing scenario overview |
| `locations_json` | TEXT | Location array (JSON) |
| `enemies_json` | TEXT | Enemy info array (JSON) |
| `plot_hooks_json` | TEXT | Introduction hooks (JSON) |
| `events_json` | TEXT | Event array (JSON) |
| `status` | TEXT | `draft` / `ready` / `used` |

### 6.10 scenario_images
Images linked to scenarios (`scenario_id=NULL` means shared background).

| Column | Type | Description |
|--------|------|-------------|
| `scenario_id` | INTEGER FK | Scenario ID (NULL for shared backgrounds) |
| `category` | TEXT | `scene` / `portrait` / `background` |
| `label` | TEXT | Identifying label |
| `file_path` | TEXT | Absolute path to image file |
| `prompt_text` | TEXT | Generation prompt used |
| `width` / `height` | INTEGER | Image dimensions (px) |

---

## 7. API Endpoints

### Game Engine

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/turn` | Process a player turn |
| `POST` | `/api/gm-channel` | GM direct intervention |
| `GET` | `/api/session/{id}` | Get session state |
| `GET` | `/api/checkpoint/restore/{turn}` | Retrieve a checkpoint |
| `GET` | `/api/comfy/status` | Check ComfyUI connectivity |

### NPC & Party Management

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/personas` | List persona templates |
| `POST` | `/api/npc-sheet` | Add NPC to session |
| `DELETE` | `/api/npc-sheet` | Remove NPC from session |
| `PATCH` | `/api/npc-sheet/{id}` | Update NPC state (HP/MP/YAML) |
| `PATCH` | `/api/npc-position` | Change NPC formation lane |

### Player Characters

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/player-characters` | List all PCs |
| `POST` | `/api/player-characters` | Create new PC |
| `PUT` | `/api/player-characters/{id}` | Update PC |
| `DELETE` | `/api/player-characters/{id}` | Delete PC |
| `PATCH` | `/api/player-characters/{id}/activate` | Set active PC |
| `PATCH` | `/api/player-characters/{id}/deactivate` | Deactivate PC |

### Quests

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/quests` | List quests |
| `PATCH` | `/api/quests/{id}/accept` | Accept quest |
| `PATCH` | `/api/quests/{id}/complete` | Complete quest |

### Rules Management

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/rules` | List chunks (filter: `?source_type=`). Each item includes `section` / `page_from` / `page_to` when set |
| `POST` | `/api/rules` | Add houserule or correction. Embedded right after saving; the response reports the result as `embedded` (true/false) |
| `PATCH` | `/api/rules/{id}` | Update chunk (text, tag, enabled). Re-embedded after the update |
| `DELETE` | `/api/rules/{id}` | Delete chunk |

If embedding fails (e.g. the mlx-serve embedding profile is down), the rule is still saved. Its `embedding` stays NULL; embed it later with `embed_chunks.py`.

### Scenarios

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/scenarios` | List scenarios (filter: `?status=`) |
| `GET` | `/api/scenarios/{id}` | Get scenario detail |
| `GET` | `/api/scenarios/backgrounds` | List shared background images |
| `GET` | `/api/scenarios/{id}/image/{imgid}` | Serve image file |
| `PATCH` | `/api/scenarios/{id}/status` | Update scenario status |

### DEV Mode (Developer Only)

| Method | Path | Description |
|--------|------|-------------|
| `DELETE` | `/api/checkpoint/{id}` | Delete a checkpoint |
| `DELETE` | `/api/session-log/turn/{turn}` | Delete turn logs (`?session_id=` required) |

### WebSocket

| Path | Description |
|------|-------------|
| `/ws` | WebSocket connection. Broadcasts `TurnResult` and `ImageUpdate` |

---

## 8. Game Engine Processing Flow

Every LLM stage calls the same chat model (`LLM_URL`), one stage at a time.

```
Player → POST /api/turn
                │
       ┌────────▼─────────┐
       │  Checkpoint      │ ← Save checkpoint (auto)
       └────────┬─────────┘
       ┌────────▼─────────┐
       │  RAG Search      │ ← Search rag_chunks with the player input (top 5)
       │  (extract rules) │   Priority: houserule > correction > rulebook
       └────────┬─────────┘
                │ Context injection
       ┌────────▼─────────┐
       │  GM              │ ← Generate scene narration (prose)
       └────────┬─────────┘
       ┌────────▼─────────┐
       │  NPC 1 → 2 → 3   │ ← One at a time, in sheet order
       │  Actions (YAML)  │   Based on each NPC's persona
       └────────┬─────────┘
       ┌────────▼─────────┐
       │  Support         │ ← RAG search with the NPCs' actions (top 3),
       │                  │   then confirm HP/MP deltas (JSON array)
       └────────┬─────────┘
       ┌────────▼─────────┐
       │  DB Update       │ ← Update npc_sheets HP/MP and lanes
       │  Log             │   Append session_logs
       └────────┬─────────┘
                │ WebSocket broadcast
       ┌────────▼─────────┐
       │  ComfyUI Image   │ ← Optional, async
       │  Generate/serve  │   Broadcast ImageUpdate separately
       └──────────────────┘
```

### 8.1 Per-Stage Validation and Re-thinking

Each LLM stage's output is validated. If it fails, only that stage is called again ("re-thinking"): the rejected output and the error message are appended to the conversation, and thinking is enabled (`enable_thinking=true`, `max_tokens` +2048). A stage is retried at most `STAGE_RETRY_LIMIT` times (default 2, i.e. up to 3 calls). Transport errors are not retried.

| Stage | Rejected when | When retries run out |
|-------|---------------|----------------------|
| GM | Empty / truncated (`finish_reason=length`) / a refusal / outputs YAML or JSON | The turn fails |
| NPC | YAML does not parse / `name` differs from the sheet / `action` or `dialogue` empty / `mp_cost` outside 0..current MP / `hp_delta` < -current HP / `new_lane` not `enemy`, `front` or `party` | That NPC sits out the turn (recorded in `warnings`) |
| Support | Not a JSON array / not exactly one entry per acting NPC (unknown or duplicate names) / values out of range | The NPCs' own `hp_delta` / `mp_cost` are used (recorded in `warnings`) |

Truncated output (`finish_reason=length`) is rejected at every stage. Every attempt is written to `session_logs`.

### 8.2 LLM Requests

Every chat request (`/v1/chat/completions`) carries:

| Item | Value |
|------|-------|
| `model` | `LLM_MODEL_ID`. mlx-vlm returns 422 without it and reloads the resident model if a different name is sent |
| `seed` | Random per request. Without it mlx-vlm returns identical text for identical input |
| thinking | Normally `{"enable_thinking": false, "chat_template_kwargs": {"enable_thinking": false}}`; enabled only when re-thinking |

Embeddings are requested from `/v1/embeddings` with `{"model": EMBED_MODEL_ID, "input": ...}`.

### 8.3 Additional TurnResult Fields

| Field | Type | Description |
|-------|------|-------------|
| `stage_attempts` | map[string]int | LLM calls per stage. Keys: `gm` / `npc:<name>` / `support`. A value of 2 or more means the stage was re-thought |
| `warnings` | []string | Stages that never validated and were skipped or fell back |

---

## 9. Frontend Screen Layout

| Screen | Nav Label | Description |
|--------|-----------|-------------|
| **INFORMATION** | `start` | Pre-session: NPC selection, PC check, checkpoint restore |
| **SESSION** | `session` | Active gameplay: chat log, action input, battle map, quest acceptance |
| **QUEST BOARD** | `quest` | Quest list with party-level eligibility filter |
| **PARTY** | `npc` | NPC stats, portrait album, attribute editing |
| **RULES** | `rules` | Rulebook search, houserule entry, correction management, enable/disable toggle. Shows each chunk's part > chapter > heading and pages |

### DEV Mode
Activated via the "DEV MODE" button in the header. Unlocks:
- INFORMATION: Delete individual checkpoints
- SESSION: Delete individual turn logs (✕ button per turn in chat)
- PARTY: Direct YAML editing of NPC sheets

---

## 10. NPC Persona YAML Specification

Placed at `data/personas/{id}.yaml`.

```yaml
id: gard                    # System ID (match filename recommended)
name: Gard                  # Display name

race: Dwarf
gender: Male
age: 87

classes:
  - name: Fighter
    level: 4
  - name: Grappler
    level: 2

stats:                      # Ability scores (attrs or stats both accepted)
  str: 14
  dex: 9
  pow: 10
  int: 8
  agl: 8
  luc: 11

hp: 38
hp_max: 38
mp: 10
mp_max: 10

position:                   # Initial battle map coordinates
  x: 2
  y: 5                      # 0-2=enemy rear / 3-5=front / 6-7=party rear

equipment:
  weapon: Battle Axe +1
  armor: Chain Mail
  shield: Large Shield

skills:
  - name: Weapon Mastery (Axe/Hammer)
    rank: 4
    type: combat             # combat / magic / general
    note: +1 to attack rolls with axes and hammers

consumables:
  - name: Magic Arrow
    cur: 3
    max: 5

persona:                    # Injected into LLM system prompt
  personality: Gruff and short-tempered. Deeply loyal to companions.
  motivation: Protect his party at all costs.
  speech: Terse sentences. Uses declarative endings.
  priorities:
    - Take the front line and draw enemy attacks
    - Prioritize defense when HP drops below 10
  forbidden:
    - Delivering a killing blow to a surrendering enemy
    - Attacking women or children
```

### NPC LLM Output Format (fixed YAML)

```yaml
name: Gard
action: Swings axe at enemy
dice: 2d6+8
target: Goblin Warrior
dialogue: "Don't move!"
hp_delta: -15       # negative = damage / positive = healing
mp_cost: 2
new_lane: front     # Only when lane changes (enemy/front/party)
```

---

## 11. ComfyUI Integration

### Connection
- Run ComfyUI on the Windows machine and set `COMFY_URL` to its LAN address
- If `COMFY_URL` is empty, image generation is fully disabled (no impact on gameplay)

### Workflow
Standard txt2img pipeline:

```
CheckpointLoaderSimple → CLIPTextEncode (positive/negative)
    → EmptyLatentImage → KSampler → VAEDecode → SaveImage
```

- Steps: 25
- CFG Scale: 7.0
- Sampler: Euler Ancestral / Scheduler: Karras
- Scene images: 768×512 px (landscape)
- Portraits: 512×768 px (portrait)

### Image Retrieval Flow
1. `POST /prompt` → obtain `prompt_id`
2. Poll `GET /history/{prompt_id}` every 2 seconds (up to 120–180 seconds)
3. On completion, download via `GET /view?filename=...&type=output`

---

## 12. RAG (Rule Search) Specification

### Chunk Creation
- Chunk the rulebook (OCR'd Markdown) with `build_rulebook.py` (`rulebook_chunker.py`)
  - Split by headings and paragraphs, never mid-sentence (target ~600 characters; paragraphs over 900 characters split at 「。」; tables split by rows every 450 characters with the header row repeated)
  - Ruby readings, full-width alphanumerics, page numbers, the table of contents, layout elements, blank form tables, the sample characters' filled sheets and similar noise are removed deterministically
  - Book pages are mapped to part / chapter / item via the table of contents, and each chunk gets `section` (part > chapter > item > heading), `page_from` / `page_to` and `tag`
  - The sample scenario 「蛮族を駆逐せよ」 is imported with `tag='scenario'` and `enabled=0`, so the GM does not pull the scenario's story into ordinary sessions
  - `--apply` backs up the DB to `data/backup/`, then replaces the rulebook rows (`source` = `rulebook1` or empty). House rules and corrections are kept
- Short house-rule or correction text files can be added with `ocr_rulebook.py` (500-character chunks; not for the rulebook itself)
- Vectorize via `embed_chunks.py` (mlx-serve `bge-m3-q8`, 1024-dim)
  - For `rag_chunks`, when `section` is set the embedded text is the section, a newline, then the body (the same as Go's `rag.EmbedInput`), so a query about a topic also matches chunks whose body never names it
  - If the embedding server rejects the input as too large (bge-m3-q8 takes up to 512 tokens), the text is shortened and retried
- Rules added or edited through the UI / API (`POST /api/rules`, `PATCH /api/rules/{id}`) are embedded by the orchestrator immediately
- When the embedding model changes, the vector dimension changes too, so re-embed every chunk (`embed_chunks.py --reembed-all`)
- `optimize_chunks.py` (LLM rewrite into `rag_chunks_opt`) is optional and legacy; normally not needed for the rulebook

### Search Algorithm
1. Convert input text to an embedding vector
2. Candidates are rows with `enabled=1` and an embedding, except chunks that an enabled correction or house rule overrides via `overrides_id` (only the fix is used; a disabled correction does not hide the original)
3. Sort by cosine similarity; ties broken by `priority` (houserule first)
4. If `rag_chunks_opt` exists for a chunk, use its text and vector instead
5. Format the top-K results and inject them into the prompt

### Stages That Use It
| Stage | Query | Count | Purpose |
|-------|-------|-------|---------|
| GM | Player input | 5 | Reference for narration and rulings |
| Support | NPCs' actions (`action` and `dice`) | 3 | Follow values given in the rules (e.g. spell MP cost) when confirming HP/MP |
| NPC | — | — | No rules are given |

If the search fails or finds nothing, the stage continues without rules.

### Context Format

The header `【参照ルール】` appears once, followed by one line per chunk:

```
【参照ルール】
[ハウスルール] <text>
[訂正] <text>
[<section> p.<page_from>-<page_to>] <text>
```

Rulebook chunks are labeled with their `section` and book pages (`p.<page_from>` for a single page, no page when unknown, `source_type` when `section` is empty).

### Priority Levels
| `priority` | `source_type` | Description |
|------------|--------------|-------------|
| `20` | `houserule` | House rules (highest priority) |
| `10` | `correction` | OCR corrections and clarifications |
| `0` | `rulebook` | Original rulebook text |

---

## 13. Scenario Batch Generation

`generate_scenarios.py` generates and stores:

```
Scenario JSON
  ├── title, rank, summary, description
  ├── client, reward, target, level
  ├── locations[]  → ComfyUI background images (768×512)
  ├── enemies[]    → ComfyUI portrait images (512×768)
  ├── plot_hooks[] → Introduction hooks
  └── events[]     → Event descriptions
```

Storage:
- Scenario data → `scenarios` table
- Image files → `data/images/scenarios/{id}/`
- Image paths → `scenario_images` table
- Shared backgrounds → `data/images/backgrounds/` (`scenario_id=NULL`)

Use `--publish-quest` to also insert into the `quests` table, making the scenario selectable from the quest board.
