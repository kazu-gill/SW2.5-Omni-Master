export interface NPCAction {
  Name: string
  Action: string
  Dice: string
  Target: string
  Dialogue: string
  HPDelta: number
  MPCost: number
  NewLane?: string
}

export interface FormationChange {
  name: string
  new_lane: string
}

export interface ResourceDelta {
  npc_name: string
  hp_delta: number
  mp_cost: number
}

export interface TurnResult {
  session_id: number
  turn: number
  gm_narration: string
  npc_actions: NPCAction[]
  deltas: ResourceDelta[]
  formation_changes?: FormationChange[]
  image_url?: string
}

export interface NPCSheet {
  ID: number
  SessionID: number
  Name: string
  HP: number
  MP: number
  PositionX: number
  PositionY: number
  YAMLBlob: string
}

export interface Checkpoint {
  ID: number
  SessionID: number
  Turn: number
  SnapshotJSON: string
  CreatedAt: string
}

export interface SessionLog {
  id: number
  turn: number
  role: 'player' | 'gm' | 'npc' | 'support'
  content: string
}

export interface RuleEntry {
  id: number
  source_type: 'rulebook' | 'correction' | 'houserule'
  priority: number
  tag: string
  text: string
  overrides_id: number | null
  enabled: boolean
}

export interface NPCSkill {
  name: string
  rank: number
  type: 'combat' | 'magic' | 'general'
  note: string
}

export interface NPCConsumable {
  name: string
  cur: number
  max: number
}

export interface NPCPersona {
  personality: string
  motivation: string
  speech: string
  priorities: string[]
  forbidden: string[]
}

export interface NPC {
  name: string
  icon: string
  class: string
  race?: string
  lv?: number
  hp: [number, number]
  mp: [number, number]
  attrs?: { STR: number; AGI: number; DEX: number; INT: number; SPR: number; LUC: number }
  skills?: NPCSkill[]
  equip?: { 武器: string; 防具: string; 盾: string; 装飾: string }
  consumables?: NPCConsumable[]
  inventory?: { name: string; qty: number; note: string }[]
  status: string
  position: string
  persona?: NPCPersona
}

export interface PortraitEntry {
  name: string
  prompt: string
  seed: number | null
  generated: boolean
  dataUrl: string | null
}

export interface AlbumEntry {
  id: number
  note: string
  source: 'comfyui' | 'upload'
  seed: number | null
  date: string
  active: boolean
  dataUrl: string | null
}

export interface EnemyEntry {
  name: string
  status: string
  analyzed: boolean
  hpPct: number | null
  data: Record<string, string | null>
}

export interface ImageUpdate {
  session_id: number
  turn: number
  image_url: string
}

// ── Player Character (ゆとシート互換) ───────────────────────────────────────

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
  style: string   // 武器種別・流派
  hit: string     // 命中修正
  damage: string  // ダメージ
  range: string   // 射程
  note: string
}

export interface PCItem {
  name: string
  qty: number
  note: string
}

export interface PCSheet {
  // 基本情報
  name: string
  race: string
  gender: string
  age: string
  height: string
  weight: string
  birthplace: string
  adventurerLevel: number
  exp: number
  // 能力値
  attrs: {
    dex: PCAttr   // 器用度
    agi: PCAttr   // 敏捷度
    str: PCAttr   // 筋力
    vit: PCAttr   // 生命力
    int: PCAttr   // 知力
    spr: PCAttr   // 精神力
  }
  // HP/MP
  hpCurrent: number
  hpMax: number
  mpCurrent: number
  mpMax: number
  // 技能
  classes: PCClass[]
  // 戦闘特技
  combatFeats: string
  // 装備
  weapons: PCWeapon[]
  armor: string
  shield: string
  accessories: string
  // 所持品
  inventory: PCItem[]
  // 金銭・言語・特記
  gold: number
  languages: string
  notes: string
}

export interface PlayerCharacterEntry {
  id: number
  name: string
  json_blob: string
  is_active: boolean
}

export interface QuestEntry {
  id: number
  rank: 'S' | 'A' | 'B' | 'C'
  title: string
  description: string
  client: string
  reward: string
  target: string
  level: string
  tags: string        // comma-separated
  status: 'available' | 'active' | 'completed'
}

export type ViewName = 'start' | 'session' | 'quest' | 'npc' | 'rules'
export type SocketStatus = 'connecting' | 'connected' | 'disconnected'

export interface ChatEntry {
  role: 'player' | 'gm' | 'npc' | 'system'
  speaker: string
  content: string
  turn: number
  image_url?: string
}
