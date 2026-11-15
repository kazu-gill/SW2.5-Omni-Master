#!/usr/bin/env pwsh
#Requires -Version 7.0
<#
.SYNOPSIS
    Omni-Master (Go Orchestrator + Vite UI) を停止する。mlx-serve のプロファイルは止めない。

.EXAMPLE
    pwsh scripts/Stop-Omni.ps1
#>

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
Import-Module (Join-Path $PSScriptRoot 'OmniMaster.Common.psm1') -Force

$root = Get-OmniRoot
$pidFile = Join-Path $root '.pids'
$cfg = Read-OmniConfig

Write-Host '[stop] サービスを停止します...'
Stop-Port -Port 5173 -Label 'UI (Vite)'
Stop-Port -Port (Get-AddrPort $cfg.ADDR) -Label 'orchestrator'

# ── .pids に残った PID も念のため停止 ────────────────────────────────────────
if (Test-Path $pidFile) {
    foreach ($line in Get-Content $pidFile) {
        $procId, $name = $line -split ' ', 2
        if (Get-Process -Id ([int]$procId) -ErrorAction SilentlyContinue) {
            & kill -TERM $procId 2>$null
            Write-Host "  停止(PID): $name ($procId)"
        }
    }
    Remove-Item -Force $pidFile
}

Write-Host '[stop] 完了 (mlx-serve のプロファイルは起動したまま)'
