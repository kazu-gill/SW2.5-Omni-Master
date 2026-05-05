import { useEffect, useRef, useState, useCallback } from 'react'
import type { TurnResult, SocketStatus, ImageUpdate } from '../types'

export function useGameSocket() {
  const [turns, setTurns] = useState<TurnResult[]>([])
  const [status, setStatus] = useState<SocketStatus>('disconnected')
  const wsRef = useRef<WebSocket | null>(null)
  const retryDelay = useRef(3000)
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const connect = useCallback(() => {
    if (wsRef.current?.readyState === WebSocket.CONNECTING || wsRef.current?.readyState === WebSocket.OPEN) return
    const proto = location.protocol === 'https:' ? 'wss' : 'ws'
    const ws = new WebSocket(`${proto}://${location.host}/ws`)
    wsRef.current = ws
    setStatus('connecting')

    ws.onopen = () => {
      setStatus('connected')
      retryDelay.current = 3000
    }
    ws.onclose = () => {
      setStatus('disconnected')
      const delay = retryDelay.current
      retryDelay.current = Math.min(delay * 2, 30000)
      timerRef.current = setTimeout(connect, delay)
    }
    ws.onerror = () => ws.close()
    ws.onmessage = (e) => {
      try {
        const msg = JSON.parse(e.data)
        if ('image_url' in msg && !('gm_narration' in msg)) {
          const update = msg as ImageUpdate
          setTurns((prev) => prev.map((t) =>
            t.turn === update.turn && t.session_id === update.session_id
              ? { ...t, image_url: update.image_url }
              : t
          ))
        } else {
          setTurns((prev) => [...prev, msg as TurnResult])
        }
      } catch { /* ignore malformed */ }
    }
  }, [])

  useEffect(() => {
    connect()
    return () => {
      if (timerRef.current) clearTimeout(timerRef.current)
      wsRef.current?.close()
    }
  }, [connect])

  const removeTurn = useCallback((turn: number) => {
    setTurns((prev) => prev.filter((t) => t.turn !== turn))
  }, [])

  const resetTurns = useCallback((incoming: TurnResult[]) => {
    setTurns(incoming)
  }, [])

  return { turns, status, removeTurn, resetTurns }
}
