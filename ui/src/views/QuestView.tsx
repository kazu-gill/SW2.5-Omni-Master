// クエストボード: 種類で絞り込み・状態とランクで並べ替え、詳細、受注 (このセッションのクエストに)・完了。
import { useState } from 'react'
import { errorText, patch, post } from '../api'
import { useDialogs } from '../dialogs/context'
import type { QuestEntry } from '../types'
import { ELIGIBILITY_COLOR, ELIGIBILITY_LABEL, questEligibility } from '../utils/questEligibility'

type FilterType = 'all' | 'combat' | 'explore' | 'dungeon' | 'social'

const FILTERS: { id: FilterType; label: string }[] = [
  { id: 'all', label: 'すべて' }, { id: 'combat', label: '戦闘' }, { id: 'explore', label: '探索' },
  { id: 'dungeon', label: 'ダンジョン' }, { id: 'social', label: '交渉' },
]
const TAG_LABEL: Record<string, string> = { combat: '戦闘', explore: '探索', dungeon: 'ダンジョン', social: '交渉' }
const STATUS_LABEL: Record<string, string> = { active: '受注中', available: '受注可能', completed: '完了' }
const RANK_ORDER: Record<string, number> = { S: 0, A: 1, B: 2, C: 3 }
const statusOrder = (s: string) => (s === 'active' ? 0 : s === 'available' ? 1 : 2)
const tagsOf = (q: QuestEntry) => q.tags.split(',').map((t) => t.trim()).filter(Boolean)

interface Props {
  quests: QuestEntry[]
  questsErr: string
  partyAvgLevel: number | null
  sessionId: number | null
  sessionQuestId: number | null
  onChanged: () => Promise<void>
}

export default function QuestView({ quests, questsErr, partyAvgLevel, sessionId, sessionQuestId, onChanged }: Props) {
  const ui = useDialogs()
  const [filter, setFilter] = useState<FilterType>('all')
  const [selected, setSelected] = useState<number | null>(null)
  const [working, setWorking] = useState(false)

  const shown = quests
    .filter((q) => filter === 'all' || tagsOf(q).includes(filter))
    .sort((a, b) => statusOrder(a.status) - statusOrder(b.status) || (RANK_ORDER[a.rank] ?? 9) - (RANK_ORDER[b.rank] ?? 9))
  const current = quests.find((q) => q.id === sessionQuestId)

  const act = async (f: () => Promise<void>, what: string) => {
    setWorking(true)
    try {
      await f()
      await onChanged()
    } catch (e) {
      await ui.alert(errorText(e), `${what}できませんでした`)
    } finally {
      setWorking(false)
    }
  }

  const accept = (q: QuestEntry) => {
    if (sessionId === null) return
    void act(() => post(`/api/sessions/${sessionId}/quest`, { quest_id: q.id }), '受注')
  }
  const complete = (q: QuestEntry) => {
    void ui.confirm({ title: `「${q.title}」を完了にしますか？`, ok: '完了にする', run: () => act(() => patch(`/api/quests/${q.id}/complete`), '完了に') })
  }

  return (
    <div className="page">
      <div className="ui-row">
        <h2>クエストボード</h2>
        <span className="ui-muted">パーティ平均Lv {partyAvgLevel !== null ? partyAvgLevel.toFixed(1) : '—'}</span>
      </div>
      {sessionId === null && <div className="ui-muted">受注するには、先に「セッション」でセッションを選んでください。</div>}
      {current && <div className="ui-panel current-quest">このセッションのクエスト: <b>[{current.rank}] {current.title}</b></div>}
      {questsErr && <div className="err">クエストを読めませんでした: {questsErr}</div>}
      <div className="chips">
        {FILTERS.map((f) => (
          <button key={f.id} className={filter === f.id ? 'chip on' : 'chip'} onClick={() => setFilter(f.id)}>{f.label}</button>
        ))}
      </div>
      <div className="quest-grid">
        {shown.map((q) => {
          const elig = q.status === 'available' ? questEligibility(q, partyAvgLevel) : null
          const open = selected === q.id
          return (
            <div key={q.id} className={`card quest rank-${q.rank.toLowerCase()} status-${q.status}${open ? ' open' : ''}`}>
              <button className="quest-head" onClick={() => setSelected(open ? null : q.id)} aria-expanded={open}>
                <span className={`rank rank-${q.rank.toLowerCase()}`}>{q.rank}</span>
                <b>{q.title}</b>
              </button>
              <div className="chips">
                {tagsOf(q).map((t) => <span key={t} className="chip">{TAG_LABEL[t] ?? t}</span>)}
                <span className="chip">{STATUS_LABEL[q.status] ?? q.status}</span>
                {q.id === sessionQuestId && <span className="chip on">このセッション</span>}
                {elig && elig !== 'unknown' && (
                  <span className="chip" style={{ color: ELIGIBILITY_COLOR[elig] }}>{ELIGIBILITY_LABEL[elig]}</span>
                )}
              </div>
              <div className="ui-muted">依頼人 {q.client} ・ 推奨 {q.level} ・ 報酬 {q.reward}</div>
              {open && (
                <div className="quest-detail">
                  <p>{q.description}</p>
                  <div><span className="ui-muted">目的: </span>{q.target}</div>
                  <div className="ui-row">
                    {q.status !== 'completed' && (
                      <button className="primary" disabled={working || sessionId === null || q.id === sessionQuestId} onClick={() => accept(q)}>
                        {q.id === sessionQuestId ? '受注中' : '受注'}
                      </button>
                    )}
                    {q.status !== 'completed' && <button disabled={working} onClick={() => complete(q)}>完了</button>}
                  </div>
                </div>
              )}
            </div>
          )
        })}
        {shown.length === 0 && !questsErr && <div className="ui-muted">該当するクエストがありません</div>}
      </div>
    </div>
  )
}
