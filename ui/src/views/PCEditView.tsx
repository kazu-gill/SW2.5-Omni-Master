import { useState } from 'react'
import type { PCSheet, PCClass, PCWeapon, PCItem } from '../types'

interface Props {
  pcId: number | null   // null = new PC
  initial: PCSheet
  onSave: (id: number | null, sheet: PCSheet) => Promise<void>
  onClose: () => void
}

const RACES = [
  'ヒューマン', 'エルフ', 'ドワーフ', 'タビット', 'ナイトメア',
  'リルドラケン', 'グラスランナー', 'メリア', 'ルーンフォーク',
  'ウィークリング', 'ティエンス', 'レプラカーン', 'ハーフエルフ',
  'ドレイク', 'シャドウ',
]

const SW_CLASSES = [
  'ファイター', 'グラップラー', 'シューター',
  'ソーサラー', 'コンジャラー', 'プリースト',
  'マギテック', 'フェアリーテイマー', 'ドラゴンナイト',
  'アルケミスト', 'ライダー', 'バード', 'スカウト',
]

const ATTR_LABELS: { key: keyof PCSheet['attrs']; jp: string; en: string }[] = [
  { key: 'dex', jp: '器用度', en: 'DEX' },
  { key: 'agi', jp: '敏捷度', en: 'AGI' },
  { key: 'str', jp: '筋力',   en: 'STR' },
  { key: 'vit', jp: '生命力', en: 'VIT' },
  { key: 'int', jp: '知力',   en: 'INT' },
  { key: 'spr', jp: '精神力', en: 'SPR' },
]

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

function attrTotal(a: { base: number; growth: number }) { return a.base + a.growth }
function attrBonus(total: number) { return Math.floor(total / 6) }

