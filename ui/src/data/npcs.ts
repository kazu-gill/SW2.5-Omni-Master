import type { AlbumEntry } from '../types'

const PORTRAIT_COLORS = [
  ['#2a1a0a', '#8a6030'],
  ['#0a1a2a', '#3060a0'],
  ['#0a0a1a', '#603090'],
  ['#0a1a0a', '#306030'],
  ['#1a0a0a', '#903030'],
]

function makeGradientSvg(c1: string, c2: string, icon: string, name: string, w: number, h: number, fontSize: number, labelY: number, seedText: string): string {
  return `<svg xmlns='http://www.w3.org/2000/svg' width='${w}' height='${h}'>
    <defs><linearGradient id='g' x1='0' y1='0' x2='0' y2='1'>
      <stop offset='0%' stop-color='${c1}'/>
      <stop offset='100%' stop-color='${c2}'/>
    </linearGradient></defs>
    <rect width='${w}' height='${h}' fill='url(#g)'/>
    <text x='${w / 2}' y='${h * 0.44}' text-anchor='middle' font-size='${fontSize}' fill='rgba(255,255,255,0.15)'>${icon}</text>
    <text x='${w / 2}' y='${labelY}' text-anchor='middle' font-size='13' fill='rgba(255,255,255,0.3)' font-family='serif'>${name}</text>
    <text x='${w / 2}' y='${h - 16}' text-anchor='middle' font-size='9' fill='rgba(255,255,255,0.2)' font-family='monospace'>${seedText}</text>
  </svg>`
}

export function makePortraitMock(name: string, icon: string, idx: number): { seed: number; dataUrl: string } {
  const [c1, c2] = PORTRAIT_COLORS[idx % PORTRAIT_COLORS.length]
  const seed = Math.floor(Math.random() * 9999999)
  const svg = makeGradientSvg(c1, c2, icon, name, 200, 356, 52, 200, '[ComfyUI MOCK]')
  return { seed, dataUrl: 'data:image/svg+xml;base64,' + btoa(unescape(encodeURIComponent(svg))) }
}

export function makeAlbumThumb(name: string, icon: string, idx: number, entry: AlbumEntry): string {
  if (entry.dataUrl) return entry.dataUrl
  const [c1, c2] = PORTRAIT_COLORS[idx % PORTRAIT_COLORS.length]
  const svg = makeGradientSvg(c1, c2, icon, name, 160, 284, 44, 160, `SEED:${entry.seed ?? 'upload'}`)
  return 'data:image/svg+xml;base64,' + btoa(unescape(encodeURIComponent(svg)))
}
