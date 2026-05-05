import { useState, useRef, useCallback } from 'react'
import yaml from 'js-yaml'
import type { NPCSheet, NPC, NPCSkill, NPCConsumable, AlbumEntry } from '../types'
import { makePortraitMock, makeAlbumThumb } from '../data/npcs'

interface Props {
  npcSheets: NPCSheet[]
  devMode: boolean
  onSheetsChange: () => void
}

// Parse YAML blob → NPC, mapping actual persona YAML fields to NPC interface
function parseNPC(s: NPCSheet): NPC {
  try {
    const raw = yaml.load(s.YAMLBlob) as Record<string, unknown>
    if (!raw || typeof raw !== 'object') throw new Error('invalid')

    // HP/MP max from YAML (live current from DB)
    const hpMax = typeof raw.hp_max === 'number' ? raw.hp_max : s.HP
    const mpMax = typeof raw.mp_max === 'number' ? raw.mp_max : s.MP

    // class string from classes[] array or class field
    let cls = '—'
    if (typeof raw.class === 'string') cls = raw.class
    else if (Array.isArray(raw.classes) && raw.classes.length > 0) {
      const first = raw.classes[0] as { name?: string }
      cls = first.name ?? '—'
    }

    // lv: explicit or highest class level
    let lv: number | undefined
    if (typeof raw.lv === 'number') lv = raw.lv
    else if (Array.isArray(raw.classes)) {
      const max = Math.max(...(raw.classes as { level?: number }[]).map((c) => c.level ?? 0))
      if (max > 0) lv = max
    }

    // attrs: from attrs{} or stats{}
    let attrs: NPC['attrs'] | undefined
    const attrSrc = (raw.attrs ?? raw.stats) as Record<string, unknown> | undefined
    if (attrSrc && typeof attrSrc === 'object') {
      attrs = {
        STR: Number(attrSrc.STR ?? attrSrc.str ?? 10),
        AGI: Number(attrSrc.AGI ?? attrSrc.agl ?? attrSrc.agi ?? 10),
        DEX: Number(attrSrc.DEX ?? attrSrc.dex ?? 10),
        INT: Number(attrSrc.INT ?? attrSrc.int ?? 10),
        SPR: Number(attrSrc.SPR ?? attrSrc.pow ?? attrSrc.spr ?? 10),
        LUC: Number(attrSrc.LUC ?? attrSrc.luc ?? 10),
      }
    }

    // position: always a string (prevent object render crash)
    let position = '—'
    if (typeof raw.position === 'string') position = raw.position
    else if (raw.position && typeof raw.position === 'object') {
      const pos = raw.position as { x?: number; y?: number }
      position = `(${pos.x ?? 0}, ${pos.y ?? 0})`
    }

    // equip from equip{} or equipment{}
    let equip: NPC['equip'] | undefined
    const eqSrc = (raw.equip ?? raw.equipment) as Record<string, unknown> | undefined
    if (eqSrc && typeof eqSrc === 'object') {
      equip = {
        '武器': String(eqSrc['武器'] ?? eqSrc.weapon ?? '—'),
        '防具': String(eqSrc['防具'] ?? eqSrc.armor ?? '—'),
        '盾': String(eqSrc['盾'] ?? eqSrc.shield ?? '—'),
        '装飾': String(eqSrc['装飾'] ?? eqSrc.accessory ?? '—'),
      }
    }

    // skills: normalize rank/type/note fields
    let skills: NPCSkill[] | undefined
    if (Array.isArray(raw.skills)) {
      skills = (raw.skills as Record<string, unknown>[]).map((sk) => ({
        name: String(sk.name ?? ''),
        rank: typeof sk.rank === 'number' ? sk.rank : 1,
        type: (['combat', 'magic', 'general'].includes(String(sk.type)) ? sk.type : 'general') as NPCSkill['type'],
        note: String(sk.note ?? sk.description ?? ''),
      }))
    }

    // consumables
    let consumables: NPCConsumable[] | undefined
    if (Array.isArray(raw.consumables)) {
      consumables = (raw.consumables as Record<string, unknown>[]).map((c) => ({
        name: String(c.name ?? ''),
        cur: typeof c.cur === 'number' ? c.cur : (typeof c.max === 'number' ? c.max : 1),
        max: typeof c.max === 'number' ? c.max : 1,
      }))
    }

    // inventory
    let inventory: NPC['inventory'] | undefined
    if (Array.isArray(raw.inventory)) {
      inventory = (raw.inventory as Record<string, unknown>[]).map((item) => ({
        name: String(item.name ?? ''),
        qty: typeof item.qty === 'number' ? item.qty : 1,
        note: String(item.note ?? ''),
      }))
    }

    // persona: from structured persona{} block
    let persona: import('../types').NPCPersona | undefined
    if (raw.persona && typeof raw.persona === 'object') {
      const p = raw.persona as Record<string, unknown>
      persona = {
        personality: String(p.personality ?? ''),
        motivation: String(p.motivation ?? ''),
        speech: String(p.speech ?? ''),
        priorities: Array.isArray(p.priorities) ? p.priorities.map(String) : [],
        forbidden: Array.isArray(p.forbidden) ? p.forbidden.map(String) : [],
      }
    }

    return {
      name: String(raw.name ?? s.Name),
      icon: typeof raw.icon === 'string' ? raw.icon : '⚔',
      class: cls,
      race: typeof raw.race === 'string' ? raw.race : undefined,
      lv,
      hp: [s.HP, hpMax],
      mp: [s.MP, mpMax],
      attrs,
      skills,
      equip,
      consumables,
      inventory,
      status: typeof raw.status === 'string' ? raw.status : '正常',
      position,
      persona,
    }
  } catch { /* fall through */ }
  return { name: s.Name, icon: '?', class: '—', hp: [s.HP, s.HP], mp: [s.MP, s.MP], status: '正常', position: '—' }
}

