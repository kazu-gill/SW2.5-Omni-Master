// パーティ: このセッションの NPC (ペルソナから 3 体まで加える・外す・詳細と編集) と PC。
import { useEffect, useState } from 'react'
import { del, errorText, get, post } from '../../api'
import { useDialogs } from '../../dialogs/context'
import type { PersonaInfo, PlayerCharacterEntry, SessionState } from '../../types'
import NPCDetail from './NPCDetail'
import PCList from './PCList'

const MAX_NPC = 3

interface Props {
  state: SessionState | null
  busy: boolean
  devMode: boolean
  onSessionChanged: () => Promise<void>
  pcs: PlayerCharacterEntry[]
  pcsErr: string
  onPCsChanged: () => Promise<void>
}

export default function PartyView({ state, busy, devMode, onSessionChanged, pcs, pcsErr, onPCsChanged }: Props) {
  const ui = useDialogs()
  const [personas, setPersonas] = useState<PersonaInfo[]>([])
  const [personaErr, setPersonaErr] = useState('')
  const [pick, setPick] = useState('')
  const [selected, setSelected] = useState<string | null>(null)
  const [adding, setAdding] = useState(false)

  useEffect(() => {
    get<PersonaInfo[]>('/api/personas')
      .then((p) => setPersonas(p ?? []))
      .catch((e) => setPersonaErr(errorText(e)))
  }, [])

  const sheets = state?.npc_sheets ?? []
  const names = new Set(sheets.map((s) => s.name))
  const addable = personas.filter((p) => !names.has(p.name))
  const current = sheets.find((s) => s.name === selected) ?? sheets[0] ?? null

  const add = async () => {
    if (!state || !pick) return
    setAdding(true)
    try {
      await post('/api/npc-sheet', { session_id: state.session.id, persona_id: pick })
      const p = personas.find((x) => x.id === pick)
      setPick('')
      await onSessionChanged()
      if (p) setSelected(p.name)
    } catch (e) {
      await ui.alert(errorText(e), '仲間に加えられませんでした')
    } finally {
      setAdding(false)
    }
  }

  const remove = (name: string) => {
    if (!state) return
    void ui.confirm({
      title: `${name} をパーティから外しますか？`,
      message: 'このセッションの NPC シート (HP/MP・配置・編集したペルソナ) が消えます。ペルソナの元ファイルは残ります。',
      ok: '外す',
      danger: true,
      run: async () => {
        await del('/api/npc-sheet', { session_id: state.session.id, name })
        setSelected(null)
        await onSessionChanged()
      },
    })
  }

  return (
    <div className="page">
      <h2>パーティ</h2>
      <section className="ui-panel">
        <h3>NPC (このセッションの仲間・{sheets.length}/{MAX_NPC})</h3>
        {!state && <div className="ui-muted">NPC を加えるには、先に「セッション」でセッションを選んでください。</div>}
        {personaErr && <div className="err">ペルソナを読めませんでした: {personaErr}</div>}
        {state && (
          <>
            <div className="chips">
              {sheets.map((s) => (
                <button key={s.id} className={current?.id === s.id ? 'chip on' : 'chip'} onClick={() => setSelected(s.name)}>
                  🛡 {s.name}
                </button>
              ))}
            </div>
            {sheets.length < MAX_NPC ? (
              <div className="ui-row">
                <select value={pick} onChange={(e) => setPick(e.target.value)} aria-label="加えるペルソナ" disabled={busy}>
                  <option value="">ペルソナを選ぶ…</option>
                  {addable.map((p) => <option key={p.id} value={p.id}>{p.name} (HP {p.hp} / MP {p.mp})</option>)}
                </select>
                <button className="primary" disabled={!pick || adding || busy} onClick={() => void add()}>仲間に加える</button>
              </div>
            ) : <div className="ui-sys">3 体までです。入れ替えるときは誰かを外してください。</div>}
            {busy && <div className="ui-sys">ターンの進行中はパーティを変えられません</div>}
          </>
        )}
      </section>
      {current && (
        <NPCDetail
          key={`${current.id}:${current.yaml_blob.length}:${current.hp}:${current.mp}`}
          sheet={current}
          devMode={devMode}
          busy={busy}
          onSaved={onSessionChanged}
          onRemove={() => remove(current.name)}
        />
      )}
      <PCList pcs={pcs} pcsErr={pcsErr} onChanged={onPCsChanged} />
    </div>
  )
}
