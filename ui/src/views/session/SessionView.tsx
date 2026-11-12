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
  const busy = running || live.busy
  const npcNames = state.npc_sheets.map((s) => s.name)
  const blockedReason = live.summarizing
    ? '📝 要約を更新しています。終わると送れます'
    : live.busy ? '別のタブでターンが進行中です。終わると送れます'
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
      await post(`/api/sessions/${sid}/cancel`)
      dispatch({ kind: 'clear' })
      setText(cur.input)
      setMode(cur.mode)
      setErr('止めました。このターンは無かったことになっています。入力を直して送り直せます。')
    } catch (e) {
      setErr(`止められませんでした: ${errorText(e)}`)
    }
    void reload()
  }

  const redo = async (t: TurnResult, newText?: string) => {
    if (busy) return
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
                isLast={t.turn === lastTurn && !cur}
                disabled={busy}
                imageUrl={live.images[t.turn]}
                drawing={live.drawing.includes(t.turn)}
                onRedo={(x, v) => void redo(x, v)}
                onRestore={(x) => restore(x.turn)}
              />
            ))}
            {cur && (
              <LiveTurnView
                live={cur}
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
            blocked={busy}
            blockedReason={blockedReason}
            onSend={send}
            onStop={() => void stop()}
          />
        </div>
        {sideOpen && <div className="scrim" onClick={() => setSideOpen(false)} />}
        <div className={sideOpen ? 'side-wrap open' : 'side-wrap'}>
          <SidePanel
            state={state}
            disabled={busy}
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
