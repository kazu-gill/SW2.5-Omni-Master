// 途中で止まったターン: 終わった段の結果を出したまま、「続きから」か「取り消す」を選ばせる。
// 画面を開き直しても、再起動しても、state の stopped_turn だけで同じ表示になる。
import { useState } from 'react'
import type { StoppedTurn } from '../../types'
import { CheckChip, Msg, NpcBubble, PlayerBubble } from './Bubbles'

interface Props {
  stopped: StoppedTurn
  /** 続きの操作を送っている最中 */
  working: boolean
  onResume: () => void
  onDiscard: () => void
  /** 取り消してから、直した入力を新しいターンとして送る */
  onFix: (text: string) => void
}

export default function StoppedTurnView({ stopped, working, onResume, onDiscard, onFix }: Props) {
  const [editing, setEditing] = useState<string | null>(null)
  const r = stopped.result
  const done = stopped.done ?? []
  const npcActions = r.npc_actions ?? []
  const checks = r.checks ?? []
  const npcNames = new Set(npcActions.map((a) => a.name))
  const otherChecks = checks.filter((c) => !npcNames.has(c.who))

  return (
    <div className="turn stopped" data-turn={stopped.turn}>
      {editing === null ? (
        <PlayerBubble text={stopped.input} mode={stopped.mode} />
      ) : (
        <div className="msg me">
          <div className="col editbox">
            <textarea value={editing} onChange={(e) => setEditing(e.target.value)} aria-label="直した入力" autoFocus />
            <div className="ui-row" style={{ justifyContent: 'flex-end' }}>
              <button onClick={() => setEditing(null)}>やめる</button>
              <button
                className="primary"
                disabled={working || !editing.trim()}
                title="止まったターンを取り消して、直した入力を新しいターンとして送る"
                onClick={() => { const v = editing.trim(); setEditing(null); onFix(v) }}
              >
                取り消して直して送る
              </button>
            </div>
          </div>
        </div>
      )}
      {done.includes('gm') && r.gm_narration && (
        <Msg
          side="gm"
          icon="📜"
          who={stopped.mode === 'gm' ? 'GM (直訴への返答)' : 'GM'}
          below={otherChecks.length > 0 && <div className="chips">{otherChecks.map((c, i) => <CheckChip key={i} c={c} />)}</div>}
        >
          <div className="narration">{r.gm_narration}</div>
        </Msg>
      )}
      {npcActions.map((a) => {
        const d = (r.deltas ?? []).find((x) => x.npc_name === a.name)
        return (
          <NpcBubble
            key={a.name}
            name={a.name}
            dialogue={a.dialogue}
            action={a.action}
            hp={d ? d.hp_delta : a.hp_delta}
            mp={d ? d.mp_cost : a.mp_cost}
            newLane={a.new_lane}
            check={a.check}
            checks={checks.filter((c) => c.who === a.name)}
          />
        )
      })}
      <div className="fail-box" role="alert">
        <div className="err">ターン {stopped.turn} が途中で止まりました{stopped.error ? `: ${stopped.error}` : ''}</div>
        <div className="ui-muted">
          {done.length > 0 ? '終わった段の結果は残っています。続きから再開するか、取り消してこのターンの前に戻してください。' : '続きから (最初の段から) やり直せます。取り消してこのターンの前に戻すこともできます。'}
        </div>
        <div className="ui-row">
          <button className="primary" disabled={working} onClick={onResume} title="止まった段から続ける">▶ 続きから</button>
          <button className="danger" disabled={working} onClick={onDiscard} title="このターンを無かったことにして、開始前に戻す">取り消す</button>
          {editing === null && <button disabled={working} onClick={() => setEditing(stopped.input)} title="取り消して、入力を直して送り直す">✎ 直す</button>}
        </div>
      </div>
    </div>
  )
}
