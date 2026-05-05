const GRID_SIZE = 10

interface Position {
  name: string
  x: number
  y: number
  isPlayer?: boolean
}

interface Props {
  positions: Position[]
}

export function BattleMap({ positions }: Props) {
  const grid: Record<string, Position> = {}
  for (const p of positions) {
    grid[`${p.x},${p.y}`] = p
  }

  return (
    <div className="battle-map">
      <h2>バトルマップ</h2>
      <div className="grid" style={{ gridTemplateColumns: `repeat(${GRID_SIZE}, 1fr)` }}>
        {Array.from({ length: GRID_SIZE }, (_, y) =>
          Array.from({ length: GRID_SIZE }, (_, x) => {
            const key = `${x},${y}`
            const occupant = grid[key]
            return (
              <div key={key} className={`cell ${occupant ? (occupant.isPlayer ? 'player' : 'npc') : ''}`}>
                {occupant && <span className="occupant-label">{occupant.name[0]}</span>}
              </div>
            )
          })
        )}
      </div>
    </div>
  )
}
