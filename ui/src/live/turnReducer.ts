// 進行中のターン (WebSocket で届く段・本文・判定) を画面の状態にまとめる。
// ?mock=1 の作り物のターンも同じ reducer を通る。
import type { Check, RuleHit, StageStatus, StoppedTurn, TurnMode, WsMessage } from '../types'

export interface LiveStage {
  key: string // gm / npc:<名前> / support
  label: string
  status: StageStatus | 'pending'
  attempt: number
  text: string
  message?: string
  startedAt?: number
  endedAt?: number
}

export interface LiveTurn {
  /** サーバーが決めたターン番号。POST の返事か最初の進行で分かる */
  turn: number | null
  input: string
  mode: TurnMode
  stages: LiveStage[]
  rules: Record<string, RuleHit[]>
  checks: Check[]
  startedAt: number
  /** ターンが失敗した (状態は開始前に戻っている) */
  error?: string
  /** 失敗したが途中まで残っている (stopped_turn で「続きから」か「取り消す」を選ぶ) */
  resumable?: boolean
}

export interface LiveState {
  live: LiveTurn | null
  /** サーバーがこのセッションでターン (か要約) を実行中 (他のタブを含む) */
  busy: boolean
  summarizing: boolean
  /** 画像を作っているターン */
  drawing: number[]
  /** 後から届いた画像 (turn → url) */
  images: Record<number, string>
}

export type DeltaItem = { turn: number; stage: string; text: string }

export type LiveAction =
  | { kind: 'begin'; input: string; mode: TurnMode; stages: { key: string; label: string }[] }
  | { kind: 'resume'; stopped: StoppedTurn; stages: { key: string; label: string }[] }
  | { kind: 'accepted'; turn: number }
  | { kind: 'fail'; message: string }
  | { kind: 'clear' }
  | { kind: 'busy'; busy: boolean }
  | { kind: 'deltas'; items: DeltaItem[] }
  | { kind: 'ws'; msg: Exclude<WsMessage, { type: 'delta' }> }
  | { kind: 'reset' }

export const initialLive: LiveState = { live: null, busy: false, summarizing: false, drawing: [], images: {} }

/** 段の並び (GM → NPC ごと → 数値確認)。GM 直訴は GM だけ */
export function plannedStages(mode: TurnMode, npcNames: string[]) {
  if (mode === 'gm') return [{ key: 'gm', label: 'GM' }]
  return [
    { key: 'gm', label: 'GM' },
    ...npcNames.map((n) => ({ key: `npc:${n}`, label: n })),
    { key: 'support', label: '数値確認' },
  ]
}

function newLive(input: string, mode: TurnMode, stages: { key: string; label: string }[]): LiveTurn {
  return {
    turn: null, input, mode, rules: {}, checks: [], startedAt: Date.now(),
    stages: stages.map((s) => ({ ...s, status: 'pending', attempt: 0, text: '' })),
  }
}

/** 止まったターンを続けるときの画面: 終わった段は結果を出したまま、残りを待つ */
function resumedLive(st: StoppedTurn, stages: { key: string; label: string }[]): LiveTurn {
  const r = st.result
  const live = newLive(st.input, st.mode, stages)
  live.turn = st.turn
  live.checks = r.checks ?? []
  live.stages = live.stages.map((s) => {
    if (!st.done.includes(s.key)) return s
    let text = ''
    if (s.key === 'gm') text = r.gm_narration
    else if (s.key.startsWith('npc:')) {
      const a = (r.npc_actions ?? []).find((x) => x.name === s.key.slice(4))
      if (a) text = `dialogue: ${a.dialogue}\naction: ${a.action}`
    }
    return { ...s, status: 'done' as const, attempt: 1, text }
  })
  return live
}

function defaultLabel(key: string) {
  if (key === 'gm') return 'GM'
  if (key === 'support') return '数値確認'
  return key.startsWith('npc:') ? key.slice(4) : key
}

