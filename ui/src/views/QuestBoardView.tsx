import { useState } from 'react'
import type { QuestEntry } from '../types'
import { questEligibility, ELIGIBILITY_LABEL, ELIGIBILITY_COLOR } from '../utils/questEligibility'

type FilterType = 'all' | 'combat' | 'explore' | 'dungeon' | 'social'

interface Props {
  quests: QuestEntry[]
  partyAvgLevel: number | null
}

const RANK_ORDER = { S: 0, A: 1, B: 2, C: 3 }

const TAG_LABEL: Record<string, string> = {
  combat: '戦闘', explore: '探索', dungeon: 'ダンジョン', social: '交渉',
}

const STATUS_LABEL: Record<string, string> = {
  active: '受注中', available: '受注可能', completed: '完了',
}

export default function QuestBoardView({ quests, partyAvgLevel }: Props) {
  const [filter, setFilter] = useState<FilterType>('all')
  const [selected, setSelected] = useState<QuestEntry | null>(null)

  const active = quests.find((q) => q.status === 'active')

  const filtered = quests
    .filter((q) => filter === 'all' || q.tags.split(',').map((t) => t.trim()).includes(filter))
    .sort((a, b) => {
      const sa = a.status === 'active' ? 0 : a.status === 'available' ? 1 : 2
      const sb = b.status === 'active' ? 0 : b.status === 'available' ? 1 : 2
      if (sa !== sb) return sa - sb
      return (RANK_ORDER[a.rank as keyof typeof RANK_ORDER] ?? 9) - (RANK_ORDER[b.rank as keyof typeof RANK_ORDER] ?? 9)
    })

  return (
    <div className="quest-board">
      <div className="quest-board-header">
        <div className="quest-board-title">QUEST BOARD</div>
        <div className="quest-board-subtitle">現在受注可能なクエスト一覧</div>
        <div className="quest-party-level">
          パーティ平均Lv:
          <span className="quest-party-level-val">
            {partyAvgLevel !== null ? partyAvgLevel.toFixed(1) : '—'}
          </span>
        </div>
      </div>

      {active && (
        <div className="quest-active-banner">
          <span className="quest-active-label">現在のクエスト</span>
          <span className={`quest-rank rank-${active.rank.toLowerCase()}`}>{active.rank}</span>
          <span className="quest-active-title">{active.title}</span>
          <span className="quest-active-reward">⬡ {active.reward}</span>
        </div>
      )}

      <div className="quest-filters">
        {(['all', 'combat', 'explore', 'dungeon', 'social'] as FilterType[]).map((f) => (
          <button
            key={f}
            className={`filter-btn ${filter === f ? 'active' : ''}`}
            onClick={() => setFilter(f)}
          >
            {{ all: 'すべて', combat: '戦闘', explore: '探索', dungeon: 'ダンジョン', social: '交渉' }[f]}
          </button>
        ))}
      </div>

      <div className="quest-grid">
        {filtered.map((q) => {
          const tags = q.tags.split(',').map((t) => t.trim()).filter(Boolean)
          const elig = q.status === 'available' ? questEligibility(q, partyAvgLevel) : null
          return (
            <div
              key={q.id}
              className={`quest-card rank-${q.rank.toLowerCase()} status-${q.status} ${selected?.id === q.id ? 'selected' : ''}`}
              onClick={() => setSelected((p) => p?.id === q.id ? null : q)}
            >
              <div className="quest-card-top">
                <div className={`quest-rank rank-${q.rank.toLowerCase()}`}>{q.rank}</div>
                <div className="quest-tags">
                  {tags.map((t) => (
                    <span key={t} className={`quest-tag type-${t}`}>{TAG_LABEL[t] ?? t}</span>
                  ))}
                </div>
                <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'flex-end', gap: 3 }}>
                  <span className={`quest-status-badge status-${q.status}`}>{STATUS_LABEL[q.status] ?? q.status}</span>
                  {elig && elig !== 'unknown' && (
                    <span className="quest-elig-badge" style={{ color: ELIGIBILITY_COLOR[elig] }}>
                      {elig === 'ok' ? '✓' : elig === 'caution' ? '!' : '✗'} {ELIGIBILITY_LABEL[elig]}
                    </span>
                  )}
                </div>
              </div>
              <div className="quest-card-title">{q.title}</div>
              <div className="quest-desc">{q.description}</div>
              <div className="quest-meta">
                <div className="quest-meta-item">
                  <span style={{ color: 'var(--text-dim)' }}>依頼人:</span>
                  <span className="quest-meta-value">{q.client}</span>
                </div>
                <div className="quest-meta-item">
                  <span style={{ color: 'var(--text-dim)' }}>推奨:</span>
                  <span className="quest-meta-value">{q.level}</span>
                </div>
                <div className="quest-reward">⬡ {q.reward}</div>
              </div>
              {selected?.id === q.id && (
                <div className="quest-detail-target">
                  <span style={{ color: 'var(--text-dim)' }}>対象: </span>{q.target}
                </div>
              )}
            </div>
          )
        })}
        {filtered.length === 0 && (
          <div style={{ color: 'var(--text-dim)', fontSize: 13, padding: '20px 0' }}>
            該当するクエストがありません
          </div>
        )}
      </div>
    </div>
  )
}
