// HP / MP の棒 (実値 cur/max)。
export default function Bar({ kind, cur, max }: { kind: 'hp' | 'mp'; cur: number; max: number }) {
  const pct = max > 0 ? Math.max(0, Math.min(100, (cur / max) * 100)) : 0
  return (
    <div className={`bar ${kind}`} title={`${kind.toUpperCase()} ${cur}/${max}`}>
      <span className="bar-label">{kind.toUpperCase()}</span>
      <div className="bar-track"><i style={{ width: `${pct}%` }} /></div>
      <span className="bar-num">{cur}/{max || '?'}</span>
    </div>
  )
}