/** ws のメッセージがいまのターンのものか。違うターンなら新しい進行 (他のタブ) として受け直す */
function liveFor(state: LiveState, turn: number): LiveTurn | null {
  const cur = state.live
  if (cur && (cur.turn === null || cur.turn === turn)) return cur.turn === null ? { ...cur, turn } : cur
  if (cur && cur.turn !== null && turn < cur.turn) return null // 古い
  const fresh = newLive('', 'action', [])
  fresh.turn = turn
  return fresh
}

function withStage(live: LiveTurn, key: string, label: string | undefined, f: (s: LiveStage) => LiveStage): LiveTurn {
  const idx = live.stages.findIndex((s) => s.key === key)
  const stages = [...live.stages]
  if (idx < 0) stages.push(f({ key, label: label || defaultLabel(key), status: 'pending', attempt: 0, text: '' }))
  else stages[idx] = f({ ...stages[idx], label: label || stages[idx].label })
  return { ...live, stages }
}

export function liveReducer(state: LiveState, a: LiveAction): LiveState {
  switch (a.kind) {
    case 'reset':
      return initialLive
    case 'begin':
      return { ...state, live: newLive(a.input, a.mode, a.stages) }
    case 'resume':
      return { ...state, live: resumedLive(a.stopped, a.stages) }
    case 'accepted':
      return state.live ? { ...state, live: { ...state.live, turn: a.turn } } : state
    case 'fail':
      return state.live ? { ...state, live: { ...state.live, error: a.message } } : state
    case 'clear':
      return { ...state, live: null }
    case 'busy':
      return { ...state, busy: a.busy, summarizing: a.busy ? state.summarizing : false }
    case 'deltas': {
      let live = state.live
      for (const d of a.items) {
        const l = live && (live.turn === null || live.turn === d.turn) ? live : liveFor({ ...state, live }, d.turn)
        if (!l) continue
        live = withStage(l, d.stage, undefined, (s) => ({
          ...s, text: s.text + d.text, status: s.status === 'pending' ? 'start' : s.status, startedAt: s.startedAt ?? Date.now(),
        }))
      }
      return live === state.live ? state : { ...state, live }
    }
    case 'ws':
      return onWs(state, a.msg)
  }
}

function onWs(state: LiveState, m: Exclude<WsMessage, { type: 'delta' }>): LiveState {
  switch (m.type) {
    case 'busy':
      return { ...state, busy: m.busy, summarizing: m.busy ? state.summarizing : false }
    case 'image':
      return { ...state, images: { ...state.images, [m.turn]: m.url }, drawing: state.drawing.filter((t) => t !== m.turn) }
    case 'turn':
      return { ...state, live: null }
    case 'error': {
      const live = liveFor(state, m.turn)
      if (!live) return state
      const stages = live.stages.map((s) => (s.status === 'start' || s.status === 'retry' ? { ...s, status: 'failed' as const, endedAt: Date.now() } : s))
      return { ...state, live: { ...live, stages, error: m.message, resumable: !!m.resumable } }
    }
    case 'check': {
      const live = liveFor(state, m.turn)
      return live ? { ...state, live: { ...live, checks: [...live.checks, m.check] } } : state
    }
    case 'rules': {
      const live = liveFor(state, m.turn)
      return live ? { ...state, live: { ...live, rules: { ...live.rules, [m.stage]: m.rules } } } : state
    }
    case 'stage': {
      if (m.stage === 'summary') {
        return { ...state, summarizing: m.status === 'start' || m.status === 'retry' }
      }
      if (m.stage === 'image') {
        const others = state.drawing.filter((t) => t !== m.turn)
        return { ...state, drawing: m.status === 'start' || m.status === 'retry' ? [...others, m.turn] : others }
      }
      const live = liveFor(state, m.turn)
      if (!live) return state
      const now = Date.now()
      const next = withStage(live, m.stage, m.label, (s) => {
        switch (m.status) {
          case 'start':
            return { ...s, status: 'start', attempt: m.attempt, startedAt: s.startedAt ?? now, message: m.message }
          case 'retry':
            return { ...s, status: 'retry', attempt: m.attempt, text: '', message: m.message }
          default:
            return { ...s, status: m.status, attempt: m.attempt || s.attempt, endedAt: now, message: m.message }
        }
      })
      return { ...state, busy: true, live: next }
    }
  }
}
