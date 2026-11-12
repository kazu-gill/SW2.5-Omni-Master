import type { Check, Lane, PCSheet, PlayerCharacterEntry, RuleHit } from '../types'

export function formatTime(iso: string): string {
  if (!iso) return '—'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  const p = (n: number) => String(n).padStart(2, '0')
  const today = new Date()
  const sameDay = d.toDateString() === today.toDateString()
  const hm = `${p(d.getHours())}:${p(d.getMinutes())}`
  return sameDay ? `今日 ${hm}` : `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${hm}`
}

export const LANE_LABEL: Record<Lane, string> = {
  enemy: '敵後衛',
  front: '前線',
  party: '味方後衛',
}

export const SOURCE_LABEL: Record<RuleHit['source_type'], string> = {
  rulebook: 'ルールブック',
  correction: '訂正',
  houserule: 'ハウスルール',
}

const OUTCOME: Record<Check['result'], string> = {
  success: '成功',
  failure: '失敗',
  auto_success: '自動成功',
  auto_failure: '自動失敗',
}

export function checkOk(c: Check) {
  return c.result === 'success' || c.result === 'auto_success'
}

/** 🎲 ガルド 命中力判定 5+[3,4]=12 ≥ 11 成功 */
export function checkText(c: Check): string {
  const cmp = checkOk(c) ? '≥' : '<'
  const who = c.who ? `${c.who} ` : ''
  return `🎲 ${who}${c.kind} ${c.base}+[${c.dice[0]},${c.dice[1]}]=${c.total} ${cmp} ${c.target} ${OUTCOME[c.result] ?? c.result}`
}

export function pageText(r: { page_from?: number; page_to?: number }): string {
  if (!r.page_from) return ''
  return r.page_to && r.page_to !== r.page_from ? `p.${r.page_from}–${r.page_to}` : `p.${r.page_from}`
}

export function signed(n: number): string {
  return n > 0 ? `+${n}` : String(n)
}

export function parsePCSheet(pc: PlayerCharacterEntry | null): Partial<PCSheet> | null {
  if (!pc) return null
  try {
    const v = JSON.parse(pc.json_blob) as unknown
    return v && typeof v === 'object' ? (v as Partial<PCSheet>) : {}
  } catch {
    return {}
  }
}
