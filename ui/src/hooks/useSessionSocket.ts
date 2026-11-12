// WebSocket /ws?session_id= を 1 本持つ。切れたら 1 → 2 → 4 … 15 秒の間隔でつなぎ直す。
// delta (本文の追加分) は 1 フレームにまとめて渡す (rAF で間引いて描き直す)。
import { useEffect, useRef, useState } from 'react'
import type { WsMessage } from '../types'
import type { DeltaItem } from '../live/turnReducer'
import { isMock } from '../dev/mockFlag'

export type SocketStatus = 'connecting' | 'open' | 'closed'

export interface SocketHandlers {
  onMessage: (m: Exclude<WsMessage, { type: 'delta' }>) => void
  onDeltas: (items: DeltaItem[]) => void
  /** つながったとき (つなぎ直しを含む)。状態を読み直す合図 */
  onOpen: () => void
}

export function useSessionSocket(sessionId: number | null, handlers: SocketHandlers): SocketStatus {
  const [status, setStatus] = useState<SocketStatus>('connecting')
  const h = useRef(handlers)
  useEffect(() => { h.current = handlers })

  useEffect(() => {
    if (sessionId === null) return
    let ws: WebSocket | null = null
    let timer: ReturnType<typeof setTimeout> | undefined
    let delay = 1000
    let stopped = false
    let pending: DeltaItem[] = []
    let frame = 0
    let closeMock: (() => void) | null = null

    const flush = () => {
      frame = 0
      const items = pending
      pending = []
      if (items.length) h.current.onDeltas(items)
    }
    const receive = (m: WsMessage) => {
      if (m.session_id !== undefined && m.session_id !== sessionId) return
      if (m.type === 'delta') {
        pending.push({ turn: m.turn, stage: m.stage, text: m.text })
        if (!frame) frame = requestAnimationFrame(flush)
        return
      }
      if (pending.length) { cancelAnimationFrame(frame); flush() } // 順番を崩さない
      h.current.onMessage(m)
    }

    if (import.meta.env.DEV && isMock()) {
      void import('../dev/mock').then(({ mockConnect }) => {
        if (stopped) return
        setStatus('open')
        closeMock = mockConnect(sessionId, receive)
        h.current.onOpen()
      })
      return () => { stopped = true; closeMock?.(); cancelAnimationFrame(frame) }
    }

    const connect = () => {
      if (stopped) return
      const proto = location.protocol === 'https:' ? 'wss' : 'ws'
      ws = new WebSocket(`${proto}://${location.host}/ws?session_id=${sessionId}`)
      ws.onopen = () => {
        delay = 1000
        setStatus('open')
        h.current.onOpen()
      }
      ws.onmessage = (e) => {
        try {
          receive(JSON.parse(String(e.data)) as WsMessage)
        } catch {
          console.warn('ws: 読めないメッセージ', e.data)
        }
      }
      ws.onerror = () => ws?.close()
      ws.onclose = () => {
        if (stopped) return
        setStatus('closed')
        timer = setTimeout(() => { setStatus('connecting'); connect() }, delay)
        delay = Math.min(delay * 2, 15000)
      }
    }
    connect()
    return () => {
      stopped = true
      clearTimeout(timer)
      cancelAnimationFrame(frame)
      if (ws) { ws.onclose = null; ws.close() }
    }
  }, [sessionId])

  return status
}
