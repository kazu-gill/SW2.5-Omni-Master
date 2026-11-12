// 進行中のターン: プレイヤーの吹き出しをすぐ出し、段の進み具合と GM/NPC の本文を流しながら見せる。
import { useEffect, useState } from 'react'
import type { LiveStage, LiveTurn } from '../../live/turnReducer'
import { CheckChip, Msg, PlayerBubble, RuleChips } from './Bubbles'

function useNow(active: boolean) {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    if (!active) return
    const t = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(t)
  }, [active])
  return now
}

const secs = (ms: number) => `${Math.max(0, Math.round(ms / 1000))} 秒`

function stageMark(s: LiveStage) {
  switch (s.status) {
    case 'done': return '✓'
    case 'failed': return '✕'
    case 'skipped': return '–'
    case 'start': case 'retry': return '●'
    default: return '○'
  }
}

export function ProgressRow({ live, now }: { live: LiveTurn; now: number }) {
  return (
    <div className="progress" role="status" aria-live="polite">
      <ol>
        {live.stages.map((s) => {
          const running = s.status === 'start' || s.status === 'retry'
          const t = s.startedAt ? (s.endedAt ?? now) - s.startedAt : 0
          return (
            <li key={s.key} className={`st-${s.status}${running ? ' cur' : ''}`} title={s.message}>
              <span className="mk">{stageMark(s)}</span> {s.label}
              {s.startedAt && <small> {secs(t)}</small>}
              {s.status === 'retry' && <small> 再思考 {s.attempt - 1}</small>}
            </li>
          )
        })}
      </ol>
      <span className="ui-sys">{live.error ? '止まりました' : `経過 ${secs(now - live.startedAt)}`}</span>
    </div>
  )
}

function StageBubble({ s, gmMode }: { s: LiveStage; gmMode: boolean }) {
  const running = s.status === 'start' || s.status === 'retry'
  const isGM = s.key === 'gm'
  const retryNote = s.status === 'retry' && <div className="ui-sys">再思考中 ({s.attempt}回目)</div>
  if (!s.text && !running) return null
  if (isGM) {
    return (
      <Msg side="gm" icon="📜" who={gmMode ? 'GM (直訴への返答)' : 'GM'} typing={running}>
        {retryNote}
        {/* 「判定: …」の行はシステムが振ってチップで出すので、描写からは外す */}
        <div className="narration">{s.text.split('\n').filter((l) => !/^\s*判定\s*[:：]/.test(l)).join('\n')}</div>
      </Msg>
    )
  }
  if (s.key === 'support') {
    return running ? <div className="ui-sys center">🧮 数値確認中…{s.status === 'retry' && ` 再思考中 (${s.attempt}回目)`}</div> : null
  }
  // NPC は YAML で書くので、生成途中でも dialogue / action の行を拾って台詞として見せる
  const f = yamlFields(s.text)
  return (
    <Msg side="npc" icon="🛡" who={s.label} typing={running}>
      {retryNote}
      {f.dialogue || f.action ? (
        <>
          {f.dialogue && <div className="dialogue">「{f.dialogue.replace(/^「+|」+$/g, '')}」</div>}
          {f.action && <div className="action">{f.action}</div>}
        </>
      ) : (
        <span className="ui-muted">行動を考えています…</span>
      )}
    </Msg>
  )
}

/** 書きかけの YAML から 1 行の値を拾う (最後の行は書きかけでもよい)。 */
function yamlFields(text: string): { dialogue?: string; action?: string } {
  const out: { dialogue?: string; action?: string } = {}
  for (const line of text.replace(/```\w*/g, '').split('\n')) {
    const m = /^\s*(dialogue|action)\s*:\s*(.*)$/.exec(line)
    if (m && m[2].trim()) out[m[1] as 'dialogue' | 'action'] = m[2].trim().replace(/^["']|["']$/g, '')
  }
  return out
}

interface Props {
  live: LiveTurn
  onRetry: () => void
  onEditBack: () => void
}

export default function LiveTurnView({ live, onRetry, onEditBack }: Props) {
  const now = useNow(!live.error)
  const checks = live.checks
  const rules = Object.values(live.rules).flat()
  const uniqueRules = rules.filter((r, i) => rules.findIndex((x) => x.id === r.id) === i)
  return (
    <div className="turn live">
      <PlayerBubble text={live.input} mode={live.mode} />
      <ProgressRow live={live} now={now} />
      {live.stages.map((s) => <StageBubble key={s.key} s={s} gmMode={live.mode === 'gm'} />)}
      {(checks.length > 0 || uniqueRules.length > 0) && (
        <div className="live-extra">
          <div className="chips">{checks.map((c, i) => <CheckChip key={i} c={c} />)}</div>
          <RuleChips rules={uniqueRules} />
        </div>
      )}
      {live.error && (
        <div className="fail-box">
          <div className="err">ターンが失敗しました: {live.error}</div>
          <div className="ui-muted">状態はこのターンの前に戻っています。もう一度送るか、入力を直してください。</div>
          <div className="ui-row">
            {live.input && <button className="primary" onClick={onRetry}>↻ もう一度送る</button>}
            {live.input && <button onClick={onEditBack}>✎ 入力欄に戻して直す</button>}
          </div>
        </div>
      )}
    </div>
  )
}
