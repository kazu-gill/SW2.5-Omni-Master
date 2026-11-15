// 開発用の作り物のサーバー (?mock=1)。本物の API の形で答え、ターンは WebSocket と同じメッセージを
// 時間差で流して、画面の reducer をそのまま通す。サーバー無しで進行表示・ストリーミングを確かめるためだけのもの。
import { ApiError } from '../api'
import type { NPCSheet, SessionState, StoppedTurn, TurnResult, WsMessage } from '../types'

const SID = 1
const yamlOf = (name: string, tone: string) =>
  `name: ${name}\nrace: ドワーフ\nclasses:\n  - name: ファイター\n    level: 4\nstats:\n  str: 14\n  dex: 9\npersonality:\n  tone: ${tone}\n  speech_style: 短文\nbackstory: |\n  元傭兵。\naction_rules:\n  - 降伏した敵は討たない\n`
const sheet = (id: number, name: string, lane: NPCSheet['lane'], hp: number): NPCSheet => ({
  id, session_id: SID, name, hp, mp: 10, hp_max: 38, mp_max: 10, lane, yaml_blob: yamlOf(name, '無骨'),
})

const now = () => new Date().toISOString()
const state: SessionState = {
  session: { id: SID, title: '森の遺跡の調査 (作り物)', quest_id: null, turn: 0, created_at: now(), updated_at: now() },
  quest: null,
  pc: { id: 1, name: 'リナ', is_active: true, json_blob: JSON.stringify({ name: 'リナ', race: 'エルフ', adventurerLevel: 3, hpCurrent: 20, hpMax: 24, mpCurrent: 18, mpMax: 18, classes: [{ name: 'ソーサラー', level: 3 }] }) },
  npc_sheets: [sheet(1, 'ガルド', 'front', 38), sheet(2, 'セラ', 'party', 24)],
  turns: [], checkpoints: [], summary: ['リナたちは森の遺跡の入口に着いた。'], busy: false, stopped_turn: null,
}

let listener: ((m: WsMessage) => void) | null = null
let lastInput = ''
let timers: ReturnType<typeof setTimeout>[] = []
const emit = (m: WsMessage) => listener?.(m)

// 入力に「止」を含めると、GM の後で止まる (resumable)。「続きから」で残りが流れる。
function playTurn(text: string, mode: TurnResult['mode'], resume?: StoppedTurn) {
  const turn = resume ? resume.turn : state.session.turn + 1
  if (!resume) lastInput = text
  const stopHere = !resume && text.includes('止') && mode === 'action'
  const gm = '苔むした石段を下りると、ひんやりした空気が肌を撫でる。奥で何かが動く気配がした。'
  const steps: [number, WsMessage][] = []
  let t = 300
  const at = (ms: number, m: WsMessage) => { t += ms; steps.push([t, m]) }
  const base = { session_id: SID, turn }
  at(0, { type: 'busy', session_id: SID, busy: true })
  const check = { who: 'ガルド', kind: '危険感知判定', base: 5, target: 11, dice: [3, 4] as [number, number], total: 12, result: 'success' as const }
  if (!resume) {
    at(100, { type: 'stage', ...base, stage: 'gm', label: 'GM', status: 'start', attempt: 1 })
    for (const ch of '最初の書き出し…') at(60, { type: 'delta', ...base, stage: 'gm', text: ch })
    at(300, { type: 'stage', ...base, stage: 'gm', label: 'GM', status: 'retry', attempt: 2, message: '出力が形式に合わない' })
    for (const ch of gm) at(40, { type: 'delta', ...base, stage: 'gm', text: ch })
    at(100, { type: 'stage', ...base, stage: 'gm', label: 'GM', status: 'done', attempt: 2 })
    at(50, { type: 'rules', ...base, stage: 'gm', rules: [{ id: 12, source_type: 'rulebook', section: '第2部 > 探索 > 危険感知判定', page_from: 112, page_to: 112, text: '危険感知判定はスカウト技能レベル+知力ボーナス…', score: 0.61 }] })
    at(50, { type: 'check', ...base, check })
  }
  if (stopHere) {
    const partial: TurnResult = {
      session_id: SID, turn, mode, input: text, gm_narration: gm, npc_actions: [], deltas: [], formation_changes: [],
      checks: [check], rules_used: [], stage_attempts: { gm: 2 }, warnings: [], created_at: now(),
    }
    const error = 'LLM が応答しませんでした (作り物)'
    at(300, { type: 'error', ...base, message: error, resumable: true })
    at(10, { type: 'busy', session_id: SID, busy: false })
    timers = steps.map(([ms, m]) => setTimeout(() => {
      if (m.type === 'error') state.stopped_turn = { turn, input: text, mode, error, done: ['gm'], result: partial }
      emit(m)
    }, ms))
    return turn
  }
  if (resume) at(0, { type: 'busy', session_id: SID, busy: true })
  if (mode === 'action') {
    for (const s of state.npc_sheets) {
      const stage = `npc:${s.name}`
      at(200, { type: 'stage', ...base, stage, label: s.name, status: 'start', attempt: 1 })
      for (const ch of `name: ${s.name}\ndialogue: 気を抜くな。`) at(30, { type: 'delta', ...base, stage, text: ch })
      at(100, { type: 'stage', ...base, stage, label: s.name, status: 'done', attempt: 1 })
    }
    at(200, { type: 'stage', ...base, stage: 'support', label: '数値確認', status: 'start', attempt: 1 })
    at(800, { type: 'stage', ...base, stage: 'support', label: '数値確認', status: 'done', attempt: 1 })
  }
  const result: TurnResult = {
    session_id: SID, turn, mode, input: text, gm_narration: gm,
    npc_actions: mode === 'gm' ? [] : [
      { name: 'ガルド', action: '斧を構えて先頭に立つ', dialogue: '気を抜くな。', mp_cost: 0, hp_delta: -3, new_lane: 'front' },
      { name: 'セラ', action: '小声で祈りを捧げる', dialogue: '皆さん、お気をつけて。', mp_cost: 2, hp_delta: 0 },
    ],
    deltas: mode === 'gm' ? [] : [{ npc_name: 'ガルド', hp_delta: -3, mp_cost: 0 }, { npc_name: 'セラ', hp_delta: 0, mp_cost: 2 }],
    formation_changes: [], checks: [check],
    rules_used: [], stage_attempts: { gm: 2 }, warnings: [], created_at: now(),
  }
  steps.push([t + 100, { type: 'turn', session_id: SID, result }])
  steps.push([t + 150, { type: 'busy', session_id: SID, busy: false }])
  timers = steps.map(([ms, m]) => setTimeout(() => {
    if (m.type === 'turn') {
      state.turns.push(m.result)
      state.checkpoints.push({ turn: m.result.turn, created_at: now() })
      state.session.turn = m.result.turn
    }
    emit(m)
  }, ms))
  return turn
}

