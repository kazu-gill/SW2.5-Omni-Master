# SW2.5 Omni-Master — System Specification

> Version: 1.0 / Language: English

---

## 1. System Overview

**SW2.5 Omni-Master** is an AI-driven orchestration system for the TRPG "Sword World 2.5". A single local chat LLM handles the Game Master (GM), NPC and Support (resource validation) roles one after another, generating responses to player action declarations. The LLMs are served by mlx-serve (`~/AI/MLX`), a separate inference platform; Omni-Master itself does not launch any inference process. Rule consistency is maintained by retrieving relevant rulebook passages via vector search and a term index (RAG), and optional integration with the MLX image studio (mlx-studio-web) enables automated scene image generation.

The contract between the UI and the orchestrator (endpoints, WebSocket events, types) is [api.md](api.md). This document describes the processing behind it.

### Key Features

| Feature | Description |
|---------|-------------|
| **Autonomous GM** | LLM acts as GM, generating situational narration and check requests for each player action |
| **NPC Multi-Agent** | NPCs take turns on the same model, each following its persona (personality, speech, backstory, action rules). Every stage's output is validated and re-thought if invalid |
| **Session Memory** | The LLM gets a fixed per-session prefix (table rules, accepted quest, PC, NPC personas), a running summary and the turns not yet summarized (2–4) word for word |
| **Checks** | The GM and NPCs only state the check, base value and target; the code rolls 2d6 |
| **RAG Rule Lookup** | Imported rulebook is searched by embedding vector and by term reading, and injected into LLM prompts |
| **Image Generation** | The MLX image studio (mlx-studio-web) draws a scene image every few turns |
| **Real-time UI** | WebSocket streams each stage's progress and the GM / NPC text as it is generated |
| **Session Management** | Create and delete multiple sessions; cancel, redo, and restore to before any turn (the latest 10 checkpoints are kept) |
| **Batch Maintenance** | Rulebook chunking, embedding generation and scenario pre-generation run offline |

---

## 2. Architecture