export default function PCEditView({ pcId, initial, onSave, onClose }: Props) {
  const [sheet, setSheet] = useState<PCSheet>(initial)
  const [saving, setSaving] = useState(false)
  const [activeTab, setActiveTab] = useState<'basic' | 'combat' | 'equip' | 'items'>('basic')

  function setField<K extends keyof PCSheet>(key: K, val: PCSheet[K]) {
    setSheet((s) => ({ ...s, [key]: val }))
  }

  function setAttr(key: keyof PCSheet['attrs'], field: 'base' | 'growth', val: number) {
    setSheet((s) => ({
      ...s,
      attrs: { ...s.attrs, [key]: { ...s.attrs[key], [field]: val } },
    }))
  }

  function setClass(idx: number, field: keyof PCClass, val: string | number) {
    const classes = sheet.classes.map((c, i) => i === idx ? { ...c, [field]: val } : c)
    setField('classes', classes)
  }

  function addClass() {
    setField('classes', [...sheet.classes, { name: 'ファイター', level: 1 }])
  }

  function removeClass(idx: number) {
    setField('classes', sheet.classes.filter((_, i) => i !== idx))
  }

  function setWeapon(idx: number, field: keyof PCWeapon, val: string) {
    const weapons = sheet.weapons.map((w, i) => i === idx ? { ...w, [field]: val } : w)
    setField('weapons', weapons)
  }

  function addWeapon() {
    setField('weapons', [...sheet.weapons, { name: '', style: '', hit: '', damage: '', range: '近接', note: '' }])
  }

  function removeWeapon(idx: number) {
    setField('weapons', sheet.weapons.filter((_, i) => i !== idx))
  }

  function setItem(idx: number, field: keyof PCItem, val: string | number) {
    const inventory = sheet.inventory.map((it, i) => i === idx ? { ...it, [field]: val } : it)
    setField('inventory', inventory)
  }

  function addItem() {
    setField('inventory', [...sheet.inventory, { name: '', qty: 1, note: '' }])
  }

  function removeItem(idx: number) {
    setField('inventory', sheet.inventory.filter((_, i) => i !== idx))
  }

  async function handleSave() {
    setSaving(true)
    await onSave(pcId, sheet)
    setSaving(false)
  }

  const tabs = [
    { key: 'basic',  label: '基本・能力値' },
    { key: 'combat', label: '技能・戦闘' },
    { key: 'equip',  label: '装備' },
    { key: 'items',  label: '所持品・特記' },
  ] as const

  return (
    <div className="pc-edit-overlay" onClick={(e) => { if (e.target === e.currentTarget) onClose() }}>
      <div className="pc-edit-modal">

        {/* ── HEADER ── */}
        <div className="pc-edit-header">
          <div className="pc-edit-title">
            <span style={{ color: 'var(--gold)', fontFamily: 'var(--font-display)', letterSpacing: '0.15em' }}>
              CHARACTER SHEET
            </span>
            <span style={{ fontSize: 12, color: 'var(--text-dim)', marginLeft: 12 }}>
              {sheet.name || '名前未設定'} / {sheet.race} Lv{sheet.adventurerLevel}
            </span>
          </div>
          <div style={{ display: 'flex', gap: 8 }}>
            <button className="pc-edit-save-btn" onClick={handleSave} disabled={saving}>
              {saving ? '保存中…' : '💾 保存'}
            </button>
            <button className="pc-edit-close-btn" onClick={onClose}>✕</button>
          </div>
        </div>

        {/* ── TABS ── */}
        <div className="pc-edit-tabs">
          {tabs.map((t) => (
            <button
              key={t.key}
              className={`pc-tab-btn ${activeTab === t.key ? 'active' : ''}`}
              onClick={() => setActiveTab(t.key)}
            >
              {t.label}
            </button>
          ))}
        </div>

        <div className="pc-edit-body">

          {/* ══ TAB: 基本・能力値 ══ */}
          {activeTab === 'basic' && (
            <div className="pc-tab-content">

              {/* 基本情報 */}
              <div className="pc-section-title">基本情報</div>
              <div className="pc-grid-2">
                <div className="pc-field">
                  <label>名前</label>
                  <input value={sheet.name} onChange={(e) => setField('name', e.target.value)} placeholder="キャラクター名" />
                </div>
                <div className="pc-field">
                  <label>種族</label>
                  <select value={sheet.race} onChange={(e) => setField('race', e.target.value)}>
                    {RACES.map((r) => <option key={r}>{r}</option>)}
                  </select>
                </div>
                <div className="pc-field">
                  <label>性別</label>
                  <input value={sheet.gender} onChange={(e) => setField('gender', e.target.value)} placeholder="男 / 女 / 他" />
                </div>
                <div className="pc-field">
                  <label>年齢</label>
                  <input value={sheet.age} onChange={(e) => setField('age', e.target.value)} placeholder="例: 20歳" />
                </div>
                <div className="pc-field">
                  <label>身長</label>
                  <input value={sheet.height} onChange={(e) => setField('height', e.target.value)} placeholder="例: 170cm" />
                </div>
                <div className="pc-field">
                  <label>体重</label>
                  <input value={sheet.weight} onChange={(e) => setField('weight', e.target.value)} placeholder="例: 65kg" />
                </div>
                <div className="pc-field">
                  <label>出身地</label>
                  <input value={sheet.birthplace} onChange={(e) => setField('birthplace', e.target.value)} placeholder="出身地・故郷" />
                </div>
                <div className="pc-field">
                  <label>冒険者Lv</label>
                  <input type="number" min={1} max={15} value={sheet.adventurerLevel}
                    onChange={(e) => setField('adventurerLevel', parseInt(e.target.value) || 1)} />
                </div>
                <div className="pc-field">
                  <label>経験点</label>
                  <input type="number" min={0} value={sheet.exp}
                    onChange={(e) => setField('exp', parseInt(e.target.value) || 0)} />
                </div>
              </div>

              {/* 能力値 */}
              <div className="pc-section-title" style={{ marginTop: 20 }}>能力値</div>
              <table className="pc-attr-table">
                <thead>
                  <tr>
                    <th>能力値</th>
                    <th>種族基本値</th>
                    <th>成長点</th>
                    <th>合計</th>
                    <th>ボーナス</th>
                  </tr>
                </thead>
                <tbody>
                  {ATTR_LABELS.map(({ key, jp, en }) => {
                    const total = attrTotal(sheet.attrs[key])
                    const bonus = attrBonus(total)
                    return (
                      <tr key={key}>
                        <td className="pc-attr-label">{jp}<span className="pc-attr-en">{en}</span></td>
                        <td>
                          <input type="number" className="pc-attr-input" min={0} value={sheet.attrs[key].base}
                            onChange={(e) => setAttr(key, 'base', parseInt(e.target.value) || 0)} />
                        </td>
                        <td>
                          <input type="number" className="pc-attr-input" min={0} value={sheet.attrs[key].growth}
                            onChange={(e) => setAttr(key, 'growth', parseInt(e.target.value) || 0)} />
                        </td>
                        <td className="pc-attr-total">{total}</td>
                        <td className="pc-attr-bonus">{bonus >= 0 ? `+${bonus}` : bonus}</td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>

              {/* HP/MP */}
              <div className="pc-section-title" style={{ marginTop: 20 }}>HP / MP</div>
              <div className="pc-hpmp-row">
                <div className="pc-hpmp-group">
                  <span className="pc-hpmp-label" style={{ color: 'var(--hp-color)' }}>HP</span>
                  <div className="pc-field-inline">
                    <label>現在</label>
                    <input type="number" min={0} value={sheet.hpCurrent}
                      onChange={(e) => setField('hpCurrent', parseInt(e.target.value) || 0)} />
                  </div>
                  <span style={{ color: 'var(--text-dim)' }}>/</span>
                  <div className="pc-field-inline">
                    <label>最大</label>
                    <input type="number" min={0} value={sheet.hpMax}
                      onChange={(e) => setField('hpMax', parseInt(e.target.value) || 0)} />
                  </div>
                </div>
                <div className="pc-hpmp-group">
                  <span className="pc-hpmp-label" style={{ color: 'var(--mp-color)' }}>MP</span>
                  <div className="pc-field-inline">
                    <label>現在</label>
                    <input type="number" min={0} value={sheet.mpCurrent}
                      onChange={(e) => setField('mpCurrent', parseInt(e.target.value) || 0)} />
                  </div>
                  <span style={{ color: 'var(--text-dim)' }}>/</span>
                  <div className="pc-field-inline">
                    <label>最大</label>
                    <input type="number" min={0} value={sheet.mpMax}
                      onChange={(e) => setField('mpMax', parseInt(e.target.value) || 0)} />
                  </div>
                </div>
              </div>
            </div>
          )}

          {/* ══ TAB: 技能・戦闘 ══ */}
          {activeTab === 'combat' && (
            <div className="pc-tab-content">

              {/* 技能 */}
              <div className="pc-section-title">技能（クラス）</div>
              <table className="pc-class-table">
                <thead>
                  <tr><th>技能名</th><th>Lv</th><th style={{ width: 40 }} /></tr>
                </thead>
                <tbody>
                  {sheet.classes.map((cls, i) => (
                    <tr key={i}>
                      <td>
                        <select value={cls.name} onChange={(e) => setClass(i, 'name', e.target.value)}>
                          {SW_CLASSES.map((c) => <option key={c}>{c}</option>)}
                        </select>
                      </td>
                      <td>
                        <input type="number" min={1} max={10} value={cls.level}
                          onChange={(e) => setClass(i, 'level', parseInt(e.target.value) || 1)}
                          style={{ width: 50 }} />
                      </td>
                      <td>
                        <button className="pc-row-del" onClick={() => removeClass(i)} title="削除">×</button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
              {sheet.classes.length < 3 && (
                <button className="pc-add-row-btn" onClick={addClass}>+ 技能を追加</button>
              )}

              {/* 言語 */}
              <div className="pc-section-title" style={{ marginTop: 20 }}>言語</div>
              <input className="pc-wide-input" value={sheet.languages}
                onChange={(e) => setField('languages', e.target.value)}
                placeholder="共通語 / エルフ語 / etc." />

              {/* 戦闘特技 */}
              <div className="pc-section-title" style={{ marginTop: 20 }}>戦闘特技</div>
              <textarea className="pc-textarea" rows={4} value={sheet.combatFeats}
                onChange={(e) => setField('combatFeats', e.target.value)}
                placeholder="例: かばう / 武器習熟（大剣）/ 必殺技 etc." />
            </div>
          )}

          {/* ══ TAB: 装備 ══ */}
          {activeTab === 'equip' && (
            <div className="pc-tab-content">

              {/* 武器 */}
              <div className="pc-section-title">武器</div>
              <table className="pc-weapon-table">
                <thead>
                  <tr>
                    <th>武器名</th>
                    <th>武器種</th>
                    <th>命中</th>
                    <th>ダメージ</th>
                    <th>射程</th>
                    <th>備考</th>
                    <th style={{ width: 36 }} />
                  </tr>
                </thead>
                <tbody>
                  {sheet.weapons.map((w, i) => (
                    <tr key={i}>
                      <td><input value={w.name} onChange={(e) => setWeapon(i, 'name', e.target.value)} placeholder="大剣" /></td>
                      <td><input value={w.style} onChange={(e) => setWeapon(i, 'style', e.target.value)} placeholder="剣" /></td>
                      <td><input value={w.hit} onChange={(e) => setWeapon(i, 'hit', e.target.value)} placeholder="+2" style={{ width: 50 }} /></td>
                      <td><input value={w.damage} onChange={(e) => setWeapon(i, 'damage', e.target.value)} placeholder="2d+6" style={{ width: 70 }} /></td>
                      <td><input value={w.range} onChange={(e) => setWeapon(i, 'range', e.target.value)} placeholder="近接" style={{ width: 60 }} /></td>
                      <td><input value={w.note} onChange={(e) => setWeapon(i, 'note', e.target.value)} placeholder="備考" /></td>
                      <td><button className="pc-row-del" onClick={() => removeWeapon(i)}>×</button></td>
                    </tr>
                  ))}
                </tbody>
              </table>
              <button className="pc-add-row-btn" onClick={addWeapon}>+ 武器を追加</button>

              {/* 防具 */}
              <div className="pc-section-title" style={{ marginTop: 20 }}>防具・盾・装飾品</div>
              <div className="pc-grid-2">
                <div className="pc-field">
                  <label>鎧・防具</label>
                  <input value={sheet.armor} onChange={(e) => setField('armor', e.target.value)} placeholder="革鎧 / 鎖帷子 etc." />
                </div>
                <div className="pc-field">
                  <label>盾</label>
                  <input value={sheet.shield} onChange={(e) => setField('shield', e.target.value)} placeholder="小盾 etc." />
                </div>
                <div className="pc-field" style={{ gridColumn: '1 / -1' }}>
                  <label>装飾品・アクセサリー</label>
                  <input value={sheet.accessories} onChange={(e) => setField('accessories', e.target.value)} placeholder="魔法の指輪 etc." />
                </div>
              </div>
            </div>
          )}

          {/* ══ TAB: 所持品・特記 ══ */}
          {activeTab === 'items' && (
            <div className="pc-tab-content">

              {/* 所持品 */}
              <div className="pc-section-title">所持品</div>
              <table className="pc-item-table">
                <thead>
                  <tr><th>アイテム名</th><th>個数</th><th>備考</th><th style={{ width: 36 }} /></tr>
                </thead>
                <tbody>
                  {sheet.inventory.map((it, i) => (
                    <tr key={i}>
                      <td><input value={it.name} onChange={(e) => setItem(i, 'name', e.target.value)} placeholder="回復薬" /></td>
                      <td><input type="number" min={0} value={it.qty} onChange={(e) => setItem(i, 'qty', parseInt(e.target.value) || 0)} style={{ width: 60 }} /></td>
                      <td><input value={it.note} onChange={(e) => setItem(i, 'note', e.target.value)} placeholder="HP+10回復" /></td>
                      <td><button className="pc-row-del" onClick={() => removeItem(i)}>×</button></td>
                    </tr>
                  ))}
                </tbody>
              </table>
              <button className="pc-add-row-btn" onClick={addItem}>+ アイテムを追加</button>

              {/* 所持金 */}
              <div className="pc-section-title" style={{ marginTop: 20 }}>所持金</div>
              <div className="pc-field" style={{ maxWidth: 200 }}>
                <label>ガメル</label>
                <input type="number" min={0} value={sheet.gold}
                  onChange={(e) => setField('gold', parseInt(e.target.value) || 0)} />
              </div>

              {/* 特記事項 */}
              <div className="pc-section-title" style={{ marginTop: 20 }}>特記事項・バックストーリー</div>
              <textarea className="pc-textarea" rows={6} value={sheet.notes}
                onChange={(e) => setField('notes', e.target.value)}
                placeholder="キャラクターの背景、動機、特徴など自由に記入..." />
            </div>
          )}

        </div>
      </div>
    </div>
  )
}
