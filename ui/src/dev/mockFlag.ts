// 開発用: ?mock=1 で開くと、サーバー無しで作り物のセッションとターンを流す (dev/mock.ts)。
// 本番ビルドでは常に false。
export function isMock(): boolean {
  if (!import.meta.env.DEV) return false
  try {
    return new URLSearchParams(location.search).get('mock') === '1'
  } catch {
    return false
  }
}
