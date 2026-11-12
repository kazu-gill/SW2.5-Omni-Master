// NPC 1 体の詳細と編集。保存は元の YAML のキーを保ったまま、変えた値だけ差し替える。
import { useMemo, useState } from 'react'
import { errorText, patch } from '../../api'
import type { NPCSheet } from '../../types'
import { applyPersonaEdit, loadYaml, PERSONA_FIELDS, readNpc, type PersonaEdit } from '../../utils/npcYaml'
import Bar from '../../components/Bar'
import { LANE_LABEL } from '../../utils/format'

interface Props {
  sheet: NPCSheet
  devMode: boolean
  busy: boolean
  onSaved: () => Promise<void>
  onRemove: () => void
}

type Mode = 'view' | 'edit' | 'raw'

export default function NPCDetail({ sheet, devMode, busy, onSaved, onRemove }: Props) {
  const npc = useMemo(() => readNpc(sheet.yaml_blob), [sheet.yaml_blob])
  const [mode, setMode] = useState<Mode>('view')
  const [hp, setHp] = useState(sheet.hp)
  const [mp, setMp] = useState(sheet.mp)
  const [persona, setPersona] = useState<PersonaEdit>({ personality: npc.personality, backstory: npc.backstory, actionRules: npc.actionRules })
  const [raw, setRaw] = useState(sheet.yaml_blob)
  const [saving, setSaving] = useState(false)
  const [err, setErr] = useState('')

  const startEdit = (m: Mode) => {
    setHp(sheet.hp); setMp(sheet.mp)
    setPersona({ personality: npc.personality, backstory: npc.backstory, actionRules: npc.actionRules })
    setRaw(sheet.yaml_blob)
    setErr('')
    setMode(m)
  }

  const save = async () => {
    setSaving(true)
    setErr('')
    try {
      const body: { hp: number; mp: number; yaml_blob?: string } = { hp, mp }
      if (mode === 'raw') {
        loadYaml(raw) // 書き間違いはここで止める
        if (raw !== sheet.yaml_blob) body.yaml_blob = raw
      } else {
        const y = applyPersonaEdit(sheet.yaml_blob, persona)
        if (y !== null) body.yaml_blob = y
      }
      await patch(`/api/npc-sheet/${sheet.id}`, body)
      await onSaved()
      setMode('view')
    } catch (e) {
      setErr(`保存できませんでした: ${errorText(e)}`)
    } finally {
      setSaving(false)
    }
  }

  const setP = (k: keyof PersonaEdit['personality'], v: string) =>
    setPersona((p) => ({ ...p, personality: { ...p.personality, [k]: v } }))

  return (
    <div className="ui-panel npc-detail">
      <div className="ui-row">
        <h3 className="grow">🛡 {sheet.name}</h3>
        <span className="ui-sys">{[npc.race, npc.gender, npc.age && `${npc.age}歳`].filter(Boolean).join(' / ')} {npc.level ? `Lv${npc.level}` : ''} ・ {LANE_LABEL[sheet.lane] ?? sheet.lane}</span>
      </div>
      {mode === 'view' && (
        <>
          <Bar kind="hp" cur={sheet.hp} max={sheet.hp_max} />
          <Bar kind="mp" cur={sheet.mp} max={sheet.mp_max} />
          <div className="ui-row">
            <button disabled={busy} onClick={() => startEdit('edit')}>✎ 編集</button>
            {devMode && <button disabled={busy} onClick={() => startEdit('raw')}>YAML を直接編集 (DEV)</button>}
            <button className="danger" disabled={busy} onClick={onRemove}>パーティから外す</button>
          </div>
          {busy && <div className="ui-sys">ターンの進行中は編集できません</div>}

          <h4>ペルソナ</h4>
          {npc.personalityText && <p>{npc.personalityText}</p>}
          <dl className="kv">
            {PERSONA_FIELDS.map((f) => npc.personality[f.key] && (
              <div key={f.key}><dt>{f.label}</dt><dd>{npc.personality[f.key]}</dd></div>
            ))}
          </dl>
          {npc.backstory && (<><h4>来歴</h4><p className="pre">{npc.backstory}</p></>)}
          {npc.actionRules.length > 0 && (<><h4>行動原則</h4><ul>{npc.actionRules.map((r, i) => <li key={i}>{r}</li>)}</ul></>)}

          <h4>技能・能力値</h4>
          <div className="chips">{npc.classes.map((c, i) => <span key={i} className="chip">{c.name} {c.level}</span>)}</div>
          {npc.stats.length > 0 && (
            <dl className="kv stats">{npc.stats.map(([k, v]) => <div key={k}><dt>{k.toUpperCase()}</dt><dd>{v}</dd></div>)}</dl>
          )}
          {npc.equipment.length > 0 && (
            <><h4>装備</h4><dl className="kv">{npc.equipment.map(([k, v]) => <div key={k}><dt>{k}</dt><dd>{v}</dd></div>)}</dl></>
          )}
          {npc.skills.length > 0 && (
            <><h4>特技</h4><ul>{npc.skills.map((s, i) => <li key={i}><b>{s.name}</b> <span className="ui-muted">{s.description}</span></li>)}</ul></>
          )}
        </>
      )}

      {mode !== 'view' && (
        <form className="npc-edit" onSubmit={(e) => { e.preventDefault(); void save() }}>
          <div className="ui-row">
            <label className="field small">HP (最大 {sheet.hp_max})<input type="number" value={hp} onChange={(e) => setHp(Number(e.target.value) || 0)} /></label>
            <label className="field small">MP (最大 {sheet.mp_max})<input type="number" value={mp} onChange={(e) => setMp(Number(e.target.value) || 0)} /></label>
          </div>
          {mode === 'edit' && (
            <>
              {PERSONA_FIELDS.map((f) => (
                <label key={f.key} className="field">{f.label}
                  <input value={persona.personality[f.key]} onChange={(e) => setP(f.key, e.target.value)} />
                </label>
              ))}
              <label className="field">来歴
                <textarea rows={4} value={persona.backstory} onChange={(e) => setPersona((p) => ({ ...p, backstory: e.target.value }))} />
              </label>
              <label className="field">行動原則 (1 行に 1 つ)
                <textarea rows={4} value={persona.actionRules.join('\n')}
                  onChange={(e) => setPersona((p) => ({ ...p, actionRules: e.target.value.split('\n') }))} />
              </label>
              <div className="ui-sys">ほかの項目 (能力値・装備など) はそのまま残ります。YAML のコメントは保存すると消えます。</div>
            </>
          )}
          {mode === 'raw' && (
            <label className="field">YAML (DEV: キーを消すとペルソナが失われます)
              <textarea className="mono" rows={18} value={raw} onChange={(e) => setRaw(e.target.value)} spellCheck={false} />
            </label>
          )}
          {err && <div className="err">{err}</div>}
          <div className="ui-row">
            <button type="button" onClick={() => setMode('view')}>やめる</button>
            <button className="primary" disabled={saving || busy}>{saving ? '保存しています…' : '保存'}</button>
          </div>
        </form>
      )}
    </div>
  )
}
