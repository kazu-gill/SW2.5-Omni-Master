# SW2.5 Omni-Master — System Specification

> Version: 1.0 / Language: English

---

## 1. System Overview

**SW2.5 Omni-Master** is an AI-driven orchestration system for the TRPG "Sword World 2.5". Independent local LLMs handle the Game Master (GM) and NPC roles, generating real-time responses to player action declarations. Rule consistency is maintained by retrieving relevant rulebook passages via vector search (RAG), and optional integration with ComfyUI enables automated scene image generation.

### Key Features

| Feature | Description |
|---------|-------------|
| **Autonomous GM** | LLM acts as GM, generating situational narration and rulings for each player action |
| **NPC Multi-Agent** | Each NPC runs on its own LLM instance with an individual personality |
| **RAG Rule Lookup** | Imported rulebook is searched by embedding vector and injected into LLM prompts |
| **Image Generation** | ComfyUI (Stable Diffusion) generates scene images and NPC portraits |
| **Real-time UI** | WebSocket broadcasts turn results to all connected clients |
| **Checkpoints** | Session state is auto-saved and restorable (up to 10 per session) |
| **Batch Maintenance** | Scenario pre-generation and chunk optimization run offline |

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
│                      │ HTTP                         │
└──────────────────────┼──────────────────────────────┘
                       │
        ┌──────────────┼──────────────────┐
        ▼              ▼                  ▼
  ┌───────────┐  ┌───────────┐   ┌──────────────┐
  │ GM LLM    │  │ NPC LLMs  │   │ Embed LLM    │
  │ :11430    │  │ A/B/C     │   │ :11435       │
  │(Gemma-4B) │  │:11432-34  │   │(nomic-embed) │
  └───────────┘  └───────────┘   └──────────────┘
        ▲              ▲
        │ RAG Context  │
  ┌─────┴──────────────┴───────┐
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
| **Frontend** | React 18 + TypeScript + Vite | Game UI |
| **Backend** | Go 1.22+ | REST API and game engine |
| **Database** | SQLite (modernc/sqlite) | All persistent data |
| **LLM Runtime** | llama-server (llama.cpp) | Local LLM inference |
| **GM Model** | Gemma-4-E4B (Q4_K_M) | GM role |
| **NPC Model** | Gemma-4-E2B (Q4_K_P) | NPC roles and validation |
| **Embed Model** | nomic-embed-text-v1.5 (Q4_K_M) | RAG vector generation |
| **Image Gen** | ComfyUI + Stable Diffusion | Scene and portrait images |
| **Batch Tools** | Python 3.11+ + uv | Rulebook processing, scenario generation |

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
│       │   ├── validate.go     # Action validation
│       │   └── comfyui.go      # Image generation client
│       ├── llm/                # LLM HTTP client
│       └── rag/                # Vector search engine
├── ui/                         # React frontend
│   └── src/
│       ├── App.tsx             # Root, navigation
│       ├── views/              # Screen components
│       ├── hooks/              # useGameSocket.ts
│       └── utils/              # questEligibility.ts
├── ai-agents/                  # Python batch tools
│   ├── ocr_rulebook.py         # Rulebook importer
│   ├── embed_chunks.py         # Embedding generator
│   ├── optimize_chunks.py      # Chunk optimizer
│   └── generate_scenarios.py   # Scenario batch generator
├── data/
│   ├── omni.db                 # SQLite database
│   ├── personas/               # NPC persona YAML files
│   ├── rulebook/               # Source rulebook files
│   └── images/                 # Generated images
│       ├── backgrounds/        # Shared background images
│       └── scenarios/{id}/     # Per-scenario images
├── docs/                       # Documentation
├── logs/                       # Service logs
├── config.env                  # Configuration file
├── setup.sh                    # DB initialization / reset
├── start.sh                    # Start all services
└── stop.sh                     # Stop all services
```

---

## 5. Configuration (config.env)

Values in `config.env` are loaded by `start.sh` and the Go server at startup. Priority: OS environment variable > config.env > built-in default.

| Key | Default | Description |
|-----|---------|-------------|
| `DB_PATH` | `data/omni.db` | SQLite database file path |
| `ADDR` | `:8080` | Go server listen address |
| `COMFY_URL` | `""` (empty = disabled) | ComfyUI endpoint URL |
| `LLAMA_SERVER` | `llama-server` | llama-server binary name or path |
| `MODEL_GM` | (required) | Absolute path to GM LLM model file (.gguf) |
| `MODEL_NPC` | (required) | Absolute path to NPC/Support LLM model file |
| `MODEL_EMBED` | (required) | Absolute path to embedding model file |
| `GM_URL` | `http://localhost:11430` | GM LLM endpoint |
| `SUPPORT_URL` | `http://localhost:11431` | Validation LLM endpoint |
| `NPC_A_URL` | `http://localhost:11432` | NPC-A LLM endpoint |
| `NPC_B_URL` | `http://localhost:11433` | NPC-B LLM endpoint |
| `NPC_C_URL` | `http://localhost:11434` | NPC-C LLM endpoint |
| `EMBED_URL` | `http://localhost:11435` | Embedding LLM endpoint |
| `PERSONAS_DIR` | `data/personas` | Directory containing NPC persona YAML files |

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
Rulebook text split into searchable chunks.

