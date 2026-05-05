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
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
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
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    source_type TEXT    NOT NULL, -- 'houserule'|'correction'|'rulebook'
    priority    INTEGER NOT NULL, -- 20|10|0
    text        TEXT    NOT NULL,
    embedding   BLOB    -- NULL until embedded
);

CREATE TABLE IF NOT EXISTS npc_portraits (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    npc_name   TEXT    NOT NULL,
    session_id INTEGER NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    file_path  TEXT    NOT NULL,
    prompt     TEXT    NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_session_logs_session_turn ON session_logs(session_id, turn);
CREATE INDEX IF NOT EXISTS idx_checkpoints_session_turn  ON checkpoints(session_id, turn);
CREATE INDEX IF NOT EXISTS idx_rag_chunks_priority       ON rag_chunks(priority DESC);
