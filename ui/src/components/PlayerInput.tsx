import { useState } from 'react'

interface Props {
  sessionId: number
  turn: number
  onResult: () => void
}

export function PlayerInputForm({ sessionId, turn, onResult }: Props) {
  const [text, setText] = useState('')
  const [sending, setSending] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (!text.trim()) return
    setSending(true)
    setError(null)
    try {
      const res = await fetch('/api/turn', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ session_id: sessionId, turn, text }),
      })
      if (!res.ok) throw new Error(await res.text())
      setText('')
      onResult()
    } catch (e) {
      setError(String(e))
    } finally {
      setSending(false)
    }
  }

  return (
    <form className="player-input" onSubmit={handleSubmit}>
      <textarea
        value={text}
        onChange={(e) => setText(e.target.value)}
        placeholder="行動を宣言してください..."
        rows={2}
        disabled={sending}
        onKeyDown={(e) => {
          if (e.key === 'Enter' && !e.shiftKey) {
            e.preventDefault()
            handleSubmit(e as unknown as React.FormEvent)
          }
        }}
      />
      <button type="submit" disabled={sending || !text.trim()}>
        {sending ? '処理中...' : '行動宣言 (Enter)'}
      </button>
      {error && <p className="error">{error}</p>}
    </form>
  )
}
