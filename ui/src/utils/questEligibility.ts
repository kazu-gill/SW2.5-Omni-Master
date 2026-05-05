import type { QuestEntry } from '../types'

// Parse "Lv.3〜5" or "Lv.8〜12" → [min, max]
export function parseQuestLevel(level: string): [number, number] | null {
  const m = level.match(/(\d+)[〜~-](\d+)/)
  if (!m) return null
  return [parseInt(m[1], 10), parseInt(m[2], 10)]
}

export type Eligibility = 'ok' | 'caution' | 'hard' | 'unknown'

// ok      : partyAvg >= min
// caution : partyAvg >= min - 2  (少し難易度高め)
// hard    : partyAvg < min - 2   (推奨レベル未満)
// unknown : partyAvgLevel is null
export function questEligibility(quest: QuestEntry, partyAvgLevel: number | null): Eligibility {
  if (partyAvgLevel === null) return 'unknown'
  const range = parseQuestLevel(quest.level)
  if (!range) return 'unknown'
  const [min] = range
  if (partyAvgLevel >= min) return 'ok'
  if (partyAvgLevel >= min - 2) return 'caution'
  return 'hard'
}

export const ELIGIBILITY_LABEL: Record<Eligibility, string> = {
  ok:      '受注可',
  caution: '挑戦的',
  hard:    'Lv不足',
  unknown: '',
}

export const ELIGIBILITY_COLOR: Record<Eligibility, string> = {
  ok:      'var(--green)',
  caution: 'var(--amber)',
  hard:    'var(--red)',
  unknown: 'var(--text-dim)',
}
