// 3 レーンの戦闘配置 (敵後衛 / 前線 / 味方後衛)。NPC を選んでからレーンを押すと移動 (進行中は動かせない)。
import { useState } from 'react'
import type { Lane, NPCSheet } from '../../types'
import { LANE_LABEL } from '../../utils/format'

const LANES: Lane[] = ['enemy', 'front', 'party']

interface Props {
  sheets: NPCSheet[]
  pcName: string | null
  disabled: boolean
  onMove: (name: string, lane: Lane) => Promise<void>
}

export default function Formation({ sheets, pcName, disabled, onMove }: Props) {
  const [picked, setPicked] = useState<string | null>(null)
  const [moving, setMoving] = useState(false)

  const move = async (lane: Lane) => {
    if (!picked || disabled || moving) return
    const s = sheets.find((x) => x.name === picked)
    if (!s || s.lane === lane) { setPicked(null); return }
    setMoving(true)
    try {
      await onMove(picked, lane)
      setPicked(null)
    } finally {
      setMoving(false)
    }
  }

  return (
    <div className="formation">
      {LANES.map((lane) => {
        const here = sheets.filter((s) => (s.lane || 'party') === lane)
        const target = !!picked && !disabled
        return (
          <div
            key={lane}
            className={`lane lane-${lane}${target ? ' target' : ''}`}
            onClick={() => void move(lane)}
            role={target ? 'button' : undefined}
            aria-label={target ? `${picked} を${LANE_LABEL[lane]}へ` : undefined}
          >
            <span className="lane-name">{LANE_LABEL[lane]}</span>
            <div className="chips">
              {lane === 'party' && pcName && <span className="chip pc" title="PC の位置は GM が描写で決めます">🧑 {pcName}</span>}
              {here.map((s) => (
                <button
                  key={s.id}
                  className={picked === s.name ? 'chip on' : 'chip'}
                  disabled={disabled || moving}
                  onClick={(e) => { e.stopPropagation(); setPicked(picked === s.name ? null : s.name) }}
                >
                  🛡 {s.name}
                </button>
              ))}
              {lane === 'enemy' && here.length === 0 && <span className="ui-sys">(敵は GM の描写で)</span>}
            </div>
          </div>
        )
      })}
      <div className="ui-sys">
        {disabled ? 'ターンの進行中は配置を変えられません' : picked ? `${picked} の移動先のレーンを押してください` : 'NPC を押して選び、移動先のレーンを押すと配置を変えます'}
      </div>
    </div>
  )
}
