// 確定したターン 1 つ: プレイヤー → GM → NPC ごと、判定・参照ルール・再思考のバッジ、操作。
import { useState } from 'react'
import type { TurnResult } from '../../types'
import { Badges, CheckChip, Msg, NpcBubble, PlayerBubble, RuleChips } from './Bubbles'
import { signed } from '../../utils/format'

interface Props {
  t: TurnResult
  isLast: boolean
  disabled: boolean
  imageUrl?: string
  drawing: boolean
  onRedo: (t: TurnResult, text?: string) => void
  onRestore: (t: TurnResult) => void
}

export default function TurnView({ t: raw, isLast, disabled, imageUrl, drawing, onRedo, onRestore }: Props) {
  const t = withLists(raw)
  const [editing, setEditing] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)
  const npcNames = new Set(t.npc_actions.map((a) => a.name))
  const otherChecks = t.checks.filter((c) => !npcNames.has(c.who))
  const otherDeltas = t.deltas.filter((d) => !npcNames.has(d.npc_name) && (d.hp_delta || d.mp_cost))
  const img = imageUrl ?? t.image_url

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(t.gm_narration)
      setCopied(true)
      setTimeout(() => setCopied(false), 1200)
    } catch {
      /* クリップボードが使えない環境 */
    }
  }

  const playerActs = (
    <div className="acts">
      <span className="ui-sys">ターン {t.turn}</span>
      {isLast && <button disabled={disabled} title="同じ入力でこのターンを作り直す" onClick={() => onRedo(t)}>↻ 作り直す</button>}
      {isLast && <button disabled={disabled} title="入力を直して、このターンをやり直す" onClick={() => setEditing(t.input)}>✎ 直す</button>}
      <button disabled={disabled} title="このターンを始める前の状態に戻す (これ以降のターンは消える)" onClick={() => onRestore(t)}>
        ⟲ このターンの前に戻す
      </button>
    </div>
  )

  return (
    <div className="turn" data-turn={t.turn}>
      {editing === null ? (
        <PlayerBubble text={t.input} mode={t.mode} below={playerActs} />
      ) : (
        <div className="msg me">
          <div className="col editbox">
            <textarea value={editing} onChange={(e) => setEditing(e.target.value)} aria-label="直した入力" autoFocus />
            <div className="ui-row" style={{ justifyContent: 'flex-end' }}>
              <button onClick={() => setEditing(null)}>やめる</button>
              <button
                className="primary"
                disabled={disabled || !editing.trim()}
                onClick={() => { const v = editing.trim(); setEditing(null); onRedo(t, v) }}
              >
                直して送る
              </button>
            </div>
          </div>
        </div>
      )}

      <Msg
        side="gm"
        icon="📜"
        who={t.mode === 'gm' ? 'GM (直訴への返答)' : 'GM'}
        below={
          <>
            {img && (
              <a className="pic" href={img} target="_blank" rel="noreferrer">
                <img src={img} alt={`ターン ${t.turn} の場面`} loading="lazy" />
              </a>
            )}
            {!img && drawing && <div className="ui-sys">🖼 場面の画像を描いています…</div>}
            {(otherChecks.length > 0 || otherDeltas.length > 0) && (
              <div className="chips">
                {otherChecks.map((c, i) => <CheckChip key={i} c={c} />)}
                {otherDeltas.map((d, i) => (
                  <span key={i} className="chip">{d.npc_name} {d.hp_delta ? `HP ${signed(d.hp_delta)}` : ''} {d.mp_cost ? `MP -${d.mp_cost}` : ''}</span>
                ))}
              </div>
            )}
            <RuleChips rules={t.rules_used ?? []} />
            <Badges warnings={t.warnings ?? []} attempts={Object.fromEntries(Object.entries(t.stage_attempts ?? {}).filter(([k]) => !k.startsWith('npc:')))} />
            <div className="acts"><button onClick={copy}>{copied ? 'コピーしました' : 'コピー'}</button></div>
          </>
        }
      >
        <div className="narration">{t.gm_narration}</div>
      </Msg>

      {t.npc_actions.map((a) => {
        const d = t.deltas.find((x) => x.npc_name === a.name)
        const lane = a.new_lane ?? t.formation_changes?.find((f) => f.name === a.name)?.new_lane
        return (
          <NpcBubble
            key={a.name}
            name={a.name}
            dialogue={a.dialogue}
            action={a.action}
            hp={d ? d.hp_delta : a.hp_delta}
            mp={d ? d.mp_cost : a.mp_cost}
            newLane={lane}
            check={a.check}
            checks={t.checks.filter((c) => c.who === a.name)}
            attempts={t.stage_attempts?.[`npc:${a.name}`]}
          />
        )
      })}
    </div>
  )
}

// 古いターン (2026-11 の改修前に保存したもの) は空の配列が null で入っている。
function withLists(t: TurnResult): TurnResult {
  return {
    ...t,
    npc_actions: t.npc_actions ?? [],
    deltas: t.deltas ?? [],
    checks: t.checks ?? [],
    rules_used: t.rules_used ?? [],
    warnings: t.warnings ?? [],
    formation_changes: t.formation_changes ?? [],
  }
}
