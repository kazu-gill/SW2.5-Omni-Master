// localStorage はプライベートモードなどで投げることがあるので、必ずここを通す。
const SESSION_KEY = 'sw25.session'

export function loadSessionId(): number | null {
  try {
    const v = localStorage.getItem(SESSION_KEY)
    const n = v ? Number(v) : NaN
    return Number.isFinite(n) && n > 0 ? n : null
  } catch {
    return null
  }
}

export function saveSessionId(id: number | null) {
  try {
    if (id === null) localStorage.removeItem(SESSION_KEY)
    else localStorage.setItem(SESSION_KEY, String(id))
  } catch {
    /* 保存できなくても動く */
  }
}

export function loadFlag(key: string): boolean {
  try {
    return localStorage.getItem(key) === '1'
  } catch {
    return false
  }
}

export function saveFlag(key: string, on: boolean) {
  try {
    localStorage.setItem(key, on ? '1' : '0')
  } catch {
    /* 保存できなくても動く */
  }
}
