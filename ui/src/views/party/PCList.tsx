// PC の一覧と作成・編集 (ゆとシート風)・使う PC の切り替え・削除。
import { useState } from 'react'
import { del, errorText, patch, post, put } from '../../api'
import { useDialogs } from '../../dialogs/context'
import type { PCSheet, PlayerCharacterEntry } from '../../types'
import { makeEmptySheet, readPCSheet } from '../../utils/pcSheet'
import PCEditView from './PCEditView'

interface Props {
  pcs: PlayerCharacterEntry[]
  pcsErr: string
  onChanged: () => Promise<void>
}

export default function PCList({ pcs, pcsErr, onChanged }: Props) {
  const ui = useDialogs()
  const [editing, setEditing] = useState<{ id: number | null; sheet: PCSheet } | null>(null)

  const run = async (f: () => Promise<unknown>, what: string) => {
    try {
      await f()
      await onChanged()
    } catch (e) {
      await ui.alert(errorText(e), `${what}できませんでした`)
    }
  }

  const save = async (id: number | null, sheet: PCSheet) => {
    const body = { name: sheet.name || 'PC', json_blob: JSON.stringify(sheet) }
    if (id === null) {
      const r = await post<{ id: number }>('/api/player-characters', body)
      if (!pcs.some((p) => p.is_active)) await patch(`/api/player-characters/${r.id}/activate`)
    } else {
      await put(`/api/player-characters/${id}`, body)
    }
    await onChanged()
    setEditing(null)
  }

  const remove = (p: PlayerCharacterEntry) => {
    void ui.confirm({
      title: `「${p.name}」を消しますか？`,
      message: 'PC シートが消えます。元に戻せません。',
      ok: '消す',
      danger: true,
      run: () => run(() => del(`/api/player-characters/${p.id}`), '消去'),
    })
  }

  return (
    <section className="ui-panel">
      <div className="ui-row">
        <h3 className="grow">PC (プレイヤーキャラクター)</h3>
        <button onClick={() => setEditing({ id: null, sheet: makeEmptySheet() })}>＋ PC を作る</button>
      </div>
      {pcsErr && <div className="err">PC を読めませんでした: {pcsErr}</div>}
      {pcs.length === 0 && !pcsErr && <div className="ui-muted">PC がいません。「＋ PC を作る」から作ってください。</div>}
      <div className="list">
        {pcs.map((p) => {
          const s = readPCSheet(p.json_blob)
          return (
            <div key={p.id} className={p.is_active ? 'list-row on' : 'list-row'}>
              <div className="grow">
                <b>{p.name}</b> {p.is_active && <span className="chip on">使用中</span>}
                <div className="ui-sys">{s.race} 冒険者Lv{s.adventurerLevel} ・ {s.classes.map((c) => `${c.name}${c.level}`).join(' / ')} ・ HP {s.hpCurrent}/{s.hpMax} MP {s.mpCurrent}/{s.mpMax}</div>
              </div>
              <div className="ui-row">
                {p.is_active
                  ? <button onClick={() => void run(() => patch(`/api/player-characters/${p.id}/deactivate`), '切り替え')}>使わない</button>
                  : <button onClick={() => void run(() => patch(`/api/player-characters/${p.id}/activate`), '切り替え')}>使う</button>}
                <button onClick={() => setEditing({ id: p.id, sheet: s })}>✎ 編集</button>
                <button className="danger" onClick={() => remove(p)} aria-label={`${p.name} を消す`}>🗑</button>
              </div>
            </div>
          )
        })}
      </div>
      {editing && <PCEditView pcId={editing.id} initial={editing.sheet} onSave={save} onClose={() => setEditing(null)} />}
    </section>
  )
}
