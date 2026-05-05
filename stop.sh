#!/usr/bin/env bash
# SW2.5 Omni-Master — 全サービス停止スクリプト
set -uo pipefail

ROOT="$(cd "$(dirname "$0")" && pwd)"
PID_FILE="$ROOT/.pids"

# ── ポートでプロセスを停止するヘルパー ───────────────────────────────────────
# PID チェーンではなく「ポートを LISTEN しているプロセス」を直接停止する。
# これにより volta-shim → npm → vite のような多段起動でも確実に停止できる。
kill_port() {
    local port="$1"
    local label="$2"
    local pids
    pids=$(lsof -ti:"$port" 2>/dev/null || true)
    if [ -n "$pids" ]; then
        echo "$pids" | xargs kill -TERM 2>/dev/null || true
        # SIGTERM で終了しない場合は 1 秒後に SIGKILL
        sleep 1
        local survivors
        survivors=$(lsof -ti:"$port" 2>/dev/null || true)
        if [ -n "$survivors" ]; then
            echo "$survivors" | xargs kill -KILL 2>/dev/null || true
        fi
        echo "  停止: $label (:$port)"
    else
        echo "  スキップ: $label (:$port) — 起動していません"
    fi
}

echo "[stop] サービスを停止します..."

# UI (Vite dev server)
kill_port 5173 "UI (Vite)"

# Go オーケストレーター
kill_port 8080 "orchestrator"

# llama-server 6本
kill_port 11430 "gm"
kill_port 11431 "support"
kill_port 11432 "npc-a"
kill_port 11433 "npc-b"
kill_port 11434 "npc-c"
kill_port 11435 "embed"

# ── .pids に残った PID も念のため停止 ────────────────────────────────────────
if [ -f "$PID_FILE" ]; then
    while IFS=" " read -r pid name; do
        if kill -0 "$pid" 2>/dev/null; then
            kill -TERM "$pid" 2>/dev/null || true
            echo "  停止(PID): $name ($pid)"
        fi
    done < "$PID_FILE"
    rm -f "$PID_FILE"
fi

echo "[stop] 完了"
