#!/usr/bin/env bash
# SW2.5 Omni-Master — 初期構築スクリプト
#
# 実行タイミング:
#   - 初回セットアップ時
#   - データを完全リセットしたいとき
#
# 保持されるもの:
#   - data/personas/*.yaml    （ペルソナ定義ファイル）
#   - player_characters テーブル  （PCシート）
#   - rag_chunks テーブル     （インポート済みルールブックデータ）
#
# リセットされるもの:
#   - sessions / npc_sheets / session_logs / checkpoints
#   - quests
#
# 使い方:
#   ./setup.sh           # 通常リセット（ルールブックは保持）
#   ./setup.sh --full    # rag_chunks も含めて全消去

set -euo pipefail

ROOT="$(cd "$(dirname "$0")" && pwd)"
CONFIG="$ROOT/config.env"
SCHEMA="$ROOT/orchestrator/pkg/db/schema.sql"
FULL_RESET=0

# ── オプション解析 ────────────────────────────────────────────────────────────
for arg in "$@"; do
    case "$arg" in
        --full) FULL_RESET=1 ;;
        -h|--help)
            echo "使い方: $0 [--full]"
            echo "  --full   rag_chunks（ルールデータ）も含めて全消去"
            exit 0
            ;;
        *) echo "[warn] 不明なオプション: $arg" ;;
    esac
done

# ── config.env を読み込む ─────────────────────────────────────────────────────
if [ -f "$CONFIG" ]; then
    while IFS="=" read -r key val; do
        case "$key" in ""|\#*) continue ;; esac
        key="${key%"${key##*[![:space:]]}"}"
        val="${val#"${val%%[![:space:]]*}"}"
        [ -n "$key" ] && export "$key=${!key:-$val}" 2>/dev/null || true
    done < "$CONFIG"
fi

DB_PATH="${DB_PATH:-$ROOT/data/omni.db}"
mkdir -p "$(dirname "$DB_PATH")"

echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "  SW2.5 Omni-Master — 初期構築"
echo "  DB: $DB_PATH"
[ "$FULL_RESET" -eq 1 ] && echo "  モード: フルリセット（rag_chunks を含む）"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

# ── DB がなければスキーマを適用して作成 ──────────────────────────────────────
if [ ! -f "$DB_PATH" ]; then
    echo "[setup] DB を新規作成します..."
    sqlite3 "$DB_PATH" < "$SCHEMA"
    echo "        完了: $DB_PATH"
else
    echo "[setup] 既存 DB を検出しました"
    # スキーマの idempotent 部分を再適用（CREATE TABLE IF NOT EXISTS）
    sqlite3 "$DB_PATH" < "$SCHEMA" 2>/dev/null || true
fi

# ── カラム追加マイグレーション（ALTER TABLE はエラー無視） ───────────────────
echo "[setup] マイグレーションを適用..."
sqlite3 "$DB_PATH" << 'EOF'
ALTER TABLE rag_chunks ADD COLUMN tag TEXT NOT NULL DEFAULT '';
EOF
2>/dev/null || true
sqlite3 "$DB_PATH" << 'EOF'
ALTER TABLE rag_chunks ADD COLUMN enabled INTEGER NOT NULL DEFAULT 1;
EOF
2>/dev/null || true
sqlite3 "$DB_PATH" << 'EOF'
ALTER TABLE rag_chunks ADD COLUMN overrides_id INTEGER REFERENCES rag_chunks(id);
EOF
2>/dev/null || true

# ── データリセット ────────────────────────────────────────────────────────────
echo "[setup] データをリセット中..."
sqlite3 "$DB_PATH" << SQL
PRAGMA foreign_keys = ON;

-- セッション（カスケードで npc_sheets / session_logs / checkpoints も削除）
DELETE FROM sessions;

-- クエスト
DELETE FROM quests;

$([ "$FULL_RESET" -eq 1 ] && echo "-- ルールデータ（--full 指定時のみ）" && echo "DELETE FROM rag_chunks;")

-- session #1 を再作成（アプリが SESSION_ID = 1 を前提とする）
INSERT INTO sessions (id, quest_id) VALUES (1, '');
SQL

# ── 結果表示 ──────────────────────────────────────────────────────────────────
echo ""
echo "[setup] リセット後の件数:"
sqlite3 -column -header "$DB_PATH" << 'EOF'
SELECT
    'sessions'        AS テーブル, count(*) AS 件数 FROM sessions
UNION ALL SELECT 'npc_sheets',      count(*) FROM npc_sheets
UNION ALL SELECT 'session_logs',    count(*) FROM session_logs
UNION ALL SELECT 'checkpoints',     count(*) FROM checkpoints
UNION ALL SELECT 'quests',          count(*) FROM quests
UNION ALL SELECT 'rag_chunks',      count(*) FROM rag_chunks
UNION ALL SELECT 'player_characters', count(*) FROM player_characters;
EOF

# ── ルールブックインポートの案内 ─────────────────────────────────────────────
echo ""
RULEBOOK="$ROOT/data/rulebook/swordworld2.5_rulebook1.md"
if [ -f "$RULEBOOK" ]; then
    echo "[info] ルールブックが見つかりました。以下のコマンドでインポートできます:"
    echo "       uv run $ROOT/ai-agents/ocr_rulebook.py \\"
    echo "           $RULEBOOK \\"
    echo "           --db $DB_PATH"
else
    echo "[info] ルールブックのインポート:"
    echo "       uv run $ROOT/ai-agents/ocr_rulebook.py <file.md> --db $DB_PATH"
fi

# ── git フックのインストール ──────────────────────────────────────────────────
HOOK_SRC="$ROOT/hooks/pre-commit"
HOOK_DST="$ROOT/.git/hooks/pre-commit"
if [ -d "$ROOT/.git" ] && [ -f "$HOOK_SRC" ]; then
    ln -sf "$HOOK_SRC" "$HOOK_DST"
    echo "[setup] git フックをインストールしました: $HOOK_DST"
fi

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "  セットアップ完了。./start.sh で起動してください。"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
