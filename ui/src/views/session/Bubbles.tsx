// 会話ログの部品: 吹き出し・NPC の吹き出し・判定とルールのチップ。
import { useState, type ReactNode } from 'react'
import type { Check, Lane, RuleHit, TurnMode } from '../../types'
import { checkOk, checkText, LANE_LABEL, pageText, signed, SOURCE_LABEL } from '../../utils/format'

interface MsgProps {
  side: 'me' | 'gm' | 'npc'
  icon: string
  who: ReactNode
  children: ReactNode
  typing?: boolean
  /** 吹き出しの下 (チップ・操作) */
  below?: ReactNode
}

export function Msg({ side, icon, who, children, typing, below }: MsgProps) {
  return (
    <div className={`msg ${side}`}>
      <div className="av" aria-hidden>{icon}</div>
      <div className="col">
        <div className="who">{who}</div>
        <div className={typing ? 'bubble typing' : 'bubble'}>{children}</div>
        {below}
      </div>
    </div>
  )
}

export function PlayerBubble({ text, mode, below }: { text: string; mode: TurnMode; below?: ReactNode }) {
  return (
    <Msg side="me" icon="🧑" who={mode === 'gm' ? <span className="badge gm">GM直訴</span> : 'あなた'} below={below}>
      {text || <span className="ui-muted">(入力は別のタブから)</span>}
    </Msg>
  )
}

interface NpcBubbleProps {
  name: string
  dialogue: string
  action: string
  hp?: number
  mp?: number
  newLane?: Lane
  check?: string
  checks?: Check[]
  attempts?: number
}

export function NpcBubble({ name, dialogue, action, hp, mp, newLane, check, checks, attempts }: NpcBubbleProps) {
  const chips = (
    <div className="chips">
      {!!hp && <span className={hp < 0 ? 'chip hp bad' : 'chip hp'}>HP {signed(hp)}</span>}
      {!!mp && <span className="chip mp">MP -{Math.abs(mp)}</span>}
      {newLane && <span className="chip move">→ {LANE_LABEL[newLane] ?? newLane}</span>}
      {checks?.map((c, i) => <CheckChip key={i} c={c} />)}
      {!!attempts && attempts > 1 && <span className="badge">再思考 {attempts - 1} 回</span>}
    </div>
  )
  return (
    <Msg side="npc" icon="🛡" who={name} below={chips}>
      {dialogue && <div className="dialogue">「{unquote(dialogue)}」</div>}
      {action && <div className="action">{action}</div>}
      {/* 判定はサイコロを振った結果 (checks) をチップで出す。振る前 (ライブ中) だけ宣言を見せる */}
      {check && !checks?.length && <div className="action">判定: {check}</div>}
      {!dialogue && !action && <span className="ui-muted">(行動なし)</span>}
    </Msg>
  )
}

export function CheckChip({ c }: { c: Check }) {
  return <span className={checkOk(c) ? 'chip dice ok' : 'chip dice bad'}>{checkText(c)}</span>
}

export function RuleChips({ rules: all }: { rules: RuleHit[] }) {
  const [open, setOpen] = useState<number | null>(null)
  // GM と数値確認が同じルールを引くことがある。同じルールは 1 回だけ出す
  const rules = all.filter((r, i) => all.findIndex((x) => x.id === r.id) === i)
  if (!rules.length) return null
  const cur = rules.find((r) => r.id === open)
  return (
    <div className="rules-used">
      <div className="chips">
        {rules.map((r) => (
          <button
            key={r.id}
            className={open === r.id ? 'chip rule on' : 'chip rule'}
            title={`${r.section || SOURCE_LABEL[r.source_type] || ''}\n${r.matched_term ? `用語「${r.matched_term}」で一致` : `類似度 ${r.score.toFixed(2)}`}`}
            onClick={() => setOpen(open === r.id ? null : r.id)}
          >
            📖 {shortSection(r)} {pageText(r)}
          </button>
        ))}
      </div>
      {cur && (
        <div className="rule-text">
          <div className="ui-muted">{SOURCE_LABEL[cur.source_type]} {cur.section} {pageText(cur)}</div>
          {cur.text}
        </div>
      )}
    </div>
  )
}

export function Badges({ warnings, attempts }: { warnings: string[]; attempts: Record<string, number> }) {
  const re = Object.entries(attempts).filter(([, n]) => n > 1)
  if (!warnings.length && !re.length) return null
  return (
    <div className="chips">
      {re.map(([k, n]) => (
        <span key={k} className="badge" title="出力が検証に通らず、作り直した回数">
          再思考 {k === 'gm' ? 'GM' : k === 'support' ? '数値確認' : k.replace(/^npc:/, '')} ×{n - 1}
        </span>
      ))}
      {warnings.map((w, i) => <span key={i} className="badge warn">⚠ {w}</span>)}
    </div>
  )
}

// モデルの台詞には「」が付いていることがある。吹き出しで付け直すので外す
function unquote(s: string): string {
  return s.trim().replace(/^「+/, '').replace(/」+$/, '')
}

// 節のパスは長いので、チップには最後の見出しだけ出す (全体はツールチップ)
function shortSection(r: RuleHit): string {
  if (!r.section) return SOURCE_LABEL[r.source_type] || 'ルール'
  const parts = r.section.split(' > ')
  return parts[parts.length - 1]
}
