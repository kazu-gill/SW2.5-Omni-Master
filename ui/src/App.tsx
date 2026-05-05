import { useState, useEffect, useCallback, useMemo } from 'react'
import './App.css'
import { useGameSocket } from './hooks/useGameSocket'
import type { ViewName, NPCSheet, Checkpoint, QuestEntry, PlayerCharacterEntry, PCSheet } from './types'
import SessionStartView from './views/SessionStartView'
import SessionView from './views/SessionView'
import QuestBoardView from './views/QuestBoardView'
import NPCView from './views/NPCView'
import RulesView from './views/RulesView'

const SESSION_ID = 1

export default function App() {
  const [view, setView] = useState<ViewName>('start')
  const [npcSheets, setNpcSheets] = useState<NPCSheet[]>([])
  const [checkpoints, setCheckpoints] = useState<Checkpoint[]>([])
  const [quests, setQuests] = useState<QuestEntry[]>([])
  const [pcEntries, setPcEntries] = useState<PlayerCharacterEntry[]>([])
  const [devMode, setDevMode] = useState(false)
  const { turns, status, removeTurn, resetTurns } = useGameSocket()

  const loadSession = useCallback(async () => {
    try {
      const res = await fetch(`/api/session/${SESSION_ID}`, { signal: AbortSignal.timeout(3000) })
      if (!res.ok) return
      const data = await res.json()
      setNpcSheets(data.npc_sheets ?? [])
      setCheckpoints(data.checkpoints ?? [])
      if (Array.isArray(data.turn_results) && data.turn_results.length > 0) {
        resetTurns(data.turn_results)
      }
    } catch { /* backend offline */ }
  }, [resetTurns])

  const loadQuests = useCallback(async () => {
    try {
      const res = await fetch('/api/quests', { signal: AbortSignal.timeout(3000) })
      if (res.ok) setQuests(await res.json())
    } catch { /* offline */ }
  }, [])

  const loadPCs = useCallback(async () => {
    try {
      const res = await fetch('/api/player-characters', { signal: AbortSignal.timeout(3000) })
      if (res.ok) setPcEntries(await res.json())
    } catch { /* offline */ }
  }, [])

  useEffect(() => { loadSession(); loadQuests(); loadPCs() }, [loadSession, loadQuests, loadPCs])

  // Party average adventurer level: active PC + NPC lv from YAML blobs
  const partyAvgLevel = useMemo(() => {
    const levels: number[] = []
    const activePC = pcEntries.find((p) => p.is_active)
    if (activePC) {
      try {
        const sheet = JSON.parse(activePC.json_blob) as PCSheet
        if (sheet.adventurerLevel > 0) levels.push(sheet.adventurerLevel)
      } catch { /* invalid json */ }
    }
    for (const npc of npcSheets) {
      const m = npc.YAMLBlob.match(/\blv:\s*(\d+)/)
      if (m) levels.push(parseInt(m[1], 10))
    }
    if (levels.length === 0) return null
    return Math.round((levels.reduce((a, b) => a + b, 0) / levels.length) * 10) / 10
  }, [pcEntries, npcSheets])

  async function restoreCheckpoint(turn: number) {
    try {
      const res = await fetch(`/api/checkpoint/restore/${turn}?session_id=${SESSION_ID}`, {
        signal: AbortSignal.timeout(5000),
      })
      if (res.ok) {
        const restored = await res.json()
        if (Array.isArray(restored)) resetTurns(restored)
      }
    } catch { /* ignore */ }
    await loadSession()
    setView('session')
  }

  const dotClass = status === 'connected' ? 'online' : status === 'connecting' ? 'connecting' : 'offline'
  const dotLabel = status === 'connected' ? '接続済み' : status === 'connecting' ? '接続中...' : '切断'

  const NAV: { id: ViewName; label: string }[] = [
    { id: 'start',   label: 'INFORMATION' },
    { id: 'session', label: 'SESSION' },
    { id: 'quest',   label: 'QUEST BOARD' },
    { id: 'npc',     label: 'PARTY' },
    { id: 'rules',   label: 'RULES' },
  ]

  return (
    <>
      <header id="app-header">
        <div className="header-title">
          <span className="rune">⚜</span>
          SW2.5 OMNI-MASTER
        </div>

        <nav className="header-nav">
          {NAV.map((n) => (
            <button
              key={n.id}
              className={`nav-btn ${view === n.id ? 'active' : ''}`}
              onClick={() => setView(n.id)}
            >
              {n.label}
            </button>
          ))}
        </nav>

        <div className="header-status">
          <span><span className={`status-dot ${dotClass}`} />{dotLabel}</span>
          <span style={{ color: 'var(--text-dim)' }}>Session #{SESSION_ID}</span>
          <button
            className={`dev-toggle-btn ${devMode ? 'active' : ''}`}
            onClick={() => setDevMode((v) => !v)}
          >
            <span className="dev-dot" />
            {devMode ? 'DEV ON' : 'DEV MODE'}
          </button>
        </div>
      </header>

      {devMode && (
        <div className="dev-banner">
          ⚠ DEV MODE — 編集制限が解除されています。本番セッション中は注意してください。
        </div>
      )}

      <div id="view-root">
        {view === 'start' && (
          <SessionStartView
            npcSheets={npcSheets}
            checkpoints={checkpoints}
            onStart={() => setView('session')}
            onRestore={restoreCheckpoint}
            onSheetsChange={loadSession}
            devMode={devMode}
          />
        )}
        {view === 'session' && (
          <SessionView
            turns={turns}
            status={status}
            npcSheets={npcSheets}
            quests={quests}
            partyAvgLevel={partyAvgLevel}
            onSheetsChange={loadSession}
            devMode={devMode}
            onRemoveTurn={removeTurn}
            onQuestAccept={async (id) => {
              await fetch(`/api/quests/${id}/accept`, { method: 'PATCH' })
              await loadQuests()
            }}
          />
        )}
        {view === 'quest' && (
          <QuestBoardView quests={quests} partyAvgLevel={partyAvgLevel} />
        )}
        {view === 'npc' && (
          <NPCView npcSheets={npcSheets} devMode={devMode} onSheetsChange={loadSession} />
        )}
        {view === 'rules' && (
          <RulesView />
        )}
      </div>
    </>
  )
}
