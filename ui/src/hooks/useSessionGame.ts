// 1 セッションの状態: GET /api/sessions/{id} の SessionState と、WebSocket で届く進行。
// つなぎ直したとき・ターンが確定/失敗したときは SessionState を読み直してそろえる。
// セッションを切り替えるときは呼び出し側で key を変えて作り直す。
import { useCallback, useEffect, useReducer, useRef, useState } from 'react'
import { errorText, get } from '../api'
import type { SessionState, TurnResult } from '../types'
import { initialLive, liveReducer, type LiveAction, type LiveState } from '../live/turnReducer'
import { useSessionSocket, type SocketStatus } from './useSessionSocket'

export interface SessionGame {
  state: SessionState | null
  loadError: string
  reload: () => Promise<void>
  /** 画面の上だけ先に変える (送った直後のターンの切り詰めなど) */
  patchState: (f: (s: SessionState) => SessionState) => void
  live: LiveState
  dispatch: (a: LiveAction) => void
  socket: SocketStatus
}

function withResult(s: SessionState, r: TurnResult): SessionState {
  const turns = [...s.turns.filter((t) => t.turn < r.turn), r]
  return { ...s, turns, session: { ...s.session, turn: Math.max(s.session.turn, r.turn) } }
}

export function useSessionGame(sessionId: number | null): SessionGame {
  const [state, setState] = useState<SessionState | null>(null)
  const [loadError, setLoadError] = useState('')
  const [live, dispatch] = useReducer(liveReducer, initialLive)
  const seq = useRef(0)

  // 読んだ結果を入れる (古い読み直しの結果は捨てる)
  const fetchState = useCallback(() => {
    const my = ++seq.current
    const apply = (s: SessionState) => {
      if (my !== seq.current) return
      setState({
        ...s,
        turns: s.turns ?? [], npc_sheets: s.npc_sheets ?? [], checkpoints: s.checkpoints ?? [], summary: s.summary ?? [],
      })
      setLoadError('')
      dispatch({ kind: 'busy', busy: !!s.busy })
    }
    const fail = (e: unknown) => { if (my === seq.current) setLoadError(errorText(e)) }
    return { req: sessionId === null ? null : get<SessionState>(`/api/sessions/${sessionId}`), apply, fail }
  }, [sessionId])

  const reload = useCallback(async () => {
    const { req, apply, fail } = fetchState()
    if (req) await req.then(apply, fail)
  }, [fetchState])

  useEffect(() => {
    const { req, apply, fail } = fetchState()
    req?.then(apply, fail)
  }, [fetchState])

  const socket = useSessionSocket(sessionId, {
    onOpen: () => { void reload() },
    onDeltas: (items) => dispatch({ kind: 'deltas', items }),
    onMessage: (m) => {
      dispatch({ kind: 'ws', msg: m })
      if (m.type === 'turn') {
        setState((s) => (s ? withResult(s, m.result) : s))
        void reload()
      } else if (m.type === 'error') {
        void reload()
      } else if (m.type === 'image') {
        setState((s) => s && { ...s, turns: s.turns.map((t) => (t.turn === m.turn ? { ...t, image_url: m.url } : t)) })
      }
    },
  })

  const patchState = useCallback((f: (s: SessionState) => SessionState) => {
    setState((s) => (s ? f(s) : s))
  }, [])

  return { state, loadError, reload, patchState, live, dispatch, socket }
}