```
┌──────────────────────────────────────────────────────┐
│                 Browser (React UI)                   │
│        Session │ Quests │ Party │ Rules              │
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
│  mlx-serve (~/AI/MLX, separate repo)        │
│  ┌─────────────────────────────┐ ┌────────────────┐ │
│  │ Chat LLM :11441              │ │ Embed :11455   │ │
│  │ gemma4-31b-it-heretic-4bit-  │ │ bge-m3-q8      │ │
│  │ mtp (GM / NPC / Support /    │ │ (1024-dim)     │ │
│  │ summary)                     │ │                │ │
│  └─────────────────────────────┘ └────────────────┘ │
│  ┌─────────────────────────────────────────────────┐ │
│  │ mlx-studio-web :11462 (image studio, optional)  │ │
│  │ Gemma picks the composition, ComfyUI draws      │ │
│  └─────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────┘
        ▲
        │ RAG Context
  ┌─────┴──────────────────────┐
  │     SQLite DB (omni.db)    │
  │  rag_chunks / sessions /   │
  │  npc_sheets / turn_results │
  │  / session_summaries / ... │
  └────────────────────────────┘

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
| **Image Gen** | mlx-studio-web (MLX image studio) | Scene images. The batch tool's scenario images use ComfyUI directly |
| **Batch Tools** | Python 3.14+ + uv | Rulebook processing, scenario generation |

---

## 4. Directory Structure

```
sw25-omni-master/
├── orchestrator/               # Go backend
│   ├── cmd/server/main.go      # Entry point
│   └── pkg/
│       ├── api/
│       │   ├── handler.go      # HTTP routes and WebSocket
│       │   ├── sessions.go     # Session API, running turns (one at a time, cancel, redo, restore)
│       │   └── extras.go       # System status, rule search, scene images
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
│       │   ├── context.go      # Fixed session prefix and recent turns
│       │   ├── summary.go      # Session summary
│       │   ├── checks.go       # Checks (2d6)
│       │   └── transcript.go   # One turn = one conversation
│       ├── images/studio.go    # MLX image studio client
│       ├── llm/                # LLM HTTP client (for mlx-serve, SSE streaming)
│       └── rag/                # Vector search engine and term index (terms.go)
├── ui/                         # React frontend
│   └── src/
│       ├── App.tsx             # Root, navigation
│       ├── views/              # Start, session/, quests, party/, rules/
│       ├── hooks/              # useSessionGame.ts, useSessionSocket.ts, useSystemStatus.ts
│       ├── live/turnReducer.ts # Display of the running turn
│       ├── styles/             # theme.css, ui.css (shared look with the MLX screens)
│       ├── dev/                # Mock API for ?mock=1
│       └── utils/              # npcYaml.ts, questEligibility.ts, etc.
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
│       ├── scenarios/{id}/     # Per-scenario images
│       └── sessions/{id}/      # Session scene images (IMAGES_DIR)
├── docs/                       # Documentation
├── logs/                       # Service logs
├── config.env                  # Configuration file
├── scripts/
│   ├── Start-Omni.ps1          # Start orchestrator + UI
│   └── Stop-Omni.ps1           # Stop orchestrator + UI
└── setup.sh                    # DB initialization / reset
```

---

## 5. Configuration (config.env)

Values in `config.env` are loaded by `scripts/Start-Omni.ps1` and the Go server at startup. Priority: OS environment variable > config.env > built-in default.

| Key | Default | Description |
|-----|---------|-------------|
| `DB_PATH` | `data/omni.db` | SQLite database file path |
| `ADDR` | `:8080` | Go server listen address |
| `LLM_URL` | `http://127.0.0.1:11441` | Chat model endpoint (mlx-serve profile), shared by GM, NPC, Support and the summary |
| `LLM_MODEL_ID` | `gemma4-31b-it-heretic-4bit-mtp` | `model` sent with every request. Must match the mlx-serve profile name |
| `LLM_TIMEOUT_SEC` | `300` | Timeout per LLM request (seconds) |
| `STAGE_RETRY_LIMIT` | `2` | How many times a stage is re-thought after its output fails validation (0 = no retry) |
| `TURN_TIMEOUT_SEC` | `900` | Cap on a whole turn (all stages and re-thinks), in seconds. A turn that runs over is rolled back to its start |
| `APC_TENANT` | `sw25` | Sent as the `X-APC-Tenant` header on chat requests, keeping mlx-vlm's prefix cache apart from other apps |
| `IMAGES_URL` | `http://127.0.0.1:11462` | MLX image studio (mlx-studio-web) endpoint |
| `IMAGE_EVERY` | `3` | Draw a scene image on turn 1 and every N-th turn after. `0` = no images |
| `IMAGES_DIR` | `data/images` | Where scene images are saved (under `sessions/<id>/`) |
| `COMFY_URL` | `""` | ComfyUI used by `generate_scenarios.py`. Not used by the game itself |
| `EMBED_URL` | `http://127.0.0.1:11455` | Embedding model endpoint (mlx-serve profile) |
| `EMBED_MODEL_ID` | `bge-m3-q8` | `model` sent with embedding requests |
| `PERSONAS_DIR` | `data/personas` | Directory containing NPC persona YAML files |

> `LLM_MODEL_ID` / `EMBED_MODEL_ID` must match the `model_id` of the mlx-serve profile. mlx-vlm returns 422 when `model` is missing, and reloads the resident model when a different name is sent.
>
> An empty value (in the environment or config.env) counts as unset and the default is used. To turn images off, set `IMAGE_EVERY=0` rather than emptying `IMAGES_URL`.

---

## 6. Database Schema

### 6.1 sessions
Session-level metadata. Sessions are created from the UI (`POST /api/sessions`); there can be any number of them. Deleting a session cascades to its NPC sheets, logs, checkpoints, turn results and summary.