| Column | Type | Description |
|--------|------|-------------|
| `source_type` | TEXT | `rulebook` / `correction` / `houserule` |
| `priority` | INTEGER | `20`=houserule / `10`=correction / `0`=rulebook |
| `tag` | TEXT | `combat` / `magic` / `item` / `character` / `world` / `rule` / `status` / `general` |
| `text` | TEXT | Chunk text |
| `enabled` | INTEGER | 1=active for RAG / 0=excluded |
| `overrides_id` | INTEGER FK | ID of the chunk this corrects |
| `embedding` | BLOB | float32 vector (little-endian) |
| `opt_status` | TEXT | `NULL`=unprocessed / `done` / `failed` / `skip` |

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
| `GET` | `/api/rules` | List chunks (filter: `?source_type=`) |
| `POST` | `/api/rules` | Add houserule or correction |
| `PATCH` | `/api/rules/{id}` | Update chunk |
| `DELETE` | `/api/rules/{id}` | Delete chunk |

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

```
Player → POST /api/turn
                │
       ┌────────▼─────────┐
       │  RAG Search      │ ← Cosine similarity on rag_chunks
       │  (extract rules) │   Priority: houserule > correction > rulebook
       └────────┬─────────┘
                │ Context injection
       ┌────────▼─────────┐
       │  GM LLM :11430   │ ← Gemma-4B
       │  Generate scene  │   Outputs narration + NPC action suggestions (YAML)
       └────────┬─────────┘
                │ Parallel execution
     ┌──────────┼──────────┐
     ▼          ▼          ▼
NPC-A :11432  NPC-B :11433  NPC-C :11434
Individual actions based on persona
     └──────────┼──────────┘
                │ All NPC actions
       ┌────────▼─────────┐
       │  Support :11431  │ ← Rule validation
       │  Validate        │   Compute HP/MP deltas
       └────────┬─────────┘
                │
       ┌────────▼─────────┐
       │  DB Update       │ ← Update npc_sheets HP/MP
       │  Log & Snapshot  │   Append session_logs
       │                  │   Save checkpoint (auto)
       └────────┬─────────┘
                │ WebSocket broadcast
       ┌────────▼─────────┐
       │  ComfyUI Image   │ ← Optional, async
       │  Generate/serve  │   Broadcast ImageUpdate separately
       └──────────────────┘
```

---

## 9. Frontend Screen Layout

| Screen | Nav Label | Description |
|--------|-----------|-------------|
| **INFORMATION** | `start` | Pre-session: NPC selection, PC check, checkpoint restore |
| **SESSION** | `session` | Active gameplay: chat log, action input, battle map, quest acceptance |
| **QUEST BOARD** | `quest` | Quest list with party-level eligibility filter |
| **PARTY** | `npc` | NPC stats, portrait album, attribute editing |
| **RULES** | `rules` | Rulebook search, houserule entry, correction management |

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
port: 11432                 # llama-server port (11432–11434)

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
- Import rulebook (PDF/Markdown) via `ocr_rulebook.py`
- Chunk size: ~500 characters (split at section boundaries)
- Vectorize via `embed_chunks.py` (nomic-embed-text-v1.5)

### Search Algorithm
1. Convert input text to an embedding vector
2. Compute cosine similarity against all `rag_chunks` rows
3. Sort by similarity; ties broken by `priority` (houserule first)
4. If `rag_chunks_opt` exists for a chunk, use its text and vector instead
5. Inject top-K results into GM/NPC LLM prompts

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