export async function mockApi(path: string, method: string, body: unknown): Promise<unknown> {
  await new Promise((r) => setTimeout(r, 80))
  const b = (body ?? {}) as Record<string, unknown>
  const p = path.split('?')[0]
  if (p === '/api/system') return { llm: { url: 'mock', model_id: 'gemma4-31b-it-heretic-4bit-mtp', ok: true }, embed: { url: 'mock', ok: true }, images: { url: 'mock', ok: false }, apc_tenant: 'sw25' }
  if (p === '/api/sessions' && method === 'GET') return [{ ...state.session, quest_title: '', last_line: state.turns.at(-1)?.gm_narration ?? '' }]
  if (p === '/api/sessions' && method === 'POST') return state.session
  if (p === `/api/sessions/${SID}`) return structuredClone(state)
  const LOCK = '止まったターンがあります。「続きから」か「取り消す」を先に選んでください'
  if (state.stopped_turn && [`/api/sessions/${SID}/turn`, `/api/sessions/${SID}/redo`, `/api/sessions/${SID}/restore`].includes(p)) throw new ApiError(LOCK, 409)
  if (p === `/api/sessions/${SID}/turn`) return { turn: playTurn(String(b.text), b.mode === 'gm' ? 'gm' : 'action') }
  if (p === `/api/sessions/${SID}/redo`) {
    state.turns = state.turns.filter((t) => t.turn < Number(b.turn))
    state.session.turn = Number(b.turn) - 1
    return { turn: playTurn(String(b.text ?? 'やり直し'), 'action') }
  }
  if (p === `/api/sessions/${SID}/resume`) {
    const st = state.stopped_turn
    if (!st) throw new ApiError('止まったターンがありません', 404)
    state.stopped_turn = null
    return { turn: playTurn(st.input, st.mode, st) }
  }
  if (p === `/api/sessions/${SID}/discard`) {
    if (!state.stopped_turn) throw new ApiError('止まったターンがありません', 404)
    state.stopped_turn = null
    return undefined
  }
  if (p === `/api/sessions/${SID}/cancel`) {
    timers.forEach(clearTimeout)
    const turn = state.session.turn + 1
    const error = 'ターンを中断しました。途中まで残っています'
    state.stopped_turn = {
      turn, input: lastInput, mode: 'action', error, done: [],
      result: { session_id: SID, turn, mode: 'action', input: lastInput, gm_narration: '', npc_actions: [], deltas: [], formation_changes: [], checks: [], rules_used: [], stage_attempts: {}, warnings: [], created_at: now() },
    }
    emit({ type: 'error', session_id: SID, turn, message: error, resumable: true })
    emit({ type: 'busy', session_id: SID, busy: false })
    return undefined
  }
  if (p === `/api/sessions/${SID}/restore`) {
    state.turns = state.turns.filter((t) => t.turn < Number(b.turn))
    state.checkpoints = state.checkpoints.filter((c) => c.turn < Number(b.turn))
    state.session.turn = Number(b.turn) - 1
    return undefined
  }
  if (p === '/api/npc-position') { state.npc_sheets = state.npc_sheets.map((s) => (s.name === b.name ? { ...s, lane: b.lane as NPCSheet['lane'] } : s)); return undefined }
  if (p === '/api/quests') return [{ id: 1, rank: 'B', title: '森の遺跡の調査', description: '遺跡の奥を調べる', client: '村長', reward: '1200G', target: '最奥の部屋を調べる', level: 'Lv.2〜4', tags: 'explore,dungeon', status: 'available' }]
  if (p === '/api/player-characters') return [state.pc]
  if (p === '/api/personas') return [{ id: 'zain', name: 'ザイン', hp: 30, mp: 12 }]
  if (p.startsWith('/api/rules/search')) return []
  if (p === '/api/rules') return { total: 0, items: [] }
  throw new Error(`作り物のサーバーは ${method} ${p} に答えられません`)
}

export function mockConnect(_sessionId: number, receive: (m: WsMessage) => void) {
  listener = receive
  return () => { listener = null; timers.forEach(clearTimeout) }
}
