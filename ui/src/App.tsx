import { useCallback, useEffect, useMemo, useState } from 'react'
import { errorText, get } from './api'
import Header from './components/Header'
import { useSessionGame } from './hooks/useSessionGame'
import { useSystemStatus } from './hooks/useSystemStatus'
import type { PlayerCharacterEntry, QuestEntry, ViewName } from './types'
import { parsePCSheet } from './utils/format'
import { npcLevel } from './utils/npcYaml'
import { loadFlag, loadSessionId, saveFlag, saveSessionId } from './utils/storage'
import PartyView from './views/party/PartyView'
import QuestView from './views/QuestView'
import RulesView from './views/rules/RulesView'
import SessionView from './views/session/SessionView'
import StartView from './views/StartView'

const LLM_HINT = 'LLM が起動していません。MLX ダッシュボードで gemma4-31b-it-heretic-4bit-mtp を起動してください'

/** 一覧を読む小さな hook (クエスト・PC) */
function useList<T>(path: string) {
  const [items, setItems] = useState<T[]>([])
  const [err, setErr] = useState('')
  const load = useCallback(() => get<T[]>(path).then(
    (l) => { setItems(l ?? []); setErr('') },
    (e) => { setErr(errorText(e)) },
  ), [path])
  useEffect(() => { void load() }, [load])
  const reload = load
  return { items, err, reload }
}

export default function App() {
  const [view, setView] = useState<ViewName>('session')
  const [sessionId, setSessionId] = useState<number | null>(() => loadSessionId())
  const [devMode, setDevModeState] = useState(() => loadFlag('sw25.dev'))
  const system = useSystemStatus()
  const quests = useList<QuestEntry>('/api/quests')
  const pcs = useList<PlayerCharacterEntry>('/api/player-characters')

  const openSession = (id: number | null) => {
    setSessionId(id)
    saveSessionId(id)
    setView('session')
  }
  const setDevMode = (on: boolean) => { setDevModeState(on); saveFlag('sw25.dev', on) }

  return (
    <div className="ui-page app">
      <Header view={view} setView={setView} system={system} devMode={devMode} setDevMode={setDevMode} />
      {system.error && <div className="banner">オーケストレーターに届きません: {system.error}</div>}
      {system.status && !system.status.llm.ok && <div className="banner">{LLM_HINT}</div>}
      {system.status && !system.status.embed.ok && (
        <div className="banner soft">埋め込みモデルが止まっています。ルール検索が文字の一致だけになります。MLX ダッシュボードで埋め込みモデルを起動してください。</div>
      )}
      {devMode && <div className="banner soft">DEV: YAML の直接編集などが出ています。キーを消すとペルソナが失われるので注意してください。</div>}
      <Game
        key={sessionId ?? 0}
        sessionId={sessionId}
        view={view}
        devMode={devMode}
        quests={quests}
        pcs={pcs}
        openSession={openSession}
      />
    </div>
  )
}

interface GameProps {
  sessionId: number | null
  view: ViewName
  devMode: boolean
  quests: ReturnType<typeof useList<QuestEntry>>
  pcs: ReturnType<typeof useList<PlayerCharacterEntry>>
  openSession: (id: number | null) => void
}

/** 選んだセッションごとに作り直す (key)。セッションの状態と進行はここで持つ */
function Game({ sessionId, view, devMode, quests, pcs, openSession }: GameProps) {
  const game = useSessionGame(sessionId)
  const state = game.state
  const busy = (!!game.live.live && !game.live.live.error) || game.live.busy

  const partyAvgLevel = useMemo(() => {
    const levels: number[] = []
    const active = pcs.items.find((p) => p.is_active)
    const pc = parsePCSheet(active ?? null)
    if (pc?.adventurerLevel) levels.push(pc.adventurerLevel)
    for (const s of state?.npc_sheets ?? []) {
      const lv = npcLevel(s.yaml_blob)
      if (lv) levels.push(lv)
    }
    if (!levels.length) return null
    return Math.round((levels.reduce((a, b) => a + b, 0) / levels.length) * 10) / 10
  }, [pcs.items, state?.npc_sheets])

  const reloadAll = async () => { await Promise.all([game.reload(), quests.reload()]) }
  const reloadPCs = async () => { await Promise.all([pcs.reload(), game.reload()]) }

  return (
    <main className={view === 'session' && sessionId !== null ? 'app-main full' : 'app-main'}>
      {view === 'session' && (sessionId === null
        ? <StartView quests={quests.items} onOpen={openSession} />
        : <SessionView game={game} onLeave={() => openSession(null)} />)}
      {view === 'quest' && (
        <QuestView
          quests={quests.items}
          questsErr={quests.err}
          partyAvgLevel={partyAvgLevel}
          sessionId={sessionId}
          sessionQuestId={state?.session.quest_id ?? null}
          onChanged={reloadAll}
        />
      )}
      {view === 'party' && (
        <PartyView
          state={state}
          busy={busy}
          devMode={devMode}
          onSessionChanged={game.reload}
          pcs={pcs.items}
          pcsErr={pcs.err}
          onPCsChanged={reloadPCs}
        />
      )}
      {view === 'rules' && <RulesView />}
    </main>
  )
}
