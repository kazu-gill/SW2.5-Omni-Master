PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS sessions (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    quest_id   TEXT    NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS npc_sheets (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id INTEGER NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    name       TEXT    NOT NULL,
    hp         INTEGER NOT NULL DEFAULT 0,
    mp         INTEGER NOT NULL DEFAULT 0,
    position_x INTEGER NOT NULL DEFAULT 0,
    position_y INTEGER NOT NULL DEFAULT 0,
    yaml_blob  TEXT    NOT NULL DEFAULT '',
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(session_id, name)
);

CREATE TABLE IF NOT EXISTS session_logs (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id   INTEGER NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    turn         INTEGER NOT NULL,
    role         TEXT    NOT NULL, -- 'player','gm','npc','support'
    content      TEXT    NOT NULL,
    validated_at DATETIME
);

-- 直近10件のみ保持（削除はアプリ側で管理）
CREATE TABLE IF NOT EXISTS checkpoints (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id    INTEGER NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    turn          INTEGER NOT NULL,
    snapshot_json TEXT    NOT NULL,
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- embedding: float32をリトルエンディアンBLOBで保存
CREATE TABLE IF NOT EXISTS rag_chunks (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    source_type  TEXT    NOT NULL,           -- 'houserule'|'correction'|'rulebook'
    priority     INTEGER NOT NULL,           -- 20|10|0
    tag          TEXT    NOT NULL DEFAULT '', -- combat/magic/status/general など
    text         TEXT    NOT NULL,
    enabled      INTEGER NOT NULL DEFAULT 1, -- 0=無効（RAG検索対象外）
    overrides_id INTEGER REFERENCES rag_chunks(id), -- 訂正元チャンクID
    embedding    BLOB,                       -- NULL until embedded
    opt_status   TEXT DEFAULT NULL           -- NULL:未処理 / 'done' / 'failed' / 'skip'
);

-- LLM最適化済みチャンク（バッチメンテナンスで生成）
-- rag_chunks の各行に対応する最適化バージョン。
-- RAG検索は chunk_id が存在すればこちらのテキスト・埋め込みを優先使用する。
CREATE TABLE IF NOT EXISTS rag_chunks_opt (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    chunk_id     INTEGER NOT NULL UNIQUE REFERENCES rag_chunks(id) ON DELETE CASCADE,
    text         TEXT    NOT NULL,           -- 最適化済みテキスト
    tag          TEXT    NOT NULL DEFAULT '', -- LLMが付与したタグ
    summary      TEXT    NOT NULL DEFAULT '', -- 一行要約（30文字以内）
    embedding    BLOB,                       -- 最適化テキストの埋め込みベクトル
    model        TEXT    NOT NULL DEFAULT '', -- 使用したモデル名
    optimized_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_rag_chunks_opt_chunk ON rag_chunks_opt(chunk_id);

CREATE TABLE IF NOT EXISTS npc_portraits (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    npc_name   TEXT    NOT NULL,
    session_id INTEGER NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    file_path  TEXT    NOT NULL,
    prompt     TEXT    NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS player_characters (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT    NOT NULL DEFAULT 'PC',
    json_blob  TEXT    NOT NULL DEFAULT '{}',
    is_active  INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS quests (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    rank        TEXT NOT NULL DEFAULT 'C',
    title       TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    client      TEXT NOT NULL DEFAULT '',
    reward      TEXT NOT NULL DEFAULT '',
    target      TEXT NOT NULL DEFAULT '',
    level       TEXT NOT NULL DEFAULT '',
    tags        TEXT NOT NULL DEFAULT '',
    status      TEXT NOT NULL DEFAULT 'available',
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- ターン毎の完全な TurnResult JSON（セッションログ復元用）
CREATE TABLE IF NOT EXISTS turn_results (
    session_id  INTEGER NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    turn        INTEGER NOT NULL,
    result_json TEXT    NOT NULL,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (session_id, turn)
);

CREATE INDEX IF NOT EXISTS idx_session_logs_session_turn ON session_logs(session_id, turn);
CREATE INDEX IF NOT EXISTS idx_checkpoints_session_turn  ON checkpoints(session_id, turn);
CREATE INDEX IF NOT EXISTS idx_turn_results_session      ON turn_results(session_id, turn);
CREATE INDEX IF NOT EXISTS idx_rag_chunks_priority       ON rag_chunks(priority DESC);

-- バッチ生成シナリオ（セッション開始前にオフラインで生成済みのシナリオ素材）
CREATE TABLE IF NOT EXISTS scenarios (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    title            TEXT    NOT NULL DEFAULT '',
    rank             TEXT    NOT NULL DEFAULT 'C',          -- A/B/C/D/E
    summary          TEXT    NOT NULL DEFAULT '',
    description      TEXT    NOT NULL DEFAULT '',
    client           TEXT    NOT NULL DEFAULT '',
    reward           TEXT    NOT NULL DEFAULT '',
    target           TEXT    NOT NULL DEFAULT '',
    level            TEXT    NOT NULL DEFAULT '',
    locations_json   TEXT    NOT NULL DEFAULT '[]',         -- [{name, description, image_id}]
    enemies_json     TEXT    NOT NULL DEFAULT '[]',         -- [{name, lv, hp, mp, ...}]
    plot_hooks_json  TEXT    NOT NULL DEFAULT '[]',         -- [string, ...]
    events_json      TEXT    NOT NULL DEFAULT '[]',         -- [{title, description}]
    status           TEXT    NOT NULL DEFAULT 'draft',      -- draft/ready/used
    generated_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- シナリオに紐づく画像（scenario_id=NULLは共通背景）
CREATE TABLE IF NOT EXISTS scenario_images (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    scenario_id  INTEGER REFERENCES scenarios(id) ON DELETE CASCADE,
    category     TEXT    NOT NULL DEFAULT 'scene',   -- scene/portrait/background
    label        TEXT    NOT NULL DEFAULT '',
    file_path    TEXT    NOT NULL DEFAULT '',
    prompt_text  TEXT    NOT NULL DEFAULT '',
    width        INTEGER NOT NULL DEFAULT 768,
    height       INTEGER NOT NULL DEFAULT 512,
    generated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_scenarios_status        ON scenarios(status);
CREATE INDEX IF NOT EXISTS idx_scenario_images_scen    ON scenario_images(scenario_id);
CREATE INDEX IF NOT EXISTS idx_scenario_images_cat     ON scenario_images(category);
