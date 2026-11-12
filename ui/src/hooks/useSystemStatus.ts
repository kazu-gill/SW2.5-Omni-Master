// GET /api/system を 15 秒ごとに見る (LLM・埋め込み・画像の稼働)。
import { useEffect, useState } from 'react'
import { get, errorText } from '../api'
import type { SystemStatus } from '../types'

export interface SystemView {
  status: SystemStatus | null
  /** オーケストレーター自体に届かない */
  error: string
}

export function useSystemStatus(): SystemView {
  const [view, setView] = useState<SystemView>({ status: null, error: '' })
  useEffect(() => {
    let alive = true
    const load = async () => {
      try {
        const s = await get<SystemStatus>('/api/system')
        if (alive) setView({ status: s, error: '' })
      } catch (e) {
        if (alive) setView({ status: null, error: errorText(e) })
      }
    }
    void load()
    const t = setInterval(load, 15000)
    return () => { alive = false; clearInterval(t) }
  }, [])
  return view
}
