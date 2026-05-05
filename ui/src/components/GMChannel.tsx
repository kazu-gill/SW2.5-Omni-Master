import { useState } from 'react'

interface Props {
  sessionId: number
  turn: number
  onResult: (narration: string) => void
}

export function GMChannel({ sessionId, turn, onResult }: Props) {
  const [text, setText] = useState('')
  const [sending, setSending] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (!text.trim()) return
    setSending(true)
    setError(null)
    try {
      const res = await fetch('/api/gm-channel', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ session_id: sessionId, turn, text }),
      })
      if (!res.ok) throw new Error(await res.text())
      const data = await res.json()
      onResult(data.gm_narration)
      setText('')
    } catch (e) {
      setError(String(e))
    } finally {
      setSending(false)
    }
  }

  return (
    <div className="gm-channel">
      <h2>GM チャンネル（直訴）</h2>
      <form onSubmit={handleSubmit}>
        <textarea
          value={text}
          onChange={(e) => setText(e.target.value)}
          placeholder="判定への異議、NPCへの直接命令など..."
          rows={3}
          disabled={sending}
        />
        <button type="submit" disabled={sending || !text.trim()}>
          {sending ? '送信中...' : '送信'}
        </button>
      </form>
      {error && <p className="error">{error}</p>}
    </div>
  )
}
