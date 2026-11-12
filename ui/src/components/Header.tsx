// 上のバー: 名前・画面の切り替え・稼働状態 (LLM / 埋め込み / 画像)・DEV。
import type { SystemView } from '../hooks/useSystemStatus'
import type { ViewName } from '../types'

const NAV: { id: ViewName; label: string }[] = [
  { id: 'session', label: 'セッション' },
  { id: 'quest', label: 'クエスト' },
  { id: 'party', label: 'パーティ' },
  { id: 'rules', label: 'ルール' },
]

interface Props {
  view: ViewName
  setView: (v: ViewName) => void
  system: SystemView
  devMode: boolean
  setDevMode: (on: boolean) => void
}

function Dot({ label, ok, title }: { label: string; ok: boolean | null; title: string }) {
  return (
    <span className="sys-item" title={title}>
      <span className={`ui-dot ${ok === null ? '' : ok ? 'ok' : 'bad'}`} /> {label}
    </span>
  )
}

export default function Header({ view, setView, system, devMode, setDevMode }: Props) {
  const s = system.status
  const llmName = s?.llm.model_id ? s.llm.model_id.replace(/^.*\//, '') : 'LLM'
  return (
    <header className="ui-header app-header">
      <b>⚜ SW2.5 Omni-Master</b>
      <nav className="pills" aria-label="画面">
        {NAV.map((n) => (
          <button key={n.id} className={view === n.id ? 'pill on' : 'pill'} aria-current={view === n.id ? 'page' : undefined} onClick={() => setView(n.id)}>
            {n.label}
          </button>
        ))}
      </nav>
      <div className="sys">
        <Dot label={llmName} ok={s ? s.llm.ok : system.error ? false : null} title={s ? `LLM ${s.llm.url}` : system.error || '確かめています'} />
        <Dot label="埋め込み" ok={s ? s.embed.ok : system.error ? false : null} title={s ? `埋め込み ${s.embed.url}` : ''} />
        {s?.images && <Dot label="画像" ok={s.images.ok} title={`画像 ${s.images.url}`} />}
        <button className={devMode ? 'dev on' : 'dev'} aria-pressed={devMode} onClick={() => setDevMode(!devMode)} title="開発者向けの編集 (YAML の直接編集など) を出す">DEV</button>
      </div>
    </header>
  )
}
