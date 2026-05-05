import { useState, useEffect } from 'react'
import type { NPCSheet, Checkpoint, PlayerCharacterEntry, PCSheet } from '../types'
import PCEditView, { makeEmptySheet } from './PCEditView'

interface Props {
  npcSheets: NPCSheet[]
  checkpoints: Checkpoint[]
  onStart: () => void
  onRestore: (turn: number) => void
  onSheetsChange: () => void
  devMode?: boolean
}

interface PersonaInfo {
  id: string
  name: string
  hp: number
  mp: number
}

interface SnapshotSheet { Name: string; HP: number; MP: number }

const SESSION_ID = 1

function parseSnapshot(json: string): SnapshotSheet[] {
  try { return (JSON.parse(json).npc_sheets ?? []) as SnapshotSheet[] } catch { return [] }
}

function formatDate(iso: string): string {
  if (!iso) return '—'
  const d = new Date(iso)
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')} ${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

function swapSuggestion(sheets: NPCSheet[]): string {
  if (sheets.length < 2) return ''
  const sorted = [...sheets].sort((a, b) => b.HP / Math.max(b.MP, 1) - a.HP / Math.max(a.MP, 1))
  if (sorted[0].Name === sheets[0].Name) return ''
  return `${sorted[0].Name} → 前衛  /  ${sorted[sorted.length - 1].Name} → 後衛 を推奨`
}

function parsePCSheet(entry: PlayerCharacterEntry): PCSheet {
  try { return JSON.parse(entry.json_blob) as PCSheet } catch { return makeEmptySheet() }
}

export default function SessionStartView({ npcSheets, checkpoints, onStart, onRestore, onSheetsChange, devMode }: Props) {
  const [personas, setPersonas] = useState<PersonaInfo[]>([])
  const [showNPCAdd, setShowNPCAdd] = useState(false)
  const [busy, setBusy] = useState(false)

  // PC state
  const [pcList, setPcList] = useState<PlayerCharacterEntry[]>([])
  const [showPCList, setShowPCList] = useState(false)
  const [editingPC, setEditingPC] = useState<{ id: number | null; sheet: PCSheet } | null>(null)

  useEffect(() => {
    fetch('/api/personas').then((r) => r.json()).then((d: PersonaInfo[]) => setPersonas(d ?? [])).catch(() => {})
    loadPCs()
  }, [])

  async function loadPCs() {
    try {
      const res = await fetch('/api/player-characters')
      if (res.ok) setPcList(await res.json())
    } catch { /* offline */ }
  }

  // ── DEV: checkpoint delete ───────────────────────────────────────────────

  async function deleteCheckpoint(id: number) {
    await fetch(`/api/checkpoint/${id}`, { method: 'DELETE' })
    await onSheetsChange()
  }

  // ── NPC ops ──────────────────────────────────────────────────────────────

  async function removeNPC(name: string) {
    setBusy(true)
    await fetch('/api/npc-sheet', {
      method: 'DELETE',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ session_id: SESSION_ID, name }),
    })
    await onSheetsChange()
    setBusy(false)
  }

  async function addNPC(personaId: string) {
    setBusy(true)
    await fetch('/api/npc-sheet', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ session_id: SESSION_ID, persona_id: personaId }),
    })
    await onSheetsChange()
    setShowNPCAdd(false)
    setBusy(false)
  }

  // ── PC ops ───────────────────────────────────────────────────────────────

  async function activatePC(id: number) {
    await fetch(`/api/player-characters/${id}/activate`, { method: 'PATCH' })
    await loadPCs()
    setShowPCList(false)
  }

  async function deactivatePC(id: number) {
    await fetch(`/api/player-characters/${id}/deactivate`, { method: 'PATCH' })
    await loadPCs()
  }

  async function deletePC(id: number) {
    if (!confirm('このキャラクターを削除しますか？')) return
    await fetch(`/api/player-characters/${id}`, { method: 'DELETE' })
    await loadPCs()
  }

  async function savePC(id: number | null, sheet: PCSheet) {
    const body = { name: sheet.name || 'PC', json_blob: JSON.stringify(sheet) }
    if (id === null) {
      const res = await fetch('/api/player-characters', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      })
      if (res.ok) {
        const data = await res.json()
        await fetch(`/api/player-characters/${data.id}/activate`, { method: 'PATCH' })
      }
    } else {
      await fetch(`/api/player-characters/${id}`, {
        method: 'PUT', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      })
    }
    await loadPCs()
    setEditingPC(null)
  }

  // ── Derived ──────────────────────────────────────────────────────────────

  const latestCp = checkpoints[0] ?? null
  const cpSheets = latestCp ? parseSnapshot(latestCp.SnapshotJSON) : []
  const suggestion = swapSuggestion(npcSheets)
  const currentNPCNames = new Set(npcSheets.map((s) => s.Name))
  const availableToAdd = personas.filter((p) => !currentNPCNames.has(p.name))
  const activePC = pcList.find((p) => p.is_active) ?? null
  const inactivePCs = pcList.filter((p) => !p.is_active)

  return (
    <div className="session-start-wrap">
      <div className="session-start-view">
        <div>
          <div className="ss-title">INFORMATION</div>
          <div className="ss-subtitle">セッションを準備してください</div>
        </div>

        {/* ── 1. QUICK LOAD ── */}
        <div className="ss-section">
          <div className="ss-section-title">CHECKPOINT — クイックロード</div>
          {checkpoints.length === 0 ? (
            <div style={{ fontSize: 12, color: 'var(--text-dim)' }}>チェックポイントがありません</div>
          ) : devMode ? (
            // DEV: show all checkpoints with delete buttons
            <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
              {checkpoints.map((cp) => (
                <div key={cp.ID} className="ss-card" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: 8 }}>
                  <div>
                    <div style={{ fontSize: 12, fontWeight: 600 }}>Turn {cp.Turn}</div>
                    <div style={{ fontSize: 10, color: 'var(--text-dim)' }}>{formatDate(cp.CreatedAt)}</div>
                  </div>
                  <div style={{ display: 'flex', gap: 4 }}>
                    <button className="checkpoint-restore-btn" style={{ padding: '3px 10px', fontSize: 11 }} onClick={() => onRestore(cp.Turn)}>
                      ロード
                    </button>
                    <button className="ss-remove-btn dev-delete-btn" onClick={() => deleteCheckpoint(cp.ID)} title="チェックポイントを削除">✕</button>
                  </div>
                </div>
              ))}
            </div>
          ) : (
            latestCp && (
              <div className="ss-card">
                <div className="ss-card-row">
                  <span className="ss-card-label">Turn</span>
                  <span className="ss-card-value">{latestCp.Turn}</span>
                </div>
                <div className="ss-card-row">
                  <span className="ss-card-label">保存日時</span>
                  <span className="ss-card-value">{formatDate(latestCp.CreatedAt)}</span>
                </div>
                {cpSheets.length > 0 && (
                  <div style={{ marginTop: 8, display: 'flex', flexDirection: 'column', gap: 4 }}>
                    {cpSheets.map((s) => (
                      <div key={s.Name} style={{ display: 'flex', gap: 12, fontSize: 11, color: 'var(--text-secondary)' }}>
                        <span style={{ width: 80, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{s.Name}</span>
                        <span>HP {s.HP}</span>
                        <span>MP {s.MP}</span>
                      </div>
                    ))}
                  </div>
                )}
                <div style={{ marginTop: 10 }}>
                  <button className="checkpoint-restore-btn" onClick={() => onRestore(latestCp.Turn)}>
                    クイックロード
                  </button>
                </div>
              </div>
            )
          )}
        </div>

        {/* ── 2. PLAYER CHARACTER ── */}
        <div className="ss-section">
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16, paddingBottom: 8, borderBottom: '1px solid var(--border)' }}>
            <div className="ss-section-title" style={{ margin: 0, padding: 0, border: 'none' }}>PLAYER CHARACTER</div>
            <div style={{ display: 'flex', gap: 6 }}>
              {pcList.length > 1 && (
                <button className="ss-edit-btn" onClick={() => setShowPCList((v) => !v)}>
                  ⇄ 切替
                </button>
              )}
              {activePC && (
                <button className="ss-edit-btn" onClick={() => setEditingPC({ id: activePC.id, sheet: parsePCSheet(activePC) })}>
                  ✎ EDIT
                </button>
              )}
              <button className="ss-edit-btn" onClick={() => setEditingPC({ id: null, sheet: makeEmptySheet() })}>
                + NEW
              </button>
            </div>
          </div>

          {/* PC switcher */}
          {showPCList && inactivePCs.length > 0 && (
            <div className="ss-card" style={{ marginBottom: 10 }}>
              <div style={{ fontSize: 11, color: 'var(--text-dim)', marginBottom: 8 }}>キャラクターを選択</div>
              {inactivePCs.map((pc) => {
                const sheet = parsePCSheet(pc)
                const cls = sheet.classes?.[0]
                return (
                  <div key={pc.id} className="ss-persona-row">
                    <div>
                      <span style={{ fontSize: 12, fontWeight: 600 }}>{pc.name}</span>
                      <span style={{ fontSize: 10, color: 'var(--text-dim)', marginLeft: 8 }}>
                        {sheet.race} {cls ? `${cls.name}Lv${cls.level}` : ''}
                      </span>
                    </div>
                    <div style={{ display: 'flex', gap: 4 }}>
                      <button className="ss-add-btn" onClick={() => activatePC(pc.id)}>選択</button>
                      <button className="ss-remove-btn" onClick={() => deletePC(pc.id)} style={{ fontSize: 11 }}>×</button>
                    </div>
                  </div>
                )
              })}
            </div>
          )}

          {/* Active PC display */}
          {activePC ? (() => {
            const sheet = parsePCSheet(activePC)
            const vitTotal = sheet.attrs.vit.base + sheet.attrs.vit.growth
            return (
              <div className="ss-card" style={{ borderLeft: '3px solid var(--green)' }}>
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start' }}>
                  <div>
                    <div style={{ fontSize: 15, fontWeight: 700, color: 'var(--green)' }}>{sheet.name || '名前未設定'}</div>
                    <div style={{ fontSize: 11, color: 'var(--text-dim)', marginTop: 2 }}>
                      {sheet.race} / 冒険者Lv {sheet.adventurerLevel}
                      {sheet.classes.length > 0 && ' / ' + sheet.classes.map((c) => `${c.name}${c.level}`).join(' ')}
                    </div>
                  </div>
                  <button className="ss-remove-btn" style={{ fontSize: 11 }} onClick={() => deactivatePC(activePC.id)} title="選択解除">×</button>
                </div>
                <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr 1fr', gap: '6px 16px', marginTop: 10, fontSize: 11 }}>
                  <div><span style={{ color: 'var(--text-dim)' }}>HP </span><strong>{sheet.hpCurrent}/{sheet.hpMax}</strong></div>
                  <div><span style={{ color: 'var(--text-dim)' }}>MP </span><strong>{sheet.mpCurrent}/{sheet.mpMax}</strong></div>
                  <div><span style={{ color: 'var(--text-dim)' }}>生命力 </span><strong>{vitTotal}</strong></div>
                  {(['dex','agi','str','vit','int','spr'] as const).map((k) => {
                    const label = { dex:'器用', agi:'敏捷', str:'筋力', vit:'生命', int:'知力', spr:'精神' }[k]
                    const total = sheet.attrs[k].base + sheet.attrs[k].growth
                    return <div key={k}><span style={{ color: 'var(--text-dim)' }}>{label} </span><strong>{total}</strong><span style={{ color: 'var(--text-dim)', fontSize: 10 }}>（+{Math.floor(total/6)}）</span></div>
                  })}
                </div>
                {sheet.combatFeats && (
                  <div style={{ marginTop: 8, fontSize: 10, color: 'var(--text-dim)' }}>
                    戦闘特技: {sheet.combatFeats}
                  </div>
                )}
                <div style={{ marginTop: 4, fontSize: 10, color: 'var(--text-dim)' }}>
                  所持金: {sheet.gold} G　言語: {sheet.languages}
                </div>
              </div>
            )
          })() : (
            <div style={{ fontSize: 12, color: 'var(--text-dim)' }}>
              PCが選択されていません。「+ NEW」でキャラクターを作成してください。
            </div>
          )}
        </div>

        {/* ── 3. PARTY MEMBERS ── */}
        <div className="ss-section">
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16, paddingBottom: 8, borderBottom: '1px solid var(--border)' }}>
            <div className="ss-section-title" style={{ margin: 0, padding: 0, border: 'none' }}>PARTY MEMBERS</div>
            <button
              className="ss-edit-btn"
              onClick={() => setShowNPCAdd((v) => !v)}
              disabled={busy || availableToAdd.length === 0}
            >+ ADD</button>
          </div>

          {showNPCAdd && (
            <div className="ss-card" style={{ marginBottom: 10 }}>
              <div style={{ fontSize: 11, color: 'var(--text-dim)', marginBottom: 8 }}>追加するキャラクターを選択</div>
              {availableToAdd.map((p) => (
                <div key={p.id} className="ss-persona-row">
                  <div>
                    <span style={{ fontSize: 12, fontWeight: 600 }}>{p.name}</span>
                    <span style={{ fontSize: 10, color: 'var(--text-dim)', marginLeft: 8 }}>HP {p.hp} / MP {p.mp}</span>
                  </div>
                  <button className="ss-add-btn" onClick={() => addNPC(p.id)} disabled={busy}>追加</button>
                </div>
              ))}
            </div>
          )}

          {npcSheets.length === 0 ? (
            <div style={{ fontSize: 12, color: 'var(--text-dim)' }}>パーティーメンバーがいません</div>
          ) : (
            <>
              <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                {npcSheets.map((s) => (
                  <div key={s.ID} className="ss-card ss-card-npc" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                    <div>
                      <div style={{ fontWeight: 600, fontSize: 13 }}>{s.Name}</div>
                      <div style={{ display: 'flex', gap: 16, marginTop: 4, fontSize: 11, color: 'var(--text-secondary)' }}>
                        <span>HP <strong>{s.HP}</strong></span>
                        <span>MP <strong>{s.MP}</strong></span>
                      </div>
                    </div>
                    <button className="ss-remove-btn" onClick={() => removeNPC(s.Name)} disabled={busy} title={`${s.Name}をパーティから外す`}>×</button>
                  </div>
                ))}
              </div>
              {suggestion && (
                <div style={{ marginTop: 8, fontSize: 11, padding: '6px 10px', background: 'var(--bg-panel)', border: '1px solid var(--border)', borderRadius: 4, color: 'var(--text-secondary)' }}>
                  ⚡ 入れ替え提案: {suggestion}
                </div>
              )}
            </>
          )}
        </div>

        <div className="ss-start-row">
          <button className="ss-start-btn" onClick={onStart}>SESSION を開始 →</button>
        </div>
      </div>

      {/* ── PC EDIT OVERLAY ── */}
      {editingPC && (
        <PCEditView
          pcId={editingPC.id}
          initial={editingPC.sheet}
          onSave={savePC}
          onClose={() => setEditingPC(null)}
        />
      )}
    </div>
  )
}
