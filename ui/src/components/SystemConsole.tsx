type SocketStatus = 'connecting' | 'connected' | 'disconnected'

interface Props {
  socketStatus: SocketStatus
  turnCount: number
}

const STATUS_LABEL: Record<SocketStatus, string> = {
  connecting: '接続中...',
  connected: '接続済み',
  disconnected: '切断',
}

const STATUS_COLOR: Record<SocketStatus, string> = {
  connecting: '#f0a500',
  connected: '#4caf50',
  disconnected: '#f44336',
}

export function SystemConsole({ socketStatus, turnCount }: Props) {
  return (
    <div className="system-console">
      <h2>システムコンソール</h2>
      <div className="console-row">
        <span>WebSocket</span>
        <span style={{ color: STATUS_COLOR[socketStatus] }}>
          {STATUS_LABEL[socketStatus]}
        </span>
      </div>
      <div className="console-row">
        <span>処理済みターン</span>
        <span>{turnCount}</span>
      </div>
      <div className="console-row">
        <span>GM (E4B)</span>
        <span className="port">:11430</span>
      </div>
      <div className="console-row">
        <span>Support (E2B)</span>
        <span className="port">:11431</span>
      </div>
      <div className="console-row">
        <span>NPC-A ガルド</span>
        <span className="port">:11432</span>
      </div>
      <div className="console-row">
        <span>NPC-B セラ</span>
        <span className="port">:11433</span>
      </div>
      <div className="console-row">
        <span>NPC-C ゼイン</span>
        <span className="port">:11434</span>
      </div>
      <div className="console-row">
        <span>ComfyUI</span>
        <span className="port">:8188</span>
      </div>
    </div>
  )
}
