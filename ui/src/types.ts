// API の型。docs/api.md と同じ形 (JSON は snake_case)。

export type Lane = 'enemy' | 'front' | 'party'
export type TurnMode = 'action' | 'gm'

// ── システム ────────────────────────────────────────────────────────────────
export interface ServiceStatus {
  url: string
  model_id?: string
  ok: boolean
}

export interface SystemStatus {
  llm: ServiceStatus
  embed: ServiceStatus
  images: ServiceStatus
  apc_tenant: string
}

// ── セッション ──────────────────────────────────────────────────────────────
export interface Session {
  id: number
  title: string
  quest_id: number | null
  turn: number
  created_at: string
  updated_at: string
}

export interface SessionSummary extends Session {
  quest_title: string
  last_line: string
}

export interface Checkpoint {
  turn: number
  created_at: string
}

/** 途中で止まったターン (失敗・中断・時間切れ・再起動)。「続きから」か「取り消す」を選ぶまで次へ進めない */
export interface StoppedTurn {
  turn: number
  input: string
  mode: TurnMode
  /** 止まった原因 */
  error: string
  /** 終わった段: gm / npc:<名前> / support */
  done: string[]
  /** 終わった段までの結果 */
  result: TurnResult
}

export interface SessionState {
  session: Session
  quest: QuestEntry | null
  pc: PlayerCharacterEntry | null
  npc_sheets: NPCSheet[]
  turns: TurnResult[]
  checkpoints: Checkpoint[]
  summary: string[]
  busy: boolean
  stopped_turn?: StoppedTurn | null
}

export interface NPCSheet {
  id: number
  session_id: number
  name: string
  hp: number
  mp: number
  hp_max: number
  mp_max: number
  lane: Lane
  yaml_blob: string
}

// ── ターン ──────────────────────────────────────────────────────────────────
export interface NPCAction {
  name: string
  action: string
  dialogue: string
  mp_cost: number
  hp_delta: number
  new_lane?: Lane
  check?: string
}

export interface ResourceDelta {
  npc_name: string
  hp_delta: number
  mp_cost: number
}

export interface FormationChange {
  name: string
  new_lane: Lane
}

export type CheckOutcome = 'success' | 'failure' | 'auto_success' | 'auto_failure'

export interface Check {
  who: string
  kind: string
  base: number
  target: number
  dice: [number, number]
  total: number
  result: CheckOutcome
}

export interface RuleHit {
  id: number
  source_type: RuleSource
  section: string
  page_from: number
  page_to: number
  text: string
  score: number
  matched_term?: string
}

export interface TurnResult {
  session_id: number
  turn: number
  mode: TurnMode
  input: string
  gm_narration: string
  npc_actions: NPCAction[]
  deltas: ResourceDelta[]
  formation_changes: FormationChange[]
  checks: Check[]
  rules_used: RuleHit[]
  stage_attempts: Record<string, number>
  warnings: string[]
  image_url?: string
  created_at: string
}

// ── WebSocket ───────────────────────────────────────────────────────────────
export type StageStatus = 'start' | 'retry' | 'done' | 'failed' | 'skipped'

export type WsMessage =
  | { type: 'stage'; session_id: number; turn: number; stage: string; label: string; status: StageStatus; attempt: number; message?: string }
  | { type: 'delta'; session_id: number; turn: number; stage: string; text: string }
  | { type: 'rules'; session_id: number; turn: number; stage: string; rules: RuleHit[] }
  | { type: 'check'; session_id: number; turn: number; check: Check }
  | { type: 'turn'; session_id: number; result: TurnResult }
  | { type: 'error'; session_id: number; turn: number; message: string; resumable?: boolean }
  | { type: 'image'; session_id: number; turn: number; url: string }
  | { type: 'busy'; session_id: number; busy: boolean }

// ── パーティ・PC ────────────────────────────────────────────────────────────
export interface PersonaInfo {
  id: string
  name: string
  hp: number
  mp: number
}

export interface PlayerCharacterEntry {
  id: number
  name: string
  json_blob: string
  is_active: boolean
}

export interface PCAttr {
  base: number    // 種族基本値
  growth: number  // 成長点
}

export interface PCClass {
  name: string
  level: number
}

export interface PCWeapon {
  name: string
  style: string
  hit: string
  damage: string
  range: string
  note: string
}

export interface PCItem {
  name: string
  qty: number
  note: string
}

/** ゆとシート風の PC シート (player_characters.json_blob の中身) */
export interface PCSheet {
  name: string
  race: string
  gender: string
  age: string
  height: string
  weight: string
  birthplace: string
  adventurerLevel: number
  exp: number
  attrs: {
    dex: PCAttr
    agi: PCAttr
    str: PCAttr
    vit: PCAttr
    int: PCAttr
    spr: PCAttr
  }
  hpCurrent: number
  hpMax: number
  mpCurrent: number
  mpMax: number
  classes: PCClass[]
  combatFeats: string
  weapons: PCWeapon[]
  armor: string
  shield: string
  accessories: string
  inventory: PCItem[]
  gold: number
  languages: string
  notes: string
}

// ── クエスト ────────────────────────────────────────────────────────────────
export interface QuestEntry {
  id: number
  rank: 'S' | 'A' | 'B' | 'C'
  title: string
  description: string
  client: string
  reward: string
  target: string
  level: string
  tags: string // カンマ区切り
  status: 'available' | 'active' | 'completed'
}

// ── ルール ──────────────────────────────────────────────────────────────────
export type RuleSource = 'rulebook' | 'correction' | 'houserule'

export interface RuleEntry {
  id: number
  source_type: RuleSource
  priority: number
  tag: string
  text: string
  overrides_id: number | null
  enabled: boolean
  section?: string
  page_from?: number
  page_to?: number
  /** POST の返り値だけにある。false なら埋め込みに失敗 (RAG で見つからない) */
  embedded?: boolean
}

export interface RulePage {
  total: number
  items: RuleEntry[]
}

export type ViewName = 'session' | 'quest' | 'party' | 'rules'
