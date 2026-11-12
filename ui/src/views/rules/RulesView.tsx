// ルール: ゲーム中と同じ検索 (埋め込み + 用語の完全一致) と、絞り込み・ページ送りの一覧、ハウスルール・訂正の管理。
import { useEffect, useState } from 'react'
import { del, errorText, get, patch } from '../../api'
import { useDialogs } from '../../dialogs/context'
import type { RuleEntry, RuleHit, RulePage, RuleSource } from '../../types'
import { pageText, SOURCE_LABEL } from '../../utils/format'
import RuleEditDialog, { type RuleEditTarget } from './RuleEditDialog'

const PAGE = 50
const TYPES: { id: '' | RuleSource; label: string }[] = [
  { id: '', label: 'すべて' }, { id: 'rulebook', label: 'ルールブック' }, { id: 'correction', label: '訂正' }, { id: 'houserule', label: 'ハウスルール' },
]

function Search() {
  const [q, setQ] = useState('')
  const [hits, setHits] = useState<RuleHit[] | null>(null)
  const [err, setErr] = useState('')
  const [loading, setLoading] = useState(false)

  const run = async () => {
    if (!q.trim()) return
    setLoading(true)
    setErr('')
    try {
      setHits(await get<RuleHit[]>(`/api/rules/search?q=${encodeURIComponent(q.trim())}&k=8`))
    } catch (e) {
      setErr(`検索できませんでした: ${errorText(e)}`)
    } finally {
      setLoading(false)
    }
  }

  return (
    <section className="ui-panel">
      <h3>検索 (ゲーム中の GM と同じ引き方)</h3>
      <form className="ui-row" onSubmit={(e) => { e.preventDefault(); void run() }}>
        <input className="grow" value={q} onChange={(e) => setQ(e.target.value)} placeholder="例: かばう の効果、先制判定" aria-label="ルールを検索" />
        <button className="primary" disabled={loading || !q.trim()}>{loading ? '探しています…' : '検索'}</button>
      </form>
      {err && <div className="err">{err}</div>}
      {hits && hits.length === 0 && <div className="ui-muted">見つかりませんでした。言い方を変えるか、下の一覧で文字を含むものを探してください。</div>}
      {hits?.map((h) => (
        <div key={h.id} className="rule">
          <div className="ui-row">
            <span className={`chip src-${h.source_type}`}>{SOURCE_LABEL[h.source_type]}</span>
            <b className="grow">{h.section || `#${h.id}`} <span className="ui-sys">{pageText(h)}</span></b>
            <span className="ui-sys">{h.matched_term ? `用語「${h.matched_term}」` : `類似度 ${h.score.toFixed(3)}`}</span>
          </div>
          <div className="rule-body">{h.text}</div>
        </div>
      ))}
    </section>
  )
}

export default function RulesView() {
  const ui = useDialogs()
  const [q, setQ] = useState('')
  const [appliedQ, setAppliedQ] = useState('')
  const [type, setType] = useState<'' | RuleSource>('')
  const [offset, setOffset] = useState(0)
  const [page, setPage] = useState<RulePage | null>(null)
  const [err, setErr] = useState('')
  const [notice, setNotice] = useState('')
  const [edit, setEdit] = useState<RuleEditTarget | null>(null)
  const [seq, setSeq] = useState(0)

  useEffect(() => {
    let alive = true
    const params = new URLSearchParams({ q: appliedQ, source_type: type, limit: String(PAGE), offset: String(offset) })
    get<RulePage>(`/api/rules?${params}`)
      .then((p) => { if (alive) { setPage(p); setErr('') } })
      .catch((e) => { if (alive) setErr(`一覧を読めませんでした: ${errorText(e)}`) })
    return () => { alive = false }
  }, [appliedQ, type, offset, seq])

  const refresh = () => setSeq((n) => n + 1)

  const toggle = async (r: RuleEntry) => {
    try {
      await patch(`/api/rules/${r.id}`, { text: r.text, tag: r.tag, enabled: !r.enabled })
      setPage((p) => p && { ...p, items: p.items.map((x) => (x.id === r.id ? { ...x, enabled: !r.enabled } : x)) })
    } catch (e) {
      await ui.alert(errorText(e), '切り替えられませんでした')
    }
  }

  const remove = (r: RuleEntry) => {
    void ui.confirm({
      title: `ルール #${r.id} を消しますか？`,
      message: r.text.slice(0, 120) + (r.text.length > 120 ? '…' : ''),
      ok: '消す',
      danger: true,
      run: async () => { await del(`/api/rules/${r.id}`); refresh() },
    })
  }

  const total = page?.total ?? 0
  const from = total ? offset + 1 : 0
  const to = Math.min(offset + PAGE, total)

  return (
    <div className="page">
      <div className="ui-row"><h2 className="grow">ルール</h2><button onClick={() => setEdit({ kind: 'new' })}>＋ ハウスルール・訂正を足す</button></div>
      {notice && <div className="notice">{notice} <button onClick={() => setNotice('')}>閉じる</button></div>}
      <Search />
      <section className="ui-panel">
        <h3>一覧</h3>
        <form className="ui-row" onSubmit={(e) => { e.preventDefault(); setOffset(0); setAppliedQ(q.trim()) }}>
          <input className="grow" value={q} onChange={(e) => setQ(e.target.value)} placeholder="本文・節に含む文字" aria-label="本文・節で絞り込む" />
          <button>絞り込む</button>
        </form>
        <div className="chips">
          {TYPES.map((t) => (
            <button key={t.id} className={type === t.id ? 'chip on' : 'chip'} onClick={() => { setType(t.id); setOffset(0) }}>{t.label}</button>
          ))}
        </div>
        {err && <div className="err">{err} <button onClick={refresh}>もう一度</button></div>}
        <div className="ui-sys">{total} 件中 {from}–{to} 件</div>
        {page?.items.map((r) => (
          <div key={r.id} className={r.enabled ? 'rule' : 'rule off'}>
            <div className="ui-row">
              <span className={`chip src-${r.source_type}`}>{SOURCE_LABEL[r.source_type]}</span>
              <b className="grow">{r.section || r.tag || `#${r.id}`} <span className="ui-sys">{pageText(r)} #{r.id}{r.overrides_id ? ` → #${r.overrides_id} を訂正` : ''}</span></b>
            </div>
            <div className="rule-body">{r.text}</div>
            <div className="acts">
              <label className="check"><input type="checkbox" checked={r.enabled} onChange={() => void toggle(r)} /> 使う</label>
              {r.source_type === 'rulebook'
                ? <button onClick={() => setEdit({ kind: 'correct', of: r })}>訂正を作る</button>
                : <button onClick={() => setEdit({ kind: 'edit', rule: r })}>✎ 直す</button>}
              {r.source_type !== 'rulebook' && <button className="danger-text" onClick={() => remove(r)}>🗑 消す</button>}
            </div>
          </div>
        ))}
        <div className="ui-row pager">
          <button disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - PAGE))}>← 前</button>
          <button disabled={to >= total} onClick={() => setOffset(offset + PAGE)}>次 →</button>
        </div>
      </section>
      {edit && (
        <RuleEditDialog
          target={edit}
          onClose={() => setEdit(null)}
          onSaved={(embedded) => {
            setEdit(null)
            setNotice(embedded === false
              ? '保存しましたが、埋め込みに失敗しました (埋め込みモデルが止まっている可能性)。ゲーム中の検索では文字の一致でしか見つかりません。埋め込みモデルを起動してから ai-agents/embed_chunks.py で埋め込み直してください。'
              : '保存しました。')
            refresh()
          }}
        />
      )}
    </div>
  )
}
