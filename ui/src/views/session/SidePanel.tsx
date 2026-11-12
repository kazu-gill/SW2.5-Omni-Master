// セッション画面の右側 (スマホでは引き出し): パーティの HP/MP、配置、PC、クエスト、要約、チェックポイント。
import type { Lane, SessionState } from '../../types'
import { formatTime, LANE_LABEL, parsePCSheet } from '../../utils/format'
import Formation from './Formation'
import Bar from '../../components/Bar'

interface Props {
  state: SessionState
  disabled: boolean
  summarizing: boolean
  onMove: (name: string, lane: Lane) => Promise<void>
  onRestore: (turn: number) => void
  onClose?: () => void
}

export default function SidePanel({ state, disabled, summarizing, onMove, onRestore, onClose }: Props) {
  const pc = parsePCSheet(state.pc)
  const pcName = pc ? pc.name || state.pc?.name || 'PC' : null
  return (
    <aside className="side" aria-label="パーティと配置">
      {onClose && (
        <div className="side-head"><b>パーティ・配置</b><button className="icon-btn" onClick={onClose} aria-label="閉じる">✕</button></div>
      )}

      <section className="ui-panel">
        <h3>パーティ</h3>
        {state.npc_sheets.length === 0 && <div className="ui-muted">NPC がいません。「パーティ」で仲間を加えてください。</div>}
        {state.npc_sheets.map((s) => (
          <div key={s.id} className="member">
            <div className="ui-row"><b>{s.name}</b><span className="ui-sys">{LANE_LABEL[s.lane] ?? s.lane}</span></div>
            <Bar kind="hp" cur={s.hp} max={s.hp_max} />
            <Bar kind="mp" cur={s.mp} max={s.mp_max} />
          </div>
        ))}
      </section>

      <section className="ui-panel">
        <h3>戦闘配置</h3>
        <Formation sheets={state.npc_sheets} pcName={pcName} disabled={disabled} onMove={onMove} />
      </section>

      <section className="ui-panel">
        <h3>PC</h3>
        {!pc ? (
          <div className="ui-muted">使う PC がありません。「パーティ」で PC を作って「使う」を押してください。</div>
        ) : (
          <div className="member">
            <div className="ui-row"><b>{pcName}</b><span className="ui-sys">{pc.race} {pc.adventurerLevel ? `冒険者Lv${pc.adventurerLevel}` : ''}</span></div>
            {!!pc.hpMax && <Bar kind="hp" cur={pc.hpCurrent ?? pc.hpMax} max={pc.hpMax} />}
            {!!pc.mpMax && <Bar kind="mp" cur={pc.mpCurrent ?? pc.mpMax} max={pc.mpMax} />}
            {pc.classes && pc.classes.length > 0 && (
              <div className="ui-sys">{pc.classes.map((c) => `${c.name}${c.level}`).join(' / ')}</div>
            )}
          </div>
        )}
      </section>

      <section className="ui-panel">
        <h3>クエスト</h3>
        {state.quest ? (
          <div>
            <b>[{state.quest.rank}] {state.quest.title}</b>
            <div className="ui-muted">{state.quest.target}</div>
          </div>
        ) : (
          <div className="ui-muted">受注していません。「クエスト」で選んで「受注」を押してください。</div>
        )}
      </section>

      <section className="ui-panel">
        <h3>これまでの要約</h3>
        {summarizing && <div className="ui-sys">📝 要約を更新しています… 終わるまで次のターンは待たされます</div>}
        {state.summary.length === 0 ? (
          <div className="ui-muted">まだありません (数ターンごとに作られます)</div>
        ) : (
          <ul className="summary">{state.summary.map((s, i) => <li key={i}>{s}</li>)}</ul>
        )}
      </section>

      <section className="ui-panel">
        <h3>チェックポイント</h3>
        {state.checkpoints.length === 0 && <div className="ui-muted">まだありません</div>}
        <ul className="cps">
          {[...state.checkpoints].sort((a, b) => b.turn - a.turn).map((c) => (
            <li key={c.turn} className="ui-row">
              <span>ターン {c.turn} の前 <span className="ui-sys">{formatTime(c.created_at)}</span></span>
              <button disabled={disabled} onClick={() => onRestore(c.turn)}>戻す</button>
            </li>
          ))}
        </ul>
      </section>
    </aside>
  )
}
