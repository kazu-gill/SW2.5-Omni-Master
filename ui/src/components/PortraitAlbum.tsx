interface Portrait {
  id: number
  npc_name: string
  file_path: string
}

interface Props {
  portraits: Portrait[]
  onRegenerate: (npcName: string) => void
}

export function PortraitAlbum({ portraits, onRegenerate }: Props) {
  return (
    <div className="portrait-album">
      <h2>ポートレート</h2>
      {portraits.length === 0 && <p className="empty">画像なし</p>}
      <div className="portraits-grid">
        {portraits.map((p) => (
          <div key={p.id} className="portrait-card">
            <img src={p.file_path} alt={p.npc_name} />
            <div className="portrait-name">{p.npc_name}</div>
            <button onClick={() => onRegenerate(p.npc_name)} className="btn-regen">
              再生成
            </button>
          </div>
        ))}
      </div>
    </div>
  )
}
