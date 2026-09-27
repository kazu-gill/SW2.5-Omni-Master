#Requires -Version 7.0
# Omni-Master 起動・停止スクリプトの共通関数 (Start-Omni.ps1 / Stop-Omni.ps1 から Import-Module する)
Set-StrictMode -Version Latest

$script:Root = Split-Path -Parent $PSScriptRoot

function Get-OmniRoot { $script:Root }

function Read-OmniConfig {
    <# config.env を読み、OS 環境変数で上書きした設定をハッシュテーブルで返す (Go の config.Load と同じ優先順位) #>
    $path = Join-Path $script:Root 'config.env'
    if (-not (Test-Path $path)) {
        throw "設定ファイルが見つかりません: $path (config.env.example をコピーして作成してください)"
    }
    $cfg = @{
        ADDR           = ':8080'
        LLM_URL        = 'http://127.0.0.1:11441'
        LLM_MODEL_ID   = 'gemma4-31b-it-heretic-4bit-mtp'
        EMBED_URL      = 'http://127.0.0.1:11455'
        EMBED_MODEL_ID = 'bge-m3-q8'
        COMFY_URL      = ''
    }
    foreach ($line in Get-Content $path) {
        $t = $line.Trim()
        if ($t -eq '' -or $t.StartsWith('#')) { continue }
        $k, $v = $t -split '=', 2
        if ($null -ne $v -and $v.Trim() -ne '') { $cfg[$k.Trim()] = $v.Trim() }
    }
    foreach ($k in @($cfg.Keys)) {
        $envValue = [Environment]::GetEnvironmentVariable($k)
        if ($envValue) { $cfg[$k] = $envValue }
    }
    $cfg
}

function Get-AddrPort {
    <# ":8080" や "0.0.0.0:8080" からポート番号を取り出す #>
    param([Parameter(Mandatory)][string]$Addr)
    [int]($Addr -split ':')[-1]
}

function Test-HttpHealth {
    param([Parameter(Mandatory)][string]$BaseUrl)
    try {
        $r = Invoke-WebRequest -Uri "$BaseUrl/health" -TimeoutSec 3 -SkipHttpErrorCheck
        $r.StatusCode -eq 200
    } catch {
        $false
    }
}

function Get-PortPids {
    param([Parameter(Mandatory)][int]$Port)
    # 呼び出し側は @(Get-PortPids ...) で受ける (空配列は関数の戻り値で $null になる)
    & lsof -nP "-iTCP:$Port" -sTCP:LISTEN -t 2>$null | ForEach-Object { [int]$_ }
}

function Stop-Port {
    <# ポートを LISTEN しているプロセスを止める (volta → npm → vite のような多段起動でも確実に止まる) #>
    param([Parameter(Mandatory)][int]$Port, [Parameter(Mandatory)][string]$Label)
    $pids = @(Get-PortPids $Port)
    if ($pids.Count -eq 0) {
        Write-Host "  スキップ: $Label (:$Port) — 起動していません"
        return
    }
    & kill -TERM @pids 2>$null
    Start-Sleep -Seconds 1
    $survivors = @(Get-PortPids $Port)
    if ($survivors.Count -gt 0) { & kill -KILL @survivors 2>$null }
    Write-Host "  停止: $Label (:$Port)"
}

Export-ModuleMember -Function Get-OmniRoot, Read-OmniConfig, Get-AddrPort, Test-HttpHealth, Get-PortPids, Stop-Port
