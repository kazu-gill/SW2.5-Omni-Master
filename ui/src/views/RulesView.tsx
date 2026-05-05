import { useState, useEffect, useCallback } from 'react'
import type { RuleEntry } from '../types'

type FilterType = 'all' | 'rulebook' | 'correction' | 'houserule'

interface CorrectModalState {
  refId: number
  refTag: string
  refText: string
}

interface EditModalState {
  id: number
  text: string
  tag: string
  enabled: boolean
}

const SOURCE_LABEL: Record<RuleEntry['source_type'], string> = {
  rulebook: 'ルールブック',
  correction: '訂正',
  houserule: 'ハウスルール',
}

export default function RulesView() {
  const [rules, setRules] = useState<RuleEntry[]>([])
  const [loading, setLoading] = useState(true)
  const [filter, setFilter] = useState<FilterType>('all')
  const [showAddModal, setShowAddModal] = useState(false)
  const [correctModal, setCorrectModal] = useState<CorrectModalState | null>(null)
  const [editModal, setEditModal] = useState<EditModalState | null>(null)
  const [form, setForm] = useState({ source_type: 'houserule', tag: '', text: '', overrides_id: '' })
  const [correctText, setCorrectText] = useState('')
  const [saving, setSaving] = useState(false)

  const loadRules = useCallback(async () => {
    try {
      const res = await fetch('/api/rules', { signal: AbortSignal.timeout(5000) })
      if (res.ok) setRules(await res.json())
    } catch { /* offline */ } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { loadRules() }, [loadRules])

  const filtered = filter === 'all' ? rules : rules.filter((r) => r.source_type === filter)

  async function toggleEnabled(r: RuleEntry) {
    const next = { ...r, enabled: !r.enabled }
    setRules((prev) => prev.map((x) => x.id === r.id ? next : x))
    await fetch(`/api/rules/${r.id}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ text: r.text, tag: r.tag, enabled: !r.enabled }),
    })
  }

  async function deleteRule(id: number) {
    if (!confirm('このエントリを削除しますか？')) return
    setRules((prev) => prev.filter((r) => r.id !== id))
    await fetch(`/api/rules/${id}`, { method: 'DELETE' })
  }

  async function addRule() {
    if (!form.text.trim()) return
    setSaving(true)
    try {
      const res = await fetch('/api/rules', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          source_type: form.source_type,
          tag: form.tag,
          text: form.text,
          overrides_id: form.overrides_id ? parseInt(form.overrides_id) : null,
        }),
      })
      if (res.ok) {
        const created: RuleEntry = await res.json()
        setRules((prev) => [...prev, created])
        setShowAddModal(false)
        setForm({ source_type: 'houserule', tag: '', text: '', overrides_id: '' })
      }
    } finally { setSaving(false) }
  }

  async function saveCorrection() {
    if (!correctModal || !correctText.trim()) return
    setSaving(true)
    try {
      const res = await fetch('/api/rules', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          source_type: 'correction',
          tag: correctModal.refTag,
          text: correctText.trim(),
          overrides_id: correctModal.refId,
        }),
      })
      if (res.ok) {
        const created: RuleEntry = await res.json()
        setRules((prev) => [...prev, created])
        setCorrectModal(null)
        setCorrectText('')
      }
    } finally { setSaving(false) }
  }

  async function saveEdit() {
    if (!editModal || !editModal.text.trim()) return
    setSaving(true)
    try {
      await fetch(`/api/rules/${editModal.id}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ text: editModal.text, tag: editModal.tag, enabled: editModal.enabled }),
      })
      setRules((prev) => prev.map((r) =>
        r.id === editModal.id ? { ...r, text: editModal.text, tag: editModal.tag } : r
      ))
      setEditModal(null)
    } finally { setSaving(false) }
  }

  const filterPillClass = (f: FilterType) => {
    if (filter !== f) return 'type-pill'
    const map: Record<FilterType, string> = {
      all: 'active-rulebook', rulebook: 'active-rulebook',
      correction: 'active-correction', houserule: 'active-houserule',
    }
    return `type-pill ${map[f]}`
  }

  const counts = {
    all: rules.length,
    rulebook: rules.filter((r) => r.source_type === 'rulebook').length,
    correction: rules.filter((r) => r.source_type === 'correction').length,
    houserule: rules.filter((r) => r.source_type === 'houserule').length,
  }

  return (
    <div className="rules-view">
      <div className="rules-toolbar">
        <div className="rules-toolbar-title">RULE MANAGER</div>
        <div className="type-filter">
          {(['all', 'rulebook', 'correction', 'houserule'] as FilterType[]).map((f) => (
            <button key={f} className={filterPillClass(f)} onClick={() => setFilter(f)}>
              {{ all: 'すべて', rulebook: 'ルールブック', correction: '訂正', houserule: 'ハウスルール' }[f]}
              <span className="pill-count">{counts[f]}</span>
            </button>
          ))}
        </div>
        <button className="rules-add-btn" onClick={() => setShowAddModal(true)}>+ 追加</button>
      </div>

      <div className="rules-body">
        {loading ? (
          <div className="rules-empty">読み込み中…</div>
        ) : filtered.length === 0 ? (
          <div className="rules-empty">
            {filter === 'all'
              ? 'ルールデータがありません。ocr_rulebook.py でルールブックをインポートしてください。'
              : 'このカテゴリのエントリがありません。'}
          </div>
        ) : (
          <table className="rules-table">
            <thead>
              <tr>
                <th>種別</th>
                <th>優先</th>
                <th>タグ</th>
                <th>内容</th>
                <th>上書き</th>
                <th>状態</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {filtered.map((r) => (
                <tr key={r.id} className={r.enabled ? '' : 'inactive'}>
                  <td><span className={`source-badge ${r.source_type}`}>{SOURCE_LABEL[r.source_type]}</span></td>
                  <td><span className={`priority-badge p-${r.priority}`}>{r.priority}</span></td>
                  <td className="rules-tag-cell">{r.tag || <span style={{ color: 'var(--text-dim)' }}>—</span>}</td>
                  <td className="rules-text-cell" title={r.text}>{r.text}</td>
                  <td>
                    {r.overrides_id
                      ? <span className="overrides-chip">→ ID:{r.overrides_id}</span>
                      : <span style={{ color: 'var(--text-dim)' }}>—</span>}
                  </td>
                  <td>
                    <button
                      className={`toggle-btn ${r.enabled ? 'enabled' : 'disabled'}`}
                      onClick={() => toggleEnabled(r)}
                    >
                      {r.enabled ? '有効' : '無効'}
                    </button>
                  </td>
                  <td className="rules-action-cell">
                    <button
                      className="rule-edit-btn"
                      title="テキスト編集"
                      onClick={() => setEditModal({ id: r.id, text: r.text, tag: r.tag, enabled: r.enabled })}
                    >
                      ✎
                    </button>
                    {r.source_type === 'rulebook' && (
                      <button
                        className="correct-btn"
                        onClick={() => { setCorrectModal({ refId: r.id, refTag: r.tag, refText: r.text }); setCorrectText('') }}
                      >
                        訂正
                      </button>
                    )}
                    {r.source_type !== 'rulebook' && (
                      <button
                        className="rule-delete-btn"
                        title="削除"
                        onClick={() => deleteRule(r.id)}
                      >
                        ✕
                      </button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      {/* ── Add Modal ── */}
      {showAddModal && (
        <div className="modal-overlay visible" onClick={(e) => { if (e.target === e.currentTarget) setShowAddModal(false) }}>
          <div className="modal">
            <div className="modal-title">ルールエントリを追加</div>
            <div className="form-field">
              <label className="form-label">種別</label>
              <select className="form-select" value={form.source_type} onChange={(e) => setForm({ ...form, source_type: e.target.value })}>
                <option value="houserule">houserule — ハウスルール（優先度20）</option>
                <option value="correction">correction — 訂正（優先度10）</option>
              </select>
            </div>
            <div className="form-field">
              <label className="form-label">トピックタグ</label>
              <input className="form-input" type="text" placeholder="combat / magic / status / general …" value={form.tag} onChange={(e) => setForm({ ...form, tag: e.target.value })} />
            </div>
            <div className="form-field">
              <label className="form-label">内容</label>
              <textarea className="form-textarea" placeholder="ルールテキストを入力…" value={form.text} onChange={(e) => setForm({ ...form, text: e.target.value })} />
            </div>
            {form.source_type === 'correction' && (
              <div className="form-field">
                <label className="form-label">上書き対象 ID（訂正の場合）</label>
                <input className="form-input" type="text" placeholder="元チャンクの ID（任意）" value={form.overrides_id} onChange={(e) => setForm({ ...form, overrides_id: e.target.value })} />
              </div>
            )}
            <div className="modal-footer">
              <button className="modal-cancel" onClick={() => setShowAddModal(false)}>キャンセル</button>
              <button className="modal-save" onClick={addRule} disabled={!form.text.trim() || saving}>
                {saving ? '保存中…' : '保存'}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* ── Correction Modal ── */}
      {correctModal && (
        <div className="modal-overlay visible" onClick={(e) => { if (e.target === e.currentTarget) setCorrectModal(null) }}>
          <div className="modal">
            <div className="modal-title">訂正エントリを追加</div>
            <div className="form-field">
              <div className="correction-ref-label">訂正対象（元のテキスト）</div>
              <div className="correction-ref">{correctModal.refText}</div>
              <div className="correction-id-display">
                チャンクID: <span>{correctModal.refId}</span> — このIDは自動で紐付けられます
              </div>
            </div>
            <div className="form-field">
              <label className="form-label">タグ</label>
              <input className="form-input" type="text" readOnly value={correctModal.refTag} style={{ color: 'var(--text-dim)' }} />
            </div>
            <div className="form-field">
              <label className="form-label">訂正後のテキスト</label>
              <textarea className="form-textarea" placeholder="正しいルールテキストを入力…" value={correctText} onChange={(e) => setCorrectText(e.target.value)} />
            </div>
            <div className="modal-footer">
              <button className="modal-cancel" onClick={() => setCorrectModal(null)}>キャンセル</button>
              <button className="modal-save" onClick={saveCorrection} disabled={!correctText.trim() || saving}>
                {saving ? '保存中…' : '訂正を保存'}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* ── Edit Modal ── */}
      {editModal && (
        <div className="modal-overlay visible" onClick={(e) => { if (e.target === e.currentTarget) setEditModal(null) }}>
          <div className="modal">
            <div className="modal-title">テキストを編集</div>
            <div className="correction-id-display" style={{ marginBottom: 12 }}>
              ID: <span>{editModal.id}</span>
            </div>
            <div className="form-field">
              <label className="form-label">タグ</label>
              <input className="form-input" type="text" placeholder="combat / magic / status / general …"
                value={editModal.tag}
                onChange={(e) => setEditModal({ ...editModal, tag: e.target.value })} />
            </div>
            <div className="form-field">
              <label className="form-label">テキスト</label>
              <textarea className="form-textarea" style={{ minHeight: 120 }}
                value={editModal.text}
                onChange={(e) => setEditModal({ ...editModal, text: e.target.value })} />
            </div>
            <div className="modal-footer">
              <button className="modal-cancel" onClick={() => setEditModal(null)}>キャンセル</button>
              <button className="modal-save" onClick={saveEdit} disabled={!editModal.text.trim() || saving}>
                {saving ? '保存中…' : '保存'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
