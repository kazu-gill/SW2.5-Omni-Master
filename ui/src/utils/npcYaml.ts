// NPC シートの yaml_blob (data/personas/*.yaml と同じ形) を読む・値だけ書き換える。
// 書き換えは「読んで → 変えたキーだけ差し替えて → 書き出す」。知らないキーは消さない。
// (js-yaml はコメントを保てないので、保存するとコメントは消える。キーと値は残る)
import yaml from 'js-yaml'

export type YamlMap = Record<string, unknown>

export const PERSONA_FIELDS = [
  { key: 'tone', label: '性格・口ぶり' },
  { key: 'speech_style', label: '話し方' },
  { key: 'values', label: '大事にしているもの' },
  { key: 'fears', label: '恐れ' },
  { key: 'quirks', label: '癖' },
] as const

export type PersonaKey = (typeof PERSONA_FIELDS)[number]['key']

export interface NpcView {
  raw: YamlMap
  race: string
  gender: string
  age: string
  classes: { name: string; level: number }[]
  level: number | null
  stats: [string, string][]
  equipment: [string, string][]
  skills: { name: string; description: string }[]
  personality: Record<PersonaKey, string>
  backstory: string
  actionRules: string[]
  /** personality が文字列だけのとき (古い形) */
  personalityText: string
}

const str = (v: unknown) => (v === null || v === undefined ? '' : String(v))

export function loadYaml(blob: string): YamlMap {
  const v = yaml.load(blob || '')
  if (v === null || v === undefined) return {}
  if (typeof v !== 'object' || Array.isArray(v)) throw new Error('YAML の一番上がキーと値の形になっていません')
  return v as YamlMap
}

const EQUIP_LABEL: Record<string, string> = { weapon: '武器', armor: '鎧', shield: '盾', accessory: '装飾品' }

export function readNpc(blob: string): NpcView {
  let raw: YamlMap
  try {
    raw = loadYaml(blob)
  } catch {
    raw = {}
  }
  const classes = Array.isArray(raw.classes)
    ? (raw.classes as YamlMap[]).map((c) => ({ name: str(c?.name), level: Number(c?.level) || 0 }))
    : []
  const lvFromClasses = classes.length ? Math.max(...classes.map((c) => c.level)) : 0
  const level = typeof raw.lv === 'number' ? raw.lv : typeof raw.level === 'number' ? raw.level : lvFromClasses || null
  const statsSrc = (raw.stats ?? raw.attrs) as YamlMap | undefined
  const stats: [string, string][] = statsSrc && typeof statsSrc === 'object' ? Object.entries(statsSrc).map(([k, v]) => [k, str(v)]) : []
  const eqSrc = (raw.equipment ?? raw.equip) as YamlMap | undefined
  const equipment: [string, string][] = eqSrc && typeof eqSrc === 'object'
    ? Object.entries(eqSrc).map(([k, v]) => [EQUIP_LABEL[k] ?? k, str(v) || '—'])
    : []
  const skills = Array.isArray(raw.skills)
    ? (raw.skills as unknown[]).map((s) => (typeof s === 'object' && s
      ? { name: str((s as YamlMap).name), description: str((s as YamlMap).description ?? (s as YamlMap).note) }
      : { name: str(s), description: '' }))
    : []
  const p = raw.personality
  const pObj = p && typeof p === 'object' && !Array.isArray(p) ? (p as YamlMap) : {}
  const personality = Object.fromEntries(PERSONA_FIELDS.map((f) => [f.key, str(pObj[f.key])])) as Record<PersonaKey, string>
  return {
    raw,
    race: str(raw.race),
    gender: str(raw.gender),
    age: str(raw.age),
    classes,
    level,
    stats,
    equipment,
    skills,
    personality,
    backstory: str(raw.backstory).trimEnd(),
    actionRules: Array.isArray(raw.action_rules) ? (raw.action_rules as unknown[]).map(str) : [],
    personalityText: typeof p === 'string' ? p : '',
  }
}

export interface PersonaEdit {
  personality: Record<PersonaKey, string>
  backstory: string
  actionRules: string[]
}

/** 元の YAML のキーを保ったまま、ペルソナの値だけ差し替えた YAML を返す。変わっていなければ null */
export function applyPersonaEdit(blob: string, edit: PersonaEdit): string | null {
  const raw = loadYaml(blob)
  const before = readNpc(blob)
  let changed = false
  const p = raw.personality
  const pObj: YamlMap = p && typeof p === 'object' && !Array.isArray(p) ? { ...(p as YamlMap) } : {}
  for (const f of PERSONA_FIELDS) {
    if (edit.personality[f.key] !== before.personality[f.key]) {
      pObj[f.key] = edit.personality[f.key]
      changed = true
    }
  }
  if (changed) raw.personality = pObj
  if (edit.backstory.trim() !== before.backstory.trim()) {
    raw.backstory = edit.backstory.trimEnd() + '\n'
    changed = true
  }
  const rules = edit.actionRules.map((r) => r.trim()).filter(Boolean)
  if (rules.join('\n') !== before.actionRules.join('\n')) {
    raw.action_rules = rules
    changed = true
  }
  if (!changed) return null
  return dumpYaml(raw)
}

export function dumpYaml(raw: YamlMap): string {
  return yaml.dump(raw, { lineWidth: -1, noRefs: true })
}

/** 平均レベルの計算用: YAML の lv / classes の最大レベル */
export function npcLevel(blob: string): number | null {
  return readNpc(blob).level
}
