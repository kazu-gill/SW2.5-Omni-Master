// オーケストレーター (:8080) への呼び出し。fetch はここ以外で直接書かない。
// 失敗は {"error": "..."} の文言で Error を投げる (画面はエラー行か ui.alert で出す)。
// ターンの POST は 202 ですぐ返るので、タイムアウトは付けない (進行は WebSocket で届く)。

import { isMock } from './dev/mockFlag'

export class ApiError extends Error {
  status: number
  constructor(message: string, status: number) {
    super(message)
    this.status = status
  }
}

type Method = 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'

export async function api<T = void>(path: string, method: Method = 'GET', body?: unknown): Promise<T> {
  if (import.meta.env.DEV && isMock()) {
    const { mockApi } = await import('./dev/mock')
    return mockApi(path, method, body) as Promise<T>
  }
  let res: Response
  try {
    res = await fetch(path, {
      method,
      headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
      body: body === undefined ? undefined : JSON.stringify(body),
    })
  } catch {
    throw new ApiError('オーケストレーター (:8080) に接続できません。サーバーが起動しているか確かめてから、もう一度試してください。', 0)
  }
  if (!res.ok) {
    let msg: string
    const text = await res.text().catch(() => '')
    try {
      const j = JSON.parse(text) as { error?: string }
      msg = j.error ?? ''
    } catch {
      msg = text.trim()
    }
    if (!msg) msg = `サーバーがエラーを返しました (${res.status})。しばらく待ってからもう一度試してください。`
    throw new ApiError(msg, res.status)
  }
  if (res.status === 204) return undefined as T
  const text = await res.text()
  if (!text) return undefined as T
  return JSON.parse(text) as T
}

export const get = <T>(path: string) => api<T>(path, 'GET')
export const post = <T = void>(path: string, body?: unknown) => api<T>(path, 'POST', body ?? {})
export const put = <T = void>(path: string, body?: unknown) => api<T>(path, 'PUT', body ?? {})
export const patch = <T = void>(path: string, body?: unknown) => api<T>(path, 'PATCH', body ?? {})
export const del = <T = void>(path: string, body?: unknown) => api<T>(path, 'DELETE', body)

export function errorText(e: unknown): string {
  return e instanceof Error ? e.message : String(e)
}
