// 開始画面: 「続きから」(最近のセッション) と「新しく」。
import { useEffect, useState } from 'react'
import { del, errorText, get, post } from '../api'
import { useDialogs } from '../dialogs/context'
import type { QuestEntry, Session, SessionSummary } from '../types'
import { formatTime } from '../utils/format'

interface Props {
  quests: QuestEntry[]
  onOpen: (id: number) => void
}

export default function StartView({ quests, onOpen }: Props) {
  const ui = useDialogs()
  const [list, setList] = useState<SessionSummary[] | null>(null)
  const [listErr, setListErr] = useState('')
  const [title, setTitle] = useState('')
  const [questId, setQuestId] = useState('')
  const [creating, setCreating] = useState(false)
  const [err, setErr] = useState('')
  const [seq, setSeq] = useState(0)

  useEffect(() => {
    let alive = true
    get<SessionSummary[]>('/api/sessions')
      .then((l) => { if (alive) { setList(l ?? []); setListErr('') } })
      .catch((e) => { if (alive) setListErr(errorText(e)) })
    return () => { alive = false }
  }, [seq])

  const create = async () => {
    setCreating(true)
    setErr('')
    try {
      const body: { title?: string; quest_id?: number } = {}
      if (title.trim()) body.title = title.trim()
      if (questId) body.quest_id = Number(questId)
      const s = await post<Session>('/api/sessions', body)
      onOpen(s.id)
    } catch (e) {
      setErr(`作れませんでした: ${errorText(e)}`)
    } finally {
      setCreating(false)
    }
  }

  const remove = (s: SessionSummary) => {
    void ui.confirm({
      title: 'セッションを消しますか？',
      message: `「${s.title || `セッション ${s.id}`}」のターン・チェックポイント・NPC シートがすべて消えます。元に戻せません。`,
      ok: '消す',
      danger: true,
      run: async () => {
        await del(`/api/sessions/${s.id}`)
        setSeq((n) => n + 1)
      },
    })
  }

  const available = quests.filter((q) => q.status !== 'completed')

  return (
    <div className="start">
      <h2>続きから</h2>
      {listErr && (
        <div className="err">セッションの一覧を読めませんでした: {listErr} <button onClick={() => setSeq((n) => n + 1)}>もう一度</button></div>
      )}
      {list === null && !listErr && <div className="ui-muted">読み込んでいます…</div>}
      {list && list.length === 0 && <div className="ui-muted">まだセッションがありません。下の「新しく」から始めてください。</div>}
      {list && list.length > 0 && (
        <div className="ui-cards sessions">
          {list.map((s) => (
            <div key={s.id} className="card session-card" role="button" tabIndex={0}
              onClick={() => onOpen(s.id)} onKeyDown={(e) => { if (e.key === 'Enter') onOpen(s.id) }}>
              <b>{s.title || `セッション ${s.id}`}</b>
              <small>{s.quest_title ? `📜 ${s.quest_title}` : 'クエスト未受注'} ・ ターン {s.turn}</small>
              {s.last_line && <small className="last">{s.last_line}</small>}
              <small>{formatTime(s.updated_at)}</small>
              <button className="corner" title="このセッションを消す" aria-label="消す"
                onClick={(e) => { e.stopPropagation(); remove(s) }}>🗑</button>
            </div>
          ))}
        </div>
      )}

      <h2>新しく</h2>
      <form className="ui-panel new-session" onSubmit={(e) => { e.preventDefault(); void create() }}>
        <label className="field">セッションの名前 (空なら自動)
          <input value={title} maxLength={80} onChange={(e) => setTitle(e.target.value)} placeholder="例: 森の遺跡の調査" />
        </label>
        <label className="field">クエスト (あとで「クエスト」から受注もできます)
          <select value={questId} onChange={(e) => setQuestId(e.target.value)}>
            <option value="">選ばない</option>
            {available.map((q) => <option key={q.id} value={q.id}>[{q.rank}] {q.title} ({q.level})</option>)}
          </select>
        </label>
        <div className="ui-muted">PC と NPC は「パーティ」で選べます。PC は全セッション共通で「使う」にしたものが出ます。</div>
        {err && <div className="err">{err}</div>}
        <div className="ui-row"><button className="primary" disabled={creating}>{creating ? '作っています…' : '＋ 新しく始める'}</button></div>
      </form>
    </div>
  )
}