// ── Sub-components ──────────────────────────────────────────────────────────

function RankPips({ rank, max = 5 }: { rank: number; max?: number }) {
  return (
    <div className="skill-rank-display">
      {Array.from({ length: max }, (_, i) => (
        <div key={i} className={`rank-pip ${i < rank ? 'filled' : ''}`} />
      ))}
    </div>
  )
}

function CountPips({ cur, max }: { cur: number; max: number }) {
  const display = Math.min(cur, Math.max(max, 10))
  return (
    <div className="consumable-count">
      {Array.from({ length: display }, (_, i) => (
        <div key={i} className={`count-pip ${i < cur ? 'filled' : ''}`} />
      ))}
      <span style={{ fontSize: 10, color: 'var(--text-dim)', marginLeft: 4 }}>{cur}/{max}</span>
    </div>
  )
}

// ── Album Modal ─────────────────────────────────────────────────────────────

interface AlbumModalProps {
  npcIdx: number
  npc: NPC
  albumData: AlbumEntry[][]
  onClose: () => void
  onChange: (data: AlbumEntry[][]) => void
}

function AlbumModal({ npcIdx, npc, albumData, onClose, onChange }: AlbumModalProps) {
  const fileRef = useRef<HTMLInputElement>(null)
  const entries = albumData[npcIdx] ?? []

  function setActive(id: number) {
    onChange(albumData.map((arr, i) =>
      i !== npcIdx ? arr : arr.map((e) => ({ ...e, active: e.id === id }))
    ))
  }

  function deleteEntry(id: number) {
    const entry = entries.find((e) => e.id === id)
    if (!entry) return
    if (entry.active) { alert('使用中のポートレートは削除できません'); return }
    if (!confirm(`「${entry.note}」を削除しますか？`)) return
    onChange(albumData.map((arr, i) => i !== npcIdx ? arr : arr.filter((e) => e.id !== id)))
  }

  function handleUpload(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0]
    if (!file) return
    const reader = new FileReader()
    reader.onload = (ev) => {
      const newEntry: AlbumEntry = {
        id: Date.now(), note: file.name.replace(/\.[^.]+$/, ''), source: 'upload',
        seed: null, date: new Date().toISOString().slice(0, 10), active: false,
        dataUrl: ev.target?.result as string,
      }
      onChange(albumData.map((arr, i) => i !== npcIdx ? arr : [...arr, newEntry]))
    }
    reader.readAsDataURL(file)
  }

  return (
    <div className="album-overlay visible" onClick={(e) => { if (e.target === e.currentTarget) onClose() }}>
      <div className="album-modal">
        <div className="album-header">
          <div>
            <span className="album-title">PORTRAIT ALBUM</span>
            <span className="album-npc-tag">— {npc.name}</span>
          </div>
          <button className="album-close" onClick={onClose}>✕</button>
        </div>
        <div className="album-body">
          <div className="album-grid">
            {entries.map((entry) => (
              <div key={entry.id} className={`album-card ${entry.active ? 'active-portrait' : ''}`}>
                <img className="album-thumb" src={makeAlbumThumb(npc.name, npc.icon, npcIdx, entry)} alt={entry.note} />
                {entry.active && <div className="album-active-badge">使用中</div>}
                <div className="album-card-actions">
                  {!entry.active && (
                    <button className="album-action-btn set-active" title="プロフィールに設定" onClick={() => setActive(entry.id)}>★</button>
                  )}
                  <button className="album-action-btn delete" title="削除" onClick={() => deleteEntry(entry.id)}>✕</button>
                </div>
                <div className="album-card-info">
                  <div className="album-card-note" title={entry.note}>{entry.note}</div>
                  <div className="album-card-meta">{entry.source === 'comfyui' ? '⚙' : '↑'} {entry.date}</div>
                </div>
              </div>
            ))}
            <div className="album-add-card" onClick={() => fileRef.current?.click()}>
              <span className="album-add-icon">+</span>
              <span>追加</span>
              <input ref={fileRef} type="file" accept="image/*" style={{ display: 'none' }} onChange={handleUpload} />
            </div>
          </div>
        </div>
        <div className="album-footer">
          {entries.length}枚 — 使用中: {entries.filter((e) => e.active).length}枚
        </div>
      </div>
    </div>
  )
}

// ── NPC Detail Panel ─────────────────────────────────────────────────────────

