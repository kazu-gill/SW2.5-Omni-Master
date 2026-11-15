#!/usr/bin/env pwsh
#Requires -Version 7.0
<#
.SYNOPSIS
    Omni-Master (Go Orchestrator + Vite UI) を起動する。

.DESCRIPTION
    LLM は mlx-serve (~/AI/MLX) が提供する。このスクリプトは mlx-serve を起動しない。
    config.env の LLM_URL / EMBED_URL が応答しない場合は、起動すべきプロファイルを表示して終了する。

.PARAMETER Follow
    起動後にログを追従表示する (Ctrl+C で表示だけ終了し、サービスは動き続ける)。

.EXAMPLE
    pwsh scripts/Start-Omni.ps1 -Follow
#>
param([switch]$Follow)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
Import-Module (Join-Path $PSScriptRoot 'OmniMaster.Common.psm1') -Force

$root = Get-OmniRoot
$pidFile = Join-Path $root '.pids'
$logDir = Join-Path $root 'logs'
$binDir = Join-Path $root '.bin'

$cfg = Read-OmniConfig
$apiPort = Get-AddrPort $cfg.ADDR

# ── 二重起動の防止 ────────────────────────────────────────────────────────────
if (Test-Path $pidFile) {
    Write-Host "[start] 前回の .pids を検出 — 先に scripts/Stop-Omni.ps1 を実行してください"
    exit 1
}

# ── mlx-serve のヘルス確認 (起動はしない) ─────────────────────────────────────
$missing = @()
foreach ($svc in @(
        @{ Url = $cfg.LLM_URL; Profile = $cfg.LLM_MODEL_ID },
        @{ Url = $cfg.EMBED_URL; Profile = $cfg.EMBED_MODEL_ID })) {
    if (Test-HttpHealth $svc.Url) {
        Write-Host "[check] mlx-serve $($svc.Profile) ($($svc.Url)): OK"
    } else {
        Write-Host "[check] mlx-serve $($svc.Profile) ($($svc.Url)): 応答なし"
        $missing += $svc.Profile
    }
}
if ($missing.Count -gt 0) {
    Write-Host ''
    Write-Host '[start] mlx-serve で次のプロファイルを起動してから、もう一度実行してください:'
    foreach ($p in $missing) {
        Write-Host "  pwsh ~/AI/MLX/scripts/Start-MlxServe.ps1 -Profile $p"
    }
    exit 1
}

New-Item -ItemType Directory -Force -Path $logDir, $binDir | Out-Null
$orchLog = Join-Path $logDir 'orchestrator.log'
$uiLog = Join-Path $logDir 'ui.log'

function Start-Logged {
    <#
    sh でバックグラウンド起動し、$! (nohup が exec した実プロセスの PID) を記録する。
    Start-Process -ArgumentList は要素をクォートせずに連結するため sh -c に使えない。
    #>
    param([string]$WorkDir, [string]$Command, [string]$LogFile, [string]$Name)
    # `cd && nohup ... &` だとリスト全体がサブシェルになり、pwsh の出力パイプを握ったまま残る
    $childPid = & /bin/sh -c "cd '$WorkDir' || exit 1; nohup $Command >> '$LogFile' 2>&1 < /dev/null & echo `$!"
    "$childPid $Name" | Add-Content -Path $pidFile
    Write-Host "        PID $childPid  ログ: $LogFile"
}

# ── Go Orchestrator (ビルド → バイナリを直接起動) ─────────────────────────────
Write-Host '[start] orchestrator をビルド中...'
Push-Location (Join-Path $root 'orchestrator')
try {
    $buildOut = & go build -o (Join-Path $binDir 'orchestrator') ./cmd/server 2>&1
    if ($LASTEXITCODE -ne 0) {
        Write-Host '[error] orchestrator のビルドに失敗しました'
        $buildOut | ForEach-Object { Write-Host "  $_" }
        exit 1
    }
} finally {
    Pop-Location
}

Write-Host "[start] orchestrator → $($cfg.ADDR)"
Start-Logged -WorkDir $root -Command "'$(Join-Path $binDir 'orchestrator')'" -LogFile $orchLog -Name 'orchestrator'

# ── Vite UI dev server ────────────────────────────────────────────────────────
Write-Host '[start] UI dev server → :5173'
Start-Logged -WorkDir (Join-Path $root 'ui') -Command 'npm run dev -- --host' -LogFile $uiLog -Name 'ui'

# ── 待ち受け確認 ──────────────────────────────────────────────────────────────
$deadline = (Get-Date).AddSeconds(30)
foreach ($port in @($apiPort, 5173)) {
    while (@(Get-PortPids $port).Count -eq 0 -and (Get-Date) -lt $deadline) { Start-Sleep -Milliseconds 500 }
    if (@(Get-PortPids $port).Count -eq 0) {
        Write-Host "[WARN] :$port が 30 秒以内に待ち受けを開始しませんでした。ログを確認してください"
    }
}

$comfy = if ($cfg.COMFY_URL) { $cfg.COMFY_URL } else { '未設定' }
Write-Host ''
Write-Host '━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━'
Write-Host '  Omni-Master 起動完了'
Write-Host '  UI      → http://localhost:5173'
Write-Host "  API     → http://localhost:$apiPort"
Write-Host "  LLM     → $($cfg.LLM_MODEL_ID) ($($cfg.LLM_URL))"
Write-Host "  Embed   → $($cfg.EMBED_MODEL_ID) ($($cfg.EMBED_URL))"
Write-Host "  ComfyUI → $comfy"
Write-Host '  終了    → pwsh scripts/Stop-Omni.ps1'
Write-Host '━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━'

if ($Follow) {
    & tail -f $orchLog $uiLog
}