| Column | Type | Description |
|--------|------|-------------|
| `id` | INTEGER PK | Session ID |
| `title` | TEXT | Session name (the UI shows "セッション N" when empty) |
| `quest_id` | TEXT | Accepted quest ID (`quests.id` as text; empty = none) |
| `created_at` / `updated_at` | DATETIME | Timestamps |

### 6.2 npc_sheets
Real-time state of NPCs active in the session.

| Column | Type | Description |
|--------|------|-------------|
| `id` | INTEGER PK | Sheet ID |
| `session_id` | INTEGER FK | Session ID |
| `name` | TEXT | NPC name (unique per session) |
| `hp` / `mp` | INTEGER | Current HP/MP |
| `hp_max` / `mp_max` | INTEGER | Max HP/MP (for the UI's HP/MP bars) |
| `position_x` / `position_y` | INTEGER | Placement. Y gives the lane (0-2=`enemy` enemy rear / 3-5=`front` front line / 6-7=`party` party rear) |
| `yaml_blob` | TEXT | Full NPC YAML including persona |
| `updated_at` | DATETIME | Last update timestamp |

### 6.3 session_logs
Full audit trail of all turn communications.

| Column | Type | Description |
|--------|------|-------------|
| `session_id` | INTEGER FK | Session ID |
| `turn` | INTEGER | Turn number |
| `role` | TEXT | `player` / `gm` / `npc` / `support` / `check` (check result) / `summary` |
| `content` | TEXT | Message content |
| `validated_at` | DATETIME | Validation timestamp |

### 6.4 checkpoints
Snapshots taken before each turn. Only the latest 10 per session are kept.

| Column | Type | Description |
|--------|------|-------------|
| `session_id` | INTEGER FK | Session ID |
| `turn` | INTEGER | The state before this turn |
| `snapshot_json` | TEXT | Snapshot of the NPC sheets (HP/MP, placement, YAML) as JSON |
| `created_at` | DATETIME | Save timestamp |

"Back to before turn N" (restore, redo, and undoing a failed turn) writes the NPC sheets back from turn N's checkpoint and deletes the logs, turn results, summary and checkpoints from turn N on.

### 6.5 turn_results
Finished turns (`TurnResult` as JSON). The source of the UI's chat log and of the recent turns sent to the LLM.

| Column | Type | Description |
|--------|------|-------------|
| `session_id` | INTEGER FK | Session ID |
| `turn` | INTEGER | Turn number (primary key together with `session_id`) |
| `result_json` | TEXT | `TurnResult` (type in [api.md](api.md)) |
| `created_at` | DATETIME | Save timestamp |

### 6.6 session_summaries
The session summary, one sentence per row.

| Column | Type | Description |
|--------|------|-------------|
| `session_id` | INTEGER FK | Session ID |
| `upto_turn` | INTEGER | Turns up to this one are summarized |
| `text` | TEXT | One sentence with an explicit subject |
| `created_at` | DATETIME | Creation timestamp |

### 6.7 rag_chunks
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

### 6.8 rag_chunks_opt
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

### 6.9 player_characters
Player character sheets.

| Column | Type | Description |
|--------|------|-------------|
| `name` | TEXT | Character name |
| `json_blob` | TEXT | JSON sheet (stats, skills, HP, etc.) |
| `is_active` | INTEGER | 1=currently selected (only one at a time) |

### 6.10 quests
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

### 6.11 scenarios
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

### 6.12 scenario_images
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

[api.md](api.md) is the authoritative list of endpoints, request bodies, responses, WebSocket events and types. This section only shows the overall shape.

| Area | Main endpoints |
|------|----------------|
| System | `GET /api/system` (LLM, embedding and image studio status, plus `apc_tenant`) |
| Sessions | `GET/POST /api/sessions`, `GET/DELETE /api/sessions/{id}` |
| Turns | `POST /api/sessions/{id}/turn` (202; 409 while a turn runs), `/cancel`, `/redo`, `/restore`, `/quest` |
| Scene images | `GET /api/sessions/{id}/images/{file}` |
| Party | `GET /api/personas`, `POST/DELETE /api/npc-sheet`, `PATCH /api/npc-sheet/{id}`, `PATCH /api/npc-position` |
| PCs | `GET/POST /api/player-characters`, `PUT/DELETE /api/player-characters/{id}`, `PATCH .../{id}/activate` / `/deactivate` |
| Quests | `GET /api/quests`, `PATCH /api/quests/{id}/accept` / `/complete` |
| Rules | `GET /api/rules` (paged, filtered), `GET /api/rules/search` (the same search the game uses), `POST /api/rules`, `PATCH/DELETE /api/rules/{id}` |
| Scenarios | `GET /api/scenarios`, `GET /api/scenarios/{id}`, `GET /api/scenarios/backgrounds`, `GET /api/scenarios/{id}/image/{imgid}`, `PATCH /api/scenarios/{id}/status` |
| Other | `DELETE /api/checkpoint/{id}` (delete a checkpoint; not used by the UI) |
| WebSocket | `/ws?session_id={id}`. The server sends `stage` / `delta` / `rules` / `check` / `turn` / `error` / `image` / `busy` |

Rules added or edited (`POST /api/rules`, `PATCH /api/rules/{id}`) are embedded right after saving. If embedding fails (e.g. the mlx-serve embedding profile is down), the rule is still saved; its `embedding` stays NULL, so embed it later with `embed_chunks.py`.

---

## 8. Game Engine Processing Flow

Every LLM stage calls the same chat model (`LLM_URL`), one stage at a time. Only one turn runs per session; sending, redoing, restoring or deleting while a turn runs (including the summary update) returns 409.

```
Player → POST /api/sessions/{id}/turn  → 202 {"turn": N} (the server assigns the number)
                │                         progress then arrives over WebSocket
       ┌────────▼─────────┐
       │  Checkpoint      │ ← Save the NPC sheets as they are before turn N (latest 10 kept)
       └────────┬─────────┘
       ┌────────▼─────────┐
       │  Session context │ ← Fixed prefix + summary + last 2 turns (§8.4)
       └────────┬─────────┘
       ┌────────▼─────────┐
       │  RAG Search      │ ← Search rules with the player input (top 5, §12)
       └────────┬─────────┘
       ┌────────▼─────────┐
       │  GM              │ ← Narration, streamed. "判定: …" lines are rolled by code
       └────────┬─────────┘   (a GM appeal ends here; NPCs and numbers do not move)
       ┌────────▼─────────┐
       │  NPC 1 → 2 → 3   │ ← One at a time in sheet order, following each persona. Streamed
       │  Actions (YAML)  │   A `check` field is rolled by code
       └────────┬─────────┘
       ┌────────▼─────────┐
       │  Support         │ ← RAG search with the NPCs' actions (top 3), then confirm
       │                  │   HP/MP deltas with the check results (JSON array)
       └────────┬─────────┘
       ┌────────▼─────────┐
       │  DB Update       │ ← npc_sheets HP/MP and lanes, turn_results, session_logs
       └────────┬─────────┘
                │ WebSocket: turn (the finished turn)
       ┌────────▼─────────┐
       │  Summary update  │ ← When 3 unsummarized turns have piled up (§8.4)
       └────────┬─────────┘
       ┌────────▼─────────┐
       │  Scene image     │ ← Turn 1 and every IMAGE_EVERY turns (async, §11)
       └──────────────────┘
```

GM and NPC text is received from mlx-serve as SSE and passed to the UI as `delta` events. Each stage's start, re-think and end arrive as `stage` events, the rules given to a stage as `rules`, and checks as `check`.

When a turn fails (the GM never validates, a transport error, …), is cancelled (`/cancel`), or runs past `TURN_TIMEOUT_SEC` (default 900 s), the session goes back to its state before turn N and an `error` event is sent. `LLM_TIMEOUT_SEC` caps each single request.

Redo (`/redo`) goes back to before the given turn and sends the input again in the same mode (action / appeal), optionally with new text (the UI only redoes the last turn). Restore (`/restore`) only goes back. Both work only for turns whose checkpoint is still kept (the latest 10).

### 8.1 Per-Stage Validation and Re-thinking

Each LLM stage's output is validated. If it fails, only that stage is called again ("re-thinking"): the rejected output and the error message are appended to the conversation, and thinking is enabled (`enable_thinking=true`, `max_tokens` +2048). A stage is retried at most `STAGE_RETRY_LIMIT` times (default 2, i.e. up to 3 calls). Transport errors are not retried.

| Stage | Rejected when | When retries run out |
|-------|---------------|----------------------|
| GM | Empty (after the check lines are taken out) / truncated (`finish_reason=length`) / a refusal / outputs YAML or JSON | The turn fails (rolled back to its start) |
| NPC | YAML does not parse / `name` differs from the sheet / `action` or `dialogue` empty / `mp_cost` outside 0..current MP / `hp_delta` < -current HP / `new_lane` not `enemy`, `front` or `party` | That NPC sits out the turn (recorded in `warnings`) |
| Support | Not a JSON array / not exactly one entry per acting NPC (unknown or duplicate names) / values out of range | The NPCs' own `hp_delta` / `mp_cost` are used (recorded in `warnings`) |

Truncated output (`finish_reason=length`) is rejected at every stage. Every attempt is written to `session_logs`. When a re-think starts, a `stage` event with `status: retry` arrives and the UI discards that stage's text and receives it again.

### 8.2 LLM Requests

Every chat request (`/v1/chat/completions`) carries:

| Item | Value |
|------|-------|
| `model` | `LLM_MODEL_ID`. mlx-vlm returns 422 without it and reloads the resident model if a different name is sent |
| `X-APC-Tenant` header | `APC_TENANT` (default `sw25`). Keeps the prefix cache apart from other apps |
| `stream` | `true` for the GM and NPC stages (received as SSE and streamed to the UI). Support and the summary use plain responses |
| `seed` | Random per request. Without it mlx-vlm returns identical text for identical input |
| thinking | Normally `{"enable_thinking": false, "chat_template_kwargs": {"enable_thinking": false}}`; enabled only when re-thinking |

Embeddings are requested from `/v1/embeddings` with `{"model": EMBED_MODEL_ID, "input": ...}`.

#### One turn = one conversation (for prefix caching)

All stages of a turn are sent as one growing conversation after the session context (§8.4). Each stage's request is "the previous stage's request + its answer + this stage's instruction", so mlx-vlm's automatic prefix caching (APC) reuses the KV of everything before and only prefills the new instruction.

```
system    Fixed prefix (table rules + accepted quest + PC + NPC personas) + summary
user      【ターンN-2・プレイヤーの行動】…   assistant  that turn's recap (GM, NPC lines and actions, check results)
user      【ターンN-1・プレイヤーの行動】…   assistant  same
user      【参照ルール】【パーティの状態】【プレイヤーの行動】【指示: GM】
assistant GM narration (including check lines)
user      【判定結果】【指示: NPC「A」】 … assistant  A's YAML
user      【判定結果】【指示: NPC「B」】 … assistant  B's YAML   ← B sees A's action and picks a different one
user      【判定結果】【指示: NPC「C」】 … assistant  C's YAML
user      【判定結果】【指示: 数値計算担当】(Support rules + action list JSON)
```

Anything that changes every turn (HP/MP, lanes, retrieved rules, the stage's instruction) goes in the last user message.

- Only answers that passed validation stay in the conversation; re-think exchanges and skipped NPCs' output do not
- A re-think enables thinking, which changes the start of the template, so it misses the cache (acceptable since re-thinks are rare)
- With APC off in mlx-serve every stage prefills the whole conversation and turns get slower. The server checks `apc.enabled` in `/health` at startup and logs a warning when it is off. See `docs/mlx-serve-client-notes.md` for the mlx-serve settings

### 8.3 Main TurnResult Fields

| Field | Type | Description |
|-------|------|-------------|
| `mode` | string | `action` / `gm` (GM appeal) |
| `checks` | []Check | Checks rolled by the code (who, check, base, target, dice, total, result) |
| `rules_used` | []RuleHit | Rules given to the stages (`matched_term` set when picked by the term index) |
| `stage_attempts` | map[string]int | LLM calls per stage. Keys: `gm` / `npc:<name>` / `support`. A value of 2 or more means the stage was re-thought |
| `warnings` | []string | Stages that never validated and were skipped or fell back |
| `image_url` | string | Scene image (only on illustrated turns; attached later) |

See `TurnResult` in [api.md](api.md) for every field.

### 8.4 Session Memory

Instead of starting a new conversation each turn, the session's story reaches the LLM in three parts (`pkg/game/context.go`, `summary.go`).

| Part | Content | Where |
|------|---------|-------|
| Fixed prefix | Table rules (roles take turns, the system rolls the dice, …), the accepted quest, a summary of the active PC sheet (race, classes, abilities, max HP/MP, gear, combat feats), the NPC personas | system message |
| Summary | The `session_summaries` sentences, oldest first | end of the system message |
| Recent turns | The turns not yet in the summary, word for word: always the last 2, up to 4 just before a summary is written (player input; GM narration, NPC lines and actions, check results) | user / assistant messages after the system message |

- The prefix is ordered from least to most likely to change (table rules → quest → PC → NPCs) so it stays the same across turns and mlx-vlm's APC can reuse it
- NPC personas come from the sheet YAML: race, gender, age, classes, max HP/MP, `personality`'s `tone` / `speech_style` / `values` / `fears` / `quirks`, `backstory`, `action_rules`, `equipment` and `skills` names with their descriptions (without them the model makes up what a feat does when asked). Missing keys are skipped
- Summary: after each finished turn, once 3 or more unsummarized turns older than the last 2 have piled up, a recorder stage (`stage: summary`) folds them into 3–6 sentences, each with an explicit subject, and appends them to `session_summaries`. The session stays busy during the summary (the next turn gets 409). A failed summary is retried after the next turn

### 8.5 Checks

Check outcomes are decided by code, not by the LLM (`pkg/game/checks.go`).

- For an action that needs a roll, the GM ends its narration with `判定: 誰/判定名/基準値N/目標値M` lines (one per check). These lines are taken out of the narration and rolled. GM appeals roll nothing
- An NPC asks for a roll with the optional YAML field `check: 判定名 基準値N 目標値M`
- The 基準値 / 目標値 labels may be left out (`魔物知識 10 12` and `命中力判定/10/10` are read too), since Gemma often drops them. A 判定 line without numbers (such as `自動成功`) is removed from the narration and not rolled
- When an NPC's `check` cannot be read, the action is kept, the check is not rolled, and a warning is recorded (no re-think)
- The code rolls 2d6; dice + base ≥ target succeeds. A natural 12 always succeeds, a natural 2 always fails
- Results are passed to later stages (NPCs, Support) as `【判定結果（システムが振ったサイコロ）】` and to the UI as `check` events, shown as dice chips. The next turn's GM narrates according to the recent turns' results

---

## 9. Frontend Screen Layout

The UI uses the theme shared with the MLX screens (`ui/src/styles/theme.css`, `ui.css`), with dark mode and a 375px-wide layout. The header shows the status of the LLM, the embedding model and the image studio (`GET /api/system`), and a banner asks to start whatever is down.

| Screen | Nav Label | Description |
|--------|-----------|-------------|
| **Start** | セッション (no session open) | 続きから (recent sessions, which can also be deleted) and 新しく (create with a title and quest) |
| **Session** | セッション | Chat log (player, GM and NPC bubbles), live per-stage progress and streamed text, dice / rule / re-think / skipped chips, ⏹ stop, ↻ redo, ✎ edit the input and redo, ⟲ back to before this turn, action / GM-appeal input. A side panel (a drawer on phones) with party HP/MP bars, the 3-lane formation, the PC, the quest, the summary and checkpoints |
| **Quests** | クエスト | Quest list with party-level eligibility, accept (makes it the open session's quest) and complete |
| **Party** | パーティ | This session's NPCs (add up to 3 from personas, remove, details and editing) and PCs (create, edit, switch the active one). NPC editing changes values while keeping unknown YAML keys |
| **Rules** | ルール | The same search the game uses, a paged and filtered list, adding house rules and corrections, enable/disable toggle. Shows each chunk's part > chapter > heading and pages |

### DEV Mode
Activated via the "DEV" button in the header. It enables direct YAML editing in the Party screen's NPC details (removing keys loses the persona, so take care).

### Development
- `OMNI_API=host:port npx vite` points the dev server at another orchestrator (default `localhost:8080`)
- Opening the UI with `?mock=1` replays a mock turn without an orchestrator

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

position:                   # Initial placement
  x: 2
  y: 5                      # 0-2=enemy rear (enemy) / 3-5=front line (front) / 6-7=party rear (party)

equipment:                  # weapon / armor / shield / accessory go into the prefix
  weapon: Battle Axe +1
  armor: Chain Mail
  shield: Large Shield

skills:                     # names go into the prefix
  - name: Weapon Mastery (Axe/Hammer)
    description: +1 to attack rolls with axes and hammers

personality:                # Goes into the prefix (system message)
  tone: Gruff and short-tempered. Deeply loyal to companions.
  speech_style: Terse sentences. Uses declarative endings.
  values: Values strength and honesty.
  fears: Failing to protect his companions.
  quirks: Taps the axe handle three times before a fight.

backstory: |
  A former mercenary whose unit was wiped out; he has wandered ever since.

action_rules:
  - Never deliver a killing blow to a surrendering enemy
  - Prioritize defense when HP drops below 10
```

Only the keys listed in §8.4 reach the LLM. Other keys (`stats`, `fortune`, …) do not reach the LLM, but they survive editing in the Party screen. See `data/personas/example_gard.yaml` for the full format.

### NPC LLM Output Format (fixed YAML)

```yaml
name: Gard
action: Swings axe at enemy
dialogue: "Don't move!"
mp_cost: 0          # 0..current MP
hp_delta: 0         # Change to own HP, -current HP..0
new_lane: front     # Only when lane changes (enemy/front/party)
check: 命中力判定 基準値5 目標値12   # Only when the outcome depends on a roll (the code rolls)
```

---

## 11. Scene Images (MLX Image Studio)

### Connection
- Uses the MLX image studio mlx-studio-web (`IMAGES_URL`, default `http://127.0.0.1:11462`). The studio's Gemma picks the composition (tags, style, view) and the ComfyUI behind the studio draws it, so Omni-Master has no ComfyUI workflow of its own
- Both the studio and Gemma (mlx-serve) must be running. If they are not, turns simply get no image
- `IMAGE_EVERY=0` disables images (an empty `IMAGES_URL` falls back to the default, so it cannot be used to disable them)

### Timing
- On action turns (not GM appeals): turn 1, and turns whose number is a multiple of `IMAGE_EVERY` (default 3). The code decides when to draw
- Drawn asynchronously after the turn is finished (progress as `stage: image`). When done, an `image` event is sent and the image is attached to the turn's `image_url`, unless the turn was rewound meanwhile

### Image Retrieval Flow
1. `POST /api/studio/infer` with a request built from the last 4 summary sentences, the previous scene, the player action and this turn's GM narration (one narration alone often leaves out the place); poll the job until the composition is ready
2. `POST /api/studio/render`; poll the job until the image is ready (6 minutes in total at most)
3. Save it as `IMAGES_DIR/sessions/<session id>/<turn>.<ext>`, served by `GET /api/sessions/{id}/images/{file}`

---

## 12. RAG (Rule Search) Specification

### Chunk Creation
- Chunk the rulebook (OCR'd Markdown) with `build_rulebook.py` (`rulebook_chunker.py`)
  - Split by headings and paragraphs, never mid-sentence (target ~600 characters; paragraphs over 900 characters split at 「。」; tables split by rows every 900 characters with the header row repeated)
  - Ruby readings (「漢字（かな）」, and a kana word followed by the same kana in parentheses), full-width alphanumerics, page numbers, the table of contents, layout elements, blank form tables, the sample characters' filled sheets and similar noise are removed deterministically
  - Book pages are mapped to part / chapter / item via the table of contents, and each chunk gets `section` (part > chapter > item > heading), `page_from` / `page_to` and `tag`
  - The sample scenario 「蛮族を駆逐せよ」 is imported with `tag='scenario'` and `enabled=0`, so the GM does not pull the scenario's story into ordinary sessions
  - `--apply` backs up the DB to `data/backup/`, then replaces the rulebook rows (`source` = `rulebook1` or empty). House rules and corrections are kept
- Short house-rule or correction text files can be added with `ocr_rulebook.py` (500-character chunks; not for the rulebook itself)
- Vectorize via `embed_chunks.py` (mlx-serve `bge-m3-q8`, 1024-dim)
  - For `rag_chunks`, when `section` is set the embedded text is the section, a newline, then the body (the same as Go's `rag.EmbedInput`), so a query about a topic also matches chunks whose body never names it
  - If the embedding server rejects the input as too large (bge-m3-q8 takes up to 2048 tokens per input; normal chunks fit), the text is shortened and retried
- Rules added or edited through the UI / API (`POST /api/rules`, `PATCH /api/rules/{id}`) are embedded by the orchestrator immediately
- When the embedding model changes, the vector dimension changes too, so re-embed every chunk (`embed_chunks.py --reembed-all`)
- `optimize_chunks.py` (LLM rewrite into `rag_chunks_opt`) is optional and legacy; normally not needed for the rulebook

### Search Algorithm
1. Convert input text to an embedding vector
2. Candidates are rows with `enabled=1` and an embedding, except chunks that an enabled correction or house rule overrides via `overrides_id` (only the fix is used; a disabled correction does not hide the original)
3. Rank by cosine similarity plus a priority bonus (`priority` × 0.0025: house rules +0.05, corrections +0.025), so a house rule or correction beats a rulebook passage that is only slightly more similar
4. If `rag_chunks_opt` exists for a chunk, use its text and vector instead
5. If the input names a term in the term index (below), the chunks defining it (up to 2) come first; the rest is filled in the order above
6. Format the top-K results and inject them into the prompt

### Term Index
Names of feats, spells, items, monsters and the like are looked up by reading rather than by embedding similarity (`pkg/rag/terms.go`).

- Defining chunks: chunks whose `section` ends in a term in 《》【】〈〉. Ruby, rank numerals (Ⅰ, Ⅱ, …), middle dots and the like are stripped from the term for the index
- Matching is by reading (katakana). Words are put back to their base form with kagome (IPA dictionary) before taking the reading, so 「庇う」, 「かばう」 and 「庇います」 all match 《かばうⅠ》
- Terms whose reading is shorter than 3 characters are not indexed (short readings would match everywhere)
- The index is built from the enabled rules on the first search, and rebuilt when rules are added, edited or deleted through the UI / API (after changing the DB directly, e.g. `build_rulebook.py --apply`, restart the orchestrator to rebuild it)
- Chunks picked by term carry `matched_term` and show it on the UI's rule chips

`GET /api/rules/search?q=&k=` returns the same search the game uses, so the Rules screen can be used to check what a query finds.

### Stages That Use It
| Stage | Query | Count | Purpose |
|-------|-------|-------|---------|
| GM | Player input | 5 | Reference for narration and rulings |
| Support | NPCs' actions (`action` and `check`) | 3 | Follow values given in the rules (e.g. spell MP cost) when confirming HP/MP |
| NPC | — | — | No rules are given |

If the search fails or finds nothing, the stage continues without rules. The rules given to a stage reach the UI as `rules` events and in the turn's `rules_used`.

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