type EditSection = 'hpmp' | 'attr' | 'skill' | 'cons' | 'equip' | 'inv' | null

const ATTR_KEYS = ['STR', 'AGI', 'DEX', 'INT', 'SPR', 'LUC'] as const

interface NpcDetailProps {
  sheet: NPCSheet
  npc: NPC
  onSave: (hp: number, mp: number, updatedNpc: NPC) => void
  devMode?: boolean
  onSaveYaml: (rawYaml: string) => Promise<void>
}

function NpcDetail({ sheet, npc, onSave, devMode, onSaveYaml }: NpcDetailProps) {
  const [editSection, setEditSection] = useState<EditSection>(null)
  const [saving, setSaving] = useState(false)
  const [savedLabel, setSavedLabel] = useState('')

  // Local draft states per section
  const [hpmpDraft, setHpmpDraft] = useState({ hpc: npc.hp[0], hpm: npc.hp[1], mpc: npc.mp[0], mpm: npc.mp[1] })
  const [attrDraft, setAttrDraft] = useState(() => ({ ...npc.attrs } as Record<string, number>))
  const [skillsDraft, setSkillsDraft] = useState<NPCSkill[]>(() => (npc.skills ?? []).map((s) => ({ ...s })))
  const [consDraft, setConsDraft] = useState<NPCConsumable[]>(() => (npc.consumables ?? []).map((c) => ({ ...c })))
  const [equipDraft, setEquipDraft] = useState<Record<string, string>>(() => ({ ...npc.equip } as Record<string, string>))
  const [invDraft, setInvDraft] = useState(() => (npc.inventory ?? []).map((item) => ({ ...item })))

  const [yamlEditOpen, setYamlEditOpen] = useState(false)
  const [yamlDraft, setYamlDraft] = useState('')
  const [yamlError, setYamlError] = useState('')

  const hpPct = npc.hp[1] > 0 ? Math.round(npc.hp[0] / npc.hp[1] * 100) : 0
  const mpPct = npc.mp[1] > 0 ? Math.round(npc.mp[0] / npc.mp[1] * 100) : 0

  function toggle(s: EditSection) {
    setEditSection((prev) => (prev === s ? null : s))
    // Reset drafts outside the updater — React 18 Strict Mode forbids setState inside setState
    if (editSection !== s) {
      if (s === 'hpmp') setHpmpDraft({ hpc: npc.hp[0], hpm: npc.hp[1], mpc: npc.mp[0], mpm: npc.mp[1] })
      if (s === 'attr') setAttrDraft({ ...npc.attrs } as Record<string, number>)
      if (s === 'skill') setSkillsDraft((npc.skills ?? []).map((x) => ({ ...x })))
      if (s === 'cons') setConsDraft((npc.consumables ?? []).map((x) => ({ ...x })))
      if (s === 'equip') setEquipDraft({ ...npc.equip } as Record<string, string>)
      if (s === 'inv') setInvDraft((npc.inventory ?? []).map((x) => ({ ...x })))
    }
  }

  function flash(label: string) {
    setSavedLabel(label)
    setTimeout(() => setSavedLabel(''), 2500)
  }

  async function doSave(patch: Partial<NPC>, hp?: number, mp?: number) {
    setSaving(true)
    const updatedNpc: NPC = { ...npc, ...patch }
    const newHp = hp ?? npc.hp[0]
    const newMp = mp ?? npc.mp[0]
    try {
      await onSave(newHp, newMp, updatedNpc)
      setEditSection(null)
      flash('保存しました')
    } finally {
      setSaving(false)
    }
  }

  function editBtn(s: EditSection, label = '編集') {
    return (
      <button className="section-edit-btn" onClick={() => toggle(s)} disabled={saving}>
        {editSection === s ? 'キャンセル' : label}
      </button>
    )
  }

  return (
    <div className="npc-detail-scroll">

      {/* Saved notice */}
      {savedLabel && (
        <div className="save-notice" style={{ display: 'block', marginBottom: 12 }}>{savedLabel}</div>
      )}

      {/* Header */}
      <div className="npc-detail-header">
        <div className="npc-detail-avatar">{npc.icon || '?'}</div>
        <div>
          <div className="npc-detail-name">{npc.name}</div>
          <div className="npc-detail-subinfo">{npc.race ?? '—'} / {npc.class} — Lv.{npc.lv ?? '—'}</div>
          <div className="npc-status-tags">
            <span className={`status-tag ${npc.status === '正常' ? 'normal' : 'combat'}`}>{npc.status || '—'}</span>
            <span className="status-tag normal">位置: {npc.position || '—'}</span>
          </div>
        </div>
      </div>

      {/* ── HP / MP ── */}
      <div className="stat-block" style={{ marginBottom: 16 }}>
        <div className="stat-block-header">
          <div className="stat-block-title">HP / MP</div>
          {editBtn('hpmp')}
        </div>
        {editSection !== 'hpmp' ? (
          <>
            <div className="hpmp-row">
              <div className="hpmp-label-row"><span className="hpmp-label">HP</span><span className="hpmp-value hp">{npc.hp[0]} / {npc.hp[1]}</span></div>
              <div className="hpmp-bar"><div className="hpmp-bar-fill hp" style={{ width: `${hpPct}%` }} /></div>
            </div>
            <div className="hpmp-row">
              <div className="hpmp-label-row"><span className="hpmp-label">MP</span><span className="hpmp-value mp">{npc.mp[0]} / {npc.mp[1]}</span></div>
              <div className="hpmp-bar"><div className="hpmp-bar-fill mp" style={{ width: `${mpPct}%` }} /></div>
            </div>
          </>
        ) : (
          <div className="edit-form">
            <div className="edit-grid-2">
              {([['hpc', 'HP 現在値'], ['hpm', 'HP 最大値'], ['mpc', 'MP 現在値'], ['mpm', 'MP 最大値']] as const).map(([k, label]) => (
                <div key={k}>
                  <div className="form-label" style={{ fontSize: 9, marginBottom: 3 }}>{label}</div>
                  <input className="edit-input hpmp-input" type="number" min={0}
                    value={hpmpDraft[k]}
                    onChange={(e) => setHpmpDraft((f) => ({ ...f, [k]: +e.target.value }))} />
                </div>
              ))}
            </div>
            <div className="edit-actions">
              <button className="modal-save" style={{ fontSize: 11, padding: '5px 14px' }} disabled={saving}
                onClick={() => doSave({ hp: [hpmpDraft.hpc, hpmpDraft.hpm], mp: [hpmpDraft.mpc, hpmpDraft.mpm] }, hpmpDraft.hpc, hpmpDraft.mpc)}>
                保存
              </button>
            </div>
          </div>
        )}
      </div>

      {/* ── Attributes ── */}
      <div className="stat-block" style={{ marginBottom: 16 }}>
        <div className="stat-block-header">
          <div className="stat-block-title">Attributes</div>
          <span style={{ fontSize: 9, color: 'var(--text-dim)', marginRight: 6 }}>セッション外のみ変更推奨</span>
          {editBtn('attr')}
        </div>
        {editSection !== 'attr' ? (
          npc.attrs ? (
            <div className="attr-grid">
              {ATTR_KEYS.map((k) => (
                <div key={k} className="attr-item">
                  <div className="attr-abbr">{k}</div>
                  <div className="attr-val">{npc.attrs![k] ?? '—'}</div>
                </div>
              ))}
            </div>
          ) : (
            <div style={{ fontSize: 11, color: 'var(--text-dim)' }}>未設定 — personas/*.yaml に記載してください</div>
          )
        ) : (
          <div className="edit-form">
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: 10, marginBottom: 8 }}>
              {ATTR_KEYS.map((k) => (
                <div key={k}>
                  <div className="form-label" style={{ fontSize: 9, marginBottom: 3 }}>{k}</div>
                  <input className="edit-input attr-input" type="number" min={1} max={30}
                    value={attrDraft[k] ?? 10}
                    onChange={(e) => setAttrDraft((d) => ({ ...d, [k]: +e.target.value }))} />
                </div>
              ))}
            </div>
            <div className="edit-actions">
              <button className="modal-save" style={{ fontSize: 11, padding: '5px 14px' }} disabled={saving}
                onClick={() => doSave({ attrs: attrDraft as NPC['attrs'] })}>保存</button>
            </div>
          </div>
        )}
      </div>

      {/* ── Skills ── */}
      <div className="stat-block" style={{ marginBottom: 16 }}>
        <div className="stat-block-header">
          <div className="stat-block-title">Skills</div>
          {npc.skills && npc.skills.length > 0 && editBtn('skill')}
        </div>
        {editSection !== 'skill' ? (
          npc.skills && npc.skills.length > 0 ? (
            npc.skills.map((s) => (
              <div key={s.name} className="skill-detail-row">
                <div className="skill-detail-header">
                  <span className="skill-detail-name">{s.name}</span>
                  <span className={`skill-type-badge ${s.type}`}>{s.type}</span>
                </div>
                <div className="skill-rank-row">
                  <span className="skill-rank-label">Rank</span>
                  <span className="skill-rank-num">{s.rank}</span>
                  <RankPips rank={s.rank} />
                </div>
                {s.note && <div className="skill-abilities">{s.note}</div>}
              </div>
            ))
          ) : (
            <div style={{ fontSize: 11, color: 'var(--text-dim)' }}>未設定</div>
          )
        ) : (
          <div className="edit-form">
            {skillsDraft.map((s, si) => (
              <div key={si} style={{ marginBottom: 12, paddingBottom: 12, borderBottom: '1px solid var(--border)' }}>
                <div style={{ fontSize: 12, fontWeight: 500, marginBottom: 8, color: 'var(--text-primary)' }}>{s.name}</div>
                <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 6 }}>
                  <div className="form-label" style={{ fontSize: 9, margin: 0, width: 40 }}>Rank</div>
                  <input className="edit-input rank-input" type="number" min={1} max={7} value={s.rank}
                    onChange={(e) => setSkillsDraft((d) => d.map((x, i) => i === si ? { ...x, rank: +e.target.value } : x))} />
                  <span style={{ fontSize: 10, color: 'var(--text-dim)' }}>1〜7</span>
                </div>
                <div>
                  <div className="form-label" style={{ fontSize: 9, marginBottom: 3 }}>習得アビリティメモ</div>
                  <input className="edit-input text-input" type="text" value={s.note || ''}
                    onChange={(e) => setSkillsDraft((d) => d.map((x, i) => i === si ? { ...x, note: e.target.value } : x))} />
                </div>
              </div>
            ))}
            <div className="edit-actions">
              <button className="modal-save" style={{ fontSize: 11, padding: '5px 14px' }} disabled={saving}
                onClick={() => doSave({ skills: skillsDraft })}>保存</button>
            </div>
          </div>
        )}
      </div>

      {/* ── Consumables + Equipment (side by side) ── */}
      <div className="npc-stats-grid">
        {/* Consumables */}
        <div className="stat-block">
          <div className="stat-block-header">
            <div className="stat-block-title">Consumables</div>
            {npc.consumables && npc.consumables.length > 0 && editBtn('cons')}
          </div>
          {editSection !== 'cons' ? (
            npc.consumables && npc.consumables.length > 0 ? (
              <div className="consumable-list">
                {npc.consumables.map((c) => (
                  <div key={c.name} className="consumable-row">
                    <span className="consumable-name">{c.name}</span>
                    <CountPips cur={c.cur} max={c.max} />
                  </div>
                ))}
              </div>
            ) : (
              <div style={{ fontSize: 11, color: 'var(--text-dim)' }}>未設定</div>
            )
          ) : (
            <div>
              {consDraft.map((c, ci) => (
                <div key={ci} style={{ display: 'flex', alignItems: 'center', gap: 6, marginBottom: 7, fontSize: 12 }}>
                  <span style={{ flex: 1, color: 'var(--text-secondary)', fontSize: 11 }}>{c.name}</span>
                  <input className="edit-input qty-input" type="number" min={0} max={c.max} value={c.cur} title="現在"
                    onChange={(e) => setConsDraft((d) => d.map((x, i) => i === ci ? { ...x, cur: +e.target.value } : x))} />
                  <span style={{ color: 'var(--text-dim)', fontSize: 10 }}>/</span>
                  <input className="edit-input qty-input" type="number" min={0} max={99} value={c.max} title="最大"
                    onChange={(e) => setConsDraft((d) => d.map((x, i) => i === ci ? { ...x, max: +e.target.value } : x))} />
                </div>
              ))}
              <div className="edit-actions" style={{ marginTop: 6 }}>
                <button className="modal-save" style={{ fontSize: 11, padding: '5px 14px' }} disabled={saving}
                  onClick={() => doSave({ consumables: consDraft })}>保存</button>
              </div>
            </div>
          )}
        </div>

        {/* Equipment */}
        <div className="stat-block">
          <div className="stat-block-header">
            <div className="stat-block-title">Equipment</div>
            {npc.equip && editBtn('equip')}
          </div>
          {editSection !== 'equip' ? (
            npc.equip ? (
              <div className="equip-list">
                {(Object.entries(npc.equip) as [string, string][]).map(([slot, val]) => (
                  <div key={slot} className="equip-row">
                    <span className="equip-slot">{slot}</span>
                    <span className="equip-name">{val}</span>
                  </div>
                ))}
              </div>
            ) : (
              <div style={{ fontSize: 11, color: 'var(--text-dim)' }}>未設定</div>
            )
          ) : (
            <div>
              {Object.keys(npc.equip ?? {}).map((slot) => (
                <div key={slot} style={{ marginBottom: 8 }}>
                  <div className="form-label" style={{ fontSize: 9, marginBottom: 3 }}>{slot}</div>
                  <input className="edit-input text-input" type="text" value={equipDraft[slot] ?? ''}
                    onChange={(e) => setEquipDraft((d) => ({ ...d, [slot]: e.target.value }))} />
                </div>
              ))}
              <div className="edit-actions" style={{ marginTop: 4 }}>
                <button className="modal-save" style={{ fontSize: 11, padding: '5px 14px' }} disabled={saving}
                  onClick={() => doSave({ equip: equipDraft as NPC['equip'] })}>保存</button>
              </div>
            </div>
          )}
        </div>
      </div>

      {/* ── Inventory ── */}
      <div className="stat-block" style={{ marginBottom: 16, marginTop: 16 }}>
        <div className="stat-block-header">
          <div className="stat-block-title">Inventory（所持品）</div>
          {npc.inventory && npc.inventory.length > 0 && editBtn('inv')}
        </div>
        {editSection !== 'inv' ? (
          npc.inventory && npc.inventory.length > 0 ? (
            <div className="inventory-list">
              {npc.inventory.map((item, i) => (
                <div key={i} className="inventory-row">
                  <span className="inventory-name">{item.name}</span>
                  <span className="inventory-qty">×{item.qty}</span>
                  {item.note && <span className="inventory-note">{item.note}</span>}
                </div>
              ))}
            </div>
          ) : (
            <div style={{ fontSize: 11, color: 'var(--text-dim)' }}>未設定</div>
          )
        ) : (
          <div>
            {invDraft.map((item, ii) => (
              <div key={ii} style={{ display: 'flex', alignItems: 'center', gap: 6, marginBottom: 7 }}>
                <input className="edit-input text-input" type="text" value={item.name} style={{ flex: 2 }}
                  onChange={(e) => setInvDraft((d) => d.map((x, i) => i === ii ? { ...x, name: e.target.value } : x))} />
                <span style={{ color: 'var(--text-dim)', fontSize: 10 }}>×</span>
                <input className="edit-input qty-input" type="number" min={0} value={item.qty}
                  onChange={(e) => setInvDraft((d) => d.map((x, i) => i === ii ? { ...x, qty: +e.target.value } : x))} />
                <input className="edit-input text-input" type="text" value={item.note} placeholder="メモ" style={{ flex: 1.5, fontSize: 11 }}
                  onChange={(e) => setInvDraft((d) => d.map((x, i) => i === ii ? { ...x, note: e.target.value } : x))} />
                <button style={{ background: 'none', border: 'none', color: 'var(--text-dim)', cursor: 'pointer', fontSize: 14, padding: '0 2px' }}
                  onClick={() => setInvDraft((d) => d.filter((_, i) => i !== ii))}>✕</button>
              </div>
            ))}
            <button className="ss-add-btn" style={{ marginTop: 4, marginBottom: 8 }}
              onClick={() => setInvDraft((d) => [...d, { name: '', qty: 1, note: '' }])}>
              + アイテム追加
            </button>
            <div className="edit-actions">
              <button className="modal-save" style={{ fontSize: 11, padding: '5px 14px' }} disabled={saving}
                onClick={() => doSave({ inventory: invDraft })}>保存</button>
            </div>
          </div>
        )}
      </div>

      {/* ── Persona (read-only) ── */}
      <div style={{ display: 'flex', alignItems: 'center', marginBottom: 10 }}>
        <div className="stat-block-title" style={{ fontFamily: 'var(--font-display)', fontSize: 9, letterSpacing: '0.2em', color: 'var(--gold-dim)', textTransform: 'uppercase' }}>Persona</div>
        <span className="persona-lock-note">🔒 編集不可 — personas/*.yaml を直接編集してください</span>
      </div>
      {npc.persona ? (
        <>
          <div className="persona-block">
            <div className="persona-field"><div className="persona-label">性格</div><div className="persona-value">{npc.persona.personality}</div></div>
            <div className="persona-field"><div className="persona-label">動機</div><div className="persona-value">{npc.persona.motivation}</div></div>
            <div className="persona-field"><div className="persona-label">口調</div><div className="persona-value">{npc.persona.speech}</div></div>
          </div>
          <div className="stat-block" style={{ marginBottom: 16 }}>
            <div className="stat-block-title">行動優先順位</div>
            <div className="priorities-list">
              {npc.persona.priorities.map((p, i) => (
                <div key={i} className="priority-item"><span className="priority-num">{i + 1}.</span>{p}</div>
              ))}
            </div>
          </div>
          <div className="stat-block" style={{ marginBottom: 24 }}>
            <div className="stat-block-title">禁止行動</div>
            <div className="priorities-list">
              {npc.persona.forbidden.map((f, i) => (
                <div key={i} className="priority-item">
                  <span style={{ color: 'var(--red)', fontSize: 11, flexShrink: 0 }}>✕</span>{f}
                </div>
              ))}
            </div>
          </div>
        </>
      ) : (
        <div style={{ fontSize: 11, color: 'var(--text-dim)', marginBottom: 24 }}>未設定 — personas/*.yaml に persona: セクションを追加してください</div>
      )}

      {/* ── DEV: Raw YAML Edit ── */}
      {devMode && (
        <div className="stat-block dev-yaml-block" style={{ marginTop: 16 }}>
          <div className="stat-block-header" style={{ borderBottom: '1px solid var(--amber)', marginBottom: 8 }}>
            <div className="stat-block-title" style={{ color: 'var(--amber)' }}>⚠ DEV — Raw YAML 直接編集</div>
            <button
              className="section-edit-btn"
              style={{ color: 'var(--amber)', borderColor: 'var(--amber)' }}
              onClick={() => {
                if (!yamlEditOpen) { setYamlDraft(sheet.YAMLBlob); setYamlError('') }
                setYamlEditOpen((v) => !v)
              }}
            >
              {yamlEditOpen ? '閉じる' : '編集する'}
            </button>
          </div>
          {yamlEditOpen && (
            <div className="edit-form">
              <textarea
                className="dev-yaml-textarea"
                value={yamlDraft}
                onChange={(e) => { setYamlDraft(e.target.value); setYamlError('') }}
                rows={20}
                spellCheck={false}
              />
              {yamlError && (
                <div style={{ color: 'var(--red)', fontSize: 11, marginTop: 6 }}>✕ {yamlError}</div>
              )}
              <div className="edit-actions" style={{ marginTop: 8 }}>
                <button
                  className="modal-save"
                  style={{ fontSize: 11, padding: '5px 14px', background: 'rgba(255,165,0,0.15)', borderColor: 'var(--amber)', color: 'var(--amber)' }}
                  disabled={saving}
                  onClick={async () => {
                    try {
                      yaml.load(yamlDraft) // validate
                    } catch (e) {
                      setYamlError(e instanceof Error ? e.message : 'YAML構文エラー')
                      return
                    }
                    setSaving(true)
                    try {
                      await onSaveYaml(yamlDraft)
                      setYamlEditOpen(false)
                      flash('YAMLを保存しました')
                    } finally {
                      setSaving(false)
                    }
                  }}
                >
                  YAMLを保存
                </button>
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  )
}

// ── Main NPCView ─────────────────────────────────────────────────────────────

export default function NPCView({ npcSheets, devMode, onSheetsChange }: Props) {
  // ── All hooks must be called unconditionally ──
  const [activeIdx, setActiveIdx] = useState(0)
  const [portraits, setPortraits] = useState<import('../types').PortraitEntry[]>(() =>
    npcSheets.map((s) => ({ name: s.Name, prompt: '', seed: null, generated: false, dataUrl: null }))
  )
  const [albumData, setAlbumData] = useState<AlbumEntry[][]>(() => npcSheets.map(() => []))
  const [generating, setGenerating] = useState(false)
  const [showAlbum, setShowAlbum] = useState(false)
  const [promptEditing, setPromptEditing] = useState(false)
  const [promptDraft, setPromptDraft] = useState('')
  const uploadRef = useRef<HTMLInputElement>(null)

  // sheetId passed as arg so this callback doesn't depend on derived values below the guard
  const handleSave = useCallback(async (sheetId: number, hp: number, mp: number, updatedNpc: NPC) => {
    const yamlBlob = yaml.dump(updatedNpc, { lineWidth: -1, quotingType: '"' })
    await fetch(`/api/npc-sheet/${sheetId}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ hp, mp, yaml_blob: yamlBlob }),
    })
    onSheetsChange()
  }, [onSheetsChange])

  const handleSaveYaml = useCallback(async (sheetId: number, currentHp: number, currentMp: number, rawYaml: string) => {
    // Try to extract hp/mp from the YAML for the DB columns
    let hp = currentHp, mp = currentMp
    try {
      const parsed = yaml.load(rawYaml) as Record<string, unknown>
      if (typeof parsed.hp === 'number') hp = parsed.hp
      if (typeof parsed.mp === 'number') mp = parsed.mp
    } catch { /* keep current values */ }
    await fetch(`/api/npc-sheet/${sheetId}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ hp, mp, yaml_blob: rawYaml }),
    })
    onSheetsChange()
  }, [onSheetsChange])

  // ── Guard: no NPCs ──
  if (npcSheets.length === 0) {
    return (
      <div style={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center', color: 'var(--text-dim)', flexDirection: 'column', gap: 12 }}>
        <div style={{ fontSize: 32 }}>⚜</div>
        <div>パーティメンバーがいません</div>
        <div style={{ fontSize: 11 }}>INFORMATIONページでNPCを追加してください</div>
      </div>
    )
  }

  const safeIdx = Math.min(activeIdx, npcSheets.length - 1)
  const sheet = npcSheets[safeIdx]
  const npc = parseNPC(sheet)
  const p = portraits[safeIdx] ?? { name: sheet.Name, prompt: '', seed: null, generated: false, dataUrl: null }

  const activeAlbumEntry = (albumData[safeIdx] ?? []).find((e) => e.active)
  const displayUrl = activeAlbumEntry?.dataUrl ?? (p.generated ? p.dataUrl : null)

  function generatePortrait() {
    setGenerating(true)
    setTimeout(() => {
      const { seed, dataUrl } = makePortraitMock(npc.name, npc.icon, safeIdx)
      const newEntry: AlbumEntry = {
        id: Date.now(), note: `生成 seed:${seed}`, source: 'comfyui', seed,
        date: new Date().toISOString().slice(0, 10), active: true, dataUrl,
      }
      setAlbumData((prev) => prev.map((arr, i) =>
        i !== safeIdx ? arr : [...arr.map((e) => ({ ...e, active: false })), newEntry]
      ))
      setPortraits((prev) => prev.map((x, i) => i !== safeIdx ? x : { ...x, generated: true, seed, dataUrl }))
      setGenerating(false)
    }, 2000)
  }

  function uploadPortrait(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0]
    if (!file) return
    const reader = new FileReader()
    reader.onload = (ev) => {
      const dataUrl = ev.target?.result as string
      const newEntry: AlbumEntry = {
        id: Date.now(), note: file.name.replace(/\.[^.]+$/, ''), source: 'upload', seed: null,
        date: new Date().toISOString().slice(0, 10), active: true, dataUrl,
      }
      setAlbumData((prev) => prev.map((arr, i) =>
        i !== safeIdx ? arr : [...arr.map((e) => ({ ...e, active: false })), newEntry]
      ))
      setPortraits((prev) => prev.map((x, i) => i !== safeIdx ? x : { ...x, generated: true, dataUrl }))
    }
    reader.readAsDataURL(file)
  }

  function togglePrompt() {
    if (!promptEditing) { setPromptDraft(p.prompt); setPromptEditing(true) }
    else {
      setPortraits((prev) => prev.map((x, i) => i !== safeIdx ? x : { ...x, prompt: promptDraft }))
      setPromptEditing(false)
    }
  }

  // Bind sheet.ID here for NpcDetail
  const boundSave = (hp: number, mp: number, updatedNpc: NPC) => handleSave(sheet.ID, hp, mp, updatedNpc)
  const boundSaveYaml = (rawYaml: string) => handleSaveYaml(sheet.ID, sheet.HP, sheet.MP, rawYaml)

  return (
    <div id="npc-portrait-panel-wrap">

      {/* ── Portrait Panel ── */}
      <div id="npc-portrait-panel">
        <div className="portrait-frame">
          <div className="portrait-title">{npc.name} — PORTRAIT</div>
          <div className="portrait-canvas">
            {displayUrl
              ? <img id="portrait-img" src={displayUrl} alt="portrait" className="loaded" />
              : (
                <div className="portrait-placeholder">
                  <svg viewBox="0 0 100 180" xmlns="http://www.w3.org/2000/svg" fill="var(--text-primary)">
                    <ellipse cx="50" cy="22" rx="16" ry="18" />
                    <path d="M28 55 Q30 42 50 40 Q70 42 72 55 L78 110 Q65 115 50 116 Q35 115 22 110 Z" />
                    <path d="M28 58 Q18 72 14 95 Q12 105 16 108 Q20 110 22 100 L26 78 Z" />
                    <path d="M72 58 Q82 72 86 95 Q88 105 84 108 Q80 110 78 100 L74 78 Z" />
                    <path d="M35 114 L30 160 Q29 168 34 168 Q38 168 40 160 L44 114 Z" />
                    <path d="M65 114 L70 160 Q71 168 66 168 Q62 168 60 160 L56 114 Z" />
                  </svg>
                  <div className="portrait-placeholder-label">NO IMAGE</div>
                </div>
              )
            }
            {generating && (
              <div className="portrait-generating active">
                <div className="gen-spinner" />
                <span>ComfyUI 生成中…</span>
              </div>
            )}
          </div>

          <div className="portrait-actions">
            <button className="portrait-gen-btn" onClick={generatePortrait} disabled={generating}>⚙ ComfyUI で生成</button>
            <label className="portrait-upload-btn" style={{ cursor: 'pointer' }}>
              ↑ 画像をアップロード
              <input ref={uploadRef} type="file" accept="image/*" style={{ display: 'none' }} onChange={uploadPortrait} />
            </label>
            <button className="portrait-album-btn" onClick={() => setShowAlbum(true)}>☰ アルバムを開く</button>
          </div>

          <div className="portrait-prompt-area">
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 5 }}>
              <div className="portrait-prompt-label">Prompt</div>
              <button style={{ background: 'none', border: 'none', color: 'var(--text-dim)', fontSize: 10, cursor: 'pointer', padding: 0 }} onClick={togglePrompt}>
                {promptEditing ? '確定' : '編集'}
              </button>
            </div>
            {!promptEditing
              ? <div className="portrait-prompt-text">{p.prompt || <span style={{ color: 'var(--text-dim)' }}>プロンプト未設定</span>}</div>
              : <textarea className="portrait-prompt-edit visible" value={promptDraft} onChange={(e) => setPromptDraft(e.target.value)} />
            }
          </div>

          <div className="portrait-meta">
            {p.seed
              ? <><span>Seed: </span><span>{p.seed}</span><br /><span>Model: </span><span>stable-diffusion-xl</span></>
              : <span style={{ color: 'var(--text-dim)' }}>未生成</span>
            }
          </div>
        </div>
      </div>

      {/* ── NPC List ── */}
      <div id="npc-list">
        <div className="npc-list-title">NPC ROSTER</div>
        {npcSheets.map((s, i) => {
          const n = parseNPC(s)
          const hpLow = n.hp[1] > 0 && n.hp[0] / n.hp[1] < 0.4
          return (
            <div
              key={s.ID}
              className={`npc-list-item ${i === safeIdx ? 'active' : ''}`}
              onClick={() => { setActiveIdx(i); setShowAlbum(false) }}
            >
              <div className="npc-avatar">{n.icon || '?'}</div>
              <div className="npc-list-info">
                <div className="npc-list-name">{n.name}</div>
                <div className="npc-list-class">{n.class}</div>
              </div>
              <div className="npc-hp-tiny">
                <div className="npc-hp-val" style={{ color: hpLow ? '#ff9900' : undefined }}>
                  {n.hp[0]}/{n.hp[1]}
                </div>
              </div>
            </div>
          )
        })}
      </div>

      {/* ── NPC Detail ── */}
      <div id="npc-detail">
        <NpcDetail
          key={sheet.ID}
          sheet={sheet}
          npc={npc}
          onSave={boundSave}
          devMode={devMode}
          onSaveYaml={boundSaveYaml}
        />
      </div>

      {showAlbum && (
        <AlbumModal
          npcIdx={safeIdx}
          npc={npc}
          albumData={albumData}
          onClose={() => setShowAlbum(false)}
          onChange={setAlbumData}
        />
      )}
    </div>
  )
}
