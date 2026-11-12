import type { PCSheet } from '../types'

/** 新しい PC のシート (ゆとシート風)。json_blob の形 */
export function makeEmptySheet(): PCSheet {
  return {
    name: '', race: 'ヒューマン', gender: '', age: '', height: '', weight: '',
    birthplace: '', adventurerLevel: 1, exp: 0,
    attrs: {
      dex: { base: 10, growth: 0 },
      agi: { base: 10, growth: 0 },
      str: { base: 10, growth: 0 },
      vit: { base: 10, growth: 0 },
      int: { base: 10, growth: 0 },
      spr: { base: 10, growth: 0 },
    },
    hpCurrent: 0, hpMax: 0, mpCurrent: 0, mpMax: 0,
    classes: [{ name: 'ファイター', level: 1 }],
    combatFeats: '',
    weapons: [{ name: '', style: '', hit: '', damage: '', range: '近接', note: '' }],
    armor: '', shield: '', accessories: '',
    inventory: [],
    gold: 0, languages: '共通語', notes: '',
  }
}

/** 保存されている json_blob を読む。足りない項目は空のシートの値で埋める */
export function readPCSheet(json: string): PCSheet {
  const base = makeEmptySheet()
  try {
    const v = JSON.parse(json) as Partial<PCSheet>
    return { ...base, ...v, attrs: { ...base.attrs, ...(v.attrs ?? {}) } }
  } catch {
    return base
  }
}
