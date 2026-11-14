// セッション画面: 会話ログ (確定したターン + 進行中のターン)・入力欄・右の引き出し。
import { useState } from 'react'
import { errorText, patch, post } from '../../api'
import { useDialogs } from '../../dialogs/context'
import type { SessionGame } from '../../hooks/useSessionGame'
import { plannedStages } from '../../live/turnReducer'
import type { Lane, TurnMode, TurnResult } from '../../types'
import ChatLog from './ChatLog'
import Composer from './Composer'
import LiveTurnView from './LiveTurnView'
import SidePanel from './SidePanel'
import StoppedTurnView from './StoppedTurnView'
import TurnView from './TurnView'

interface Props {
  game: SessionGame
  onLeave: () => void
}

export default function SessionView({ game, onLeave }: Props) {
  const { state, live, dispatch, reload, patchState, socket } = game
  const ui = useDialogs()
  const [text, setText] = useState('')
  const [mode, setMode] = useState<TurnMode>('action')
  const [err, setErr] = useState('')
  const [jump, setJump] = useState(0)
  const [sideOpen, setSideOpen] = useState(false)
  const [working, setWorking] = useState(false)

  if (!state) {
    return (
      <div className="center-box">
        {game.loadError ? (
          <>
            <div className="err">セッションを読めませんでした: {game.loadError}</div>
            <div className="ui-row"><button onClick={() => void reload()}>もう一度読む</button><button onClick={onLeave}>セッション一覧へ</button></div>
          </>
        ) : <div className="ui-muted">読み込んでいます…</div>}
      </div>
    )
  }

  const sid = state.session.id
  const cur = live.live
  const running = !!cur && !cur.error
  // 止まったターンがあるあいだは、新しいターン・やり直し・巻き戻し・パーティの編集が 409 になる。
  // 続きの操作の最中 (cur が進行中) や、他のタブで進行中のときは出さない
  const stopped = !running && !live.busy ? state.stopped_turn ?? null : null
  const busy = running || live.busy
  // 止まったと分かったが stopped_turn がまだ state に無いあいだも送らせない
  const awaitingStopped = !!cur?.resumable && !stopped
  const locked = busy || !!stopped || awaitingStopped
  const npcNames = state.npc_sheets.map((s) => s.name)
  const blockedReason = live.summarizing
    ? '📝 要約を更新しています。終わると送れます'
    : live.busy ? '別のタブでターンが進行中です。終わると送れます'
      : stopped || awaitingStopped ? '止まったターンがあります。「続きから」か「取り消す」を先に選んでください'
      : socket !== 'open' ? '進行を受け取る接続をつないでいます… (送れますが、進み具合は再接続後にまとめて出ます)' : ''

  const begin = (input: string, m: TurnMode) => {
    setErr('')
    dispatch({ kind: 'begin', input, mode: m, stages: plannedStages(m, npcNames) })
    setJump((j) => j + 1)
  }

  const start = async (input: string, m: TurnMode) => {
    begin(input, m)
    try {
      const r = await post<{ turn: number }>(`/api/sessions/${sid}/turn`, { text: input, mode: m })
      dispatch({ kind: 'accepted', turn: r.turn })
    } catch (e) {
      dispatch({ kind: 'clear' })
      setText(input)
      setMode(m)
      setErr(`送れませんでした: ${errorText(e)}`)
    }
  }

  const send = () => {
    const v = text.trim()
    if (!v || busy) return
    setText('')
    void start(v, mode)
  }

  const stop = async () => {
    if (!cur) return
    try {
      // 止めたターンは途中まで残る。WS の error (resumable) と state の stopped_turn で「続きから」の画面になる
      await post(`/api/sessions/${sid}/cancel`)
      dispatch({ kind: 'halt', message: 'ターンを止めました。途中まで残っています' })
      setErr('')
    } catch (e) {
      setErr(`止められませんでした: ${errorText(e)}`)
    }
    void reload()
  }

  const resume = async () => {
    const st = stopped
    if (!st || working) return
    setWorking(true)
    setErr('')
    dispatch({ kind: 'resume', stopped: st, stages: plannedStages(st.mode, npcNames) })
    patchState((s) => ({ ...s, stopped_turn: null }))
    setJump((j) => j + 1)
    try {
      const r = await post<{ turn: number }>(`/api/sessions/${sid}/resume`)
      dispatch({ kind: 'accepted', turn: r.turn })
    } catch (e) {
      dispatch({ kind: 'clear' })
      setErr(`続けられませんでした: ${errorText(e)}`)
      await reload() // まだ止まったターンとして残っている (パーティが変わっていたら取り消すしかない)
    } finally {
      setWorking(false)
    }
  }

  /** 止まったターンを取り消す (開始前に戻る)。失敗は投げる。どちらでも state は読み直す */
  const discardNow = async () => {
    try {
      await post(`/api/sessions/${sid}/discard`)
      dispatch({ kind: 'clear' })
      patchState((s) => ({ ...s, stopped_turn: null }))
    } finally {
      await reload()
    }
  }

  const askDiscard = () => {
    const st = stopped
    if (!st) return
    void ui.confirm({
      title: `ターン ${st.turn} を取り消しますか？`,
      message: `止まったターンの途中結果が消え、ターン ${st.turn} を始める前の状態に戻ります。`,
      ok: '取り消す',
      danger: true,
      run: () => { setErr(''); return discardNow() },
    })
  }

  const fix = async (input: string) => {
    const st = stopped
    if (!st || working) return
    setWorking(true)
    setErr('')
    try {
      await discardNow()
    } catch (e) {
      setErr(`取り消せませんでした: ${errorText(e)}`)
      setWorking(false)
      return
    }
    setWorking(false)
    await start(input, st.mode)
  }

  const redo = async (t: TurnResult, newText?: string) => {
    if (locked) return
    begin(newText ?? t.input, t.mode)
    patchState((s) => ({ ...s, turns: s.turns.filter((x) => x.turn < t.turn) }))
    try {
      const r = await post<{ turn: number }>(`/api/sessions/${sid}/redo`, newText === undefined ? { turn: t.turn } : { turn: t.turn, text: newText })
      dispatch({ kind: 'accepted', turn: r.turn })
    } catch (e) {
      dispatch({ kind: 'clear' })
      setErr(`作り直せませんでした: ${errorText(e)}`)
      void reload()
    }
  }

  const restore = (turn: number) => {
    void ui.confirm({
      title: `ターン ${turn} の前に戻しますか？`,
      message: `ターン ${turn} とそれより後のターンが消え、NPC の HP/MP・配置もその時点に戻ります。`,
      ok: '戻す',
      danger: true,
      run: async () => {
        await post(`/api/sessions/${sid}/restore`, { turn })
        dispatch({ kind: 'clear' })
        await reload()
      },
    })
  }

  const move = async (name: string, lane: Lane) => {
    try {
      await patch('/api/npc-position', { session_id: sid, name, lane })
      patchState((s) => ({ ...s, npc_sheets: s.npc_sheets.map((n) => (n.name === name ? { ...n, lane } : n)) }))
    } catch (e) {
      await ui.alert(errorText(e), '配置を変えられませんでした')
    }
  }

  const turns = state.turns
  const lastTurn = turns.length ? turns[turns.length - 1].turn : -1
  const liveLen = cur ? cur.stages.reduce((n, s) => n + s.text.length + s.status.length, 0) + cur.checks.length : 0
  const version = `${turns.length}:${liveLen}:${cur?.error ?? ''}:${Object.keys(live.images).length}`

  return (
    <div className="session">
      <div className="session-bar">
        <button className="icon-btn" onClick={onLeave} title="セッション一覧へ" aria-label="セッション一覧へ">←</button>
        <b className="title">{state.session.title || `セッション ${sid}`}</b>
        <span className="ui-sys">ターン {state.session.turn}</span>
        <span className={`ui-dot ${socket === 'open' ? 'ok' : 'bad'}`} title={socket === 'open' ? '進行を受け取れます' : 'つなぎ直しています'} />
        <button className="side-toggle" onClick={() => setSideOpen(true)}>🛡 パーティ・配置</button>
      </div>
      <div className="session-body">
        <div className="chat">
          <ChatLog version={version} jump={jump}>
            {turns.length === 0 && !cur && (
              <div className="empty">
                <div className="big">🎲</div>
                冒険を始めましょう。最初の行動を宣言してください。
                <div className="ui-muted">例: 「酒場で依頼の詳しい話を聞く」「森の入口で周囲を警戒する」</div>
              </div>
            )}
            {turns.map((t) => (
              <TurnView
                key={t.turn}
                t={t}
                isLast={t.turn === lastTurn && !cur && !stopped}
                disabled={locked}
                imageUrl={live.images[t.turn]}
                drawing={live.drawing.includes(t.turn)}
                onRedo={(x, v) => void redo(x, v)}
                onRestore={(x) => restore(x.turn)}
              />
            ))}
            {stopped && (
              <StoppedTurnView stopped={stopped} working={working} onResume={() => void resume()} onDiscard={askDiscard} onFix={(v) => void fix(v)} />
            )}
            {cur && !stopped && (
              <LiveTurnView
                live={cur}
                onReload={() => void reload()}
                onRetry={() => void start(cur.input, cur.mode)}
                onEditBack={() => { setText(cur.input); setMode(cur.mode); dispatch({ kind: 'clear' }) }}
              />
            )}
          </ChatLog>
          {err && <div className="err line">{err}</div>}
          <Composer
            text={text}
            setText={setText}
            mode={mode}
            setMode={setMode}
            running={running}
            blocked={locked}
            blockedReason={blockedReason}
            onSend={send}
            onStop={() => void stop()}
          />
        </div>
        {sideOpen && <div className="scrim" onClick={() => setSideOpen(false)} />}
        <div className={sideOpen ? 'side-wrap open' : 'side-wrap'}>
          <SidePanel
            state={state}
            disabled={locked}
            summarizing={live.summarizing}
            onMove={move}
            onRestore={restore}
            onClose={() => setSideOpen(false)}
          />
        </div>
      </div>
    </div>
  )
}
