#!/usr/bin/env bash
# SW2.5 Omni-Master — 全サービス起動スクリプト
# 終了: ./stop.sh
set -uo pipefail

ROOT="$(cd "$(dirname "$0")" && pwd)"
CONFIG="$ROOT/config.env"
LOG_DIR="$ROOT/logs"
PID_FILE="$ROOT/.pids"
BIN_DIR="$ROOT/.bin"

# ── config.env を読み込む ─────────────────────────────────────────────────────
if [ ! -f "$CONFIG" ]; then
    echo "[error] 設定ファイルが見つかりません: $CONFIG"
    exit 1
fi

while IFS="=" read -r key val; do
    case "$key" in
        ""|\#*) continue ;;
    esac
    key="${key%"${key##*[![:space:]]}"}"
    val="${val#"${val%%[![:space:]]*}"}"
    if [ -z "${!key+x}" ]; then
        export "$key=$val"
    fi
done < "$CONFIG"

LLAMA="${LLAMA_SERVER:-llama-server}"
DB_PATH="${DB_PATH:-$ROOT/data/omni.db}"

# ── 前回プロセスのクリーンアップ ──────────────────────────────────────────────
if [ -f "$PID_FILE" ]; then
    echo "[start] 前回の .pids を検出 — 先に stop.sh を実行してください"
    echo "        または: rm $PID_FILE"
    exit 1
fi

mkdir -p "$LOG_DIR" "$ROOT/data" "$BIN_DIR"
: > "$PID_FILE"

# llama ログファイルをあらかじめ作成（tail -f が存在しないファイルを参照しないよう）
touch "$LOG_DIR/gm.log" "$LOG_DIR/support.log" \
      "$LOG_DIR/npc-a.log" "$LOG_DIR/npc-b.log" "$LOG_DIR/npc-c.log" \
      "$LOG_DIR/embed.log"

# ── DB が未初期化なら作成 ─────────────────────────────────────────────────────
if [ ! -f "$DB_PATH" ]; then
    echo "[start] DB を初期化します: $DB_PATH"
    sqlite3 "$DB_PATH" < "$ROOT/orchestrator/pkg/db/schema.sql"
fi

# ── llama-server が見つかるか確認 ────────────────────────────────────────────
if ! command -v "$LLAMA" > /dev/null 2>&1; then
    echo "[WARN] llama-server が見つかりません (LLAMA_SERVER=$LLAMA)"
    echo "       モデルなしで起動します（LLM呼び出しは失敗します）"
    SKIP_LLAMA=1
else
    SKIP_LLAMA=0
fi

# ── llama-server 起動ヘルパー ─────────────────────────────────────────────────
start_llama() {
    local name="$1"
    local port="$2"
    local model="$3"
    local logfile="$LOG_DIR/${name}.log"

    if [ "$SKIP_LLAMA" -eq 1 ]; then
        echo "[skip] $name (llama-server なし)"
        return
    fi

    # モデルパスが未設定または存在しない場合はスキップ
    if [ -z "$model" ]; then
        echo "[skip] $name (MODEL パスが未設定)"
        return
    fi
    if [ ! -f "$model" ]; then
        echo "[WARN] $name のモデルファイルが見つかりません: $model"
        echo "       $name をスキップします"
        return
    fi

    echo "[start] $name → :$port"
    "$LLAMA" \
        --model "$model" \
        --port "$port" \
        --host 127.0.0.1 \
        --ctx-size 4096 \
        --n-gpu-layers 99 \
        >> "$logfile" 2>&1 &
    echo "$! $name" >> "$PID_FILE"
    echo "        ログ: $logfile"
}

# ── llama-server x6 (GM / Support / NPC x3 / Embed) ─────────────────────────
start_llama "gm"      11430 "${MODEL_GM:-}"
start_llama "support" 11431 "${MODEL_NPC:-}"
start_llama "npc-a"   11432 "${MODEL_NPC:-}"
start_llama "npc-b"   11433 "${MODEL_NPC:-}"
start_llama "npc-c"   11434 "${MODEL_NPC:-}"
start_llama "embed"   11435 "${MODEL_EMBED:-}"

if [ "$SKIP_LLAMA" -eq 0 ]; then
    echo "[start] llama-server 初期化待機 (10秒)..."
    sleep 10
fi

# ── Go オーケストレーター（ビルド → 実バイナリを起動）────────────────────────
# go run は内部でコンパイル後に子プロセスを起動するため、
# $! で取得できる PID がサブシェルのものになり stop.sh で停止できない。
# ビルド後にバイナリを直接起動することで PID を正しく追跡する。
echo "[start] orchestrator をビルド中..."
if ! (cd "$ROOT/orchestrator" && go build -o "$BIN_DIR/orchestrator" ./cmd/server/main.go) >> "$LOG_DIR/orchestrator.log" 2>&1; then
    echo "[error] orchestrator のビルドに失敗しました"
    echo "        ログ: $LOG_DIR/orchestrator.log"
    cat "$LOG_DIR/orchestrator.log" | tail -20
    rm -f "$PID_FILE"
    exit 1
fi

echo "[start] orchestrator → ${ADDR:-:8080}"
"$BIN_DIR/orchestrator" >> "$LOG_DIR/orchestrator.log" 2>&1 &
echo "$! orchestrator" >> "$PID_FILE"
echo "        ログ: $LOG_DIR/orchestrator.log"
sleep 2

# ── Vite UI dev server ────────────────────────────────────────────────────────
# exec でサブシェルを npm プロセスに置き換えることで、
# $! が Vite を管理する npm の実 PID を指すようにする。
echo "[start] UI dev server → :5173"
(
    cd "$ROOT/ui" || exit 1
    exec npm run dev -- --host
) >> "$LOG_DIR/ui.log" 2>&1 &
echo "$! ui" >> "$PID_FILE"
echo "        ログ: $LOG_DIR/ui.log"

# ── 起動完了 ──────────────────────────────────────────────────────────────────
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "  Omni-Master 起動完了"
echo "  UI      → http://localhost:5173"
echo "  API     → http://localhost${ADDR:-:8080}"
echo "  ComfyUI → ${COMFY_URL:-未設定}"
echo "  終了    → ./stop.sh"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

# ── ログを tail して全サービスの出力を表示 ────────────────────────────────────
tail -f \
    "$LOG_DIR/orchestrator.log" \
    "$LOG_DIR/ui.log" \
    "$LOG_DIR/gm.log" \
    "$LOG_DIR/support.log" \
    "$LOG_DIR/npc-a.log" \
    "$LOG_DIR/npc-b.log" \
    "$LOG_DIR/npc-c.log" \
    "$LOG_DIR/embed.log" \
    2>/dev/null
