import type { TurnResult } from '../types'

interface Props {
  turns: TurnResult[]
}

export function TurnLog({ turns }: Props) {
  return (
    <div className="turn-log">
      <h2>セッションログ</h2>
      {turns.length === 0 && <p className="empty">まだログがありません</p>}
      {[...turns].reverse().map((t) => (
        <div key={`${t.session_id}-${t.turn}`} className="turn-entry">
          <div className="turn-header">Turn {t.turn}</div>
          <div className="gm-narration">
            <span className="label">GM</span>
            <p>{t.gm_narration}</p>
          </div>
          {t.npc_actions?.map((a, i) => (
            <div key={i} className="npc-entry">
              <span className="label npc-label">{a.Name}</span>
              <span className="npc-action">{a.Action}</span>
              <p className="npc-dialogue">「{a.Dialogue}」</p>
              {(a.MPCost > 0 || a.HPDelta !== 0) && (
                <span className="resource-change">
                  {a.HPDelta !== 0 && <span>HP {a.HPDelta > 0 ? '+' : ''}{a.HPDelta}</span>}
                  {a.MPCost > 0 && <span> MP -{a.MPCost}</span>}
                </span>
              )}
            </div>
          ))}
        </div>
      ))}
    </div>
  )
}
