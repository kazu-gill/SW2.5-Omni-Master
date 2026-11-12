// ルールの追加 (ハウスルール・訂正) と編集。
import { useState } from 'react'
import Modal from '../../components/Modal'
import { errorText, patch, post } from '../../api'
import type { RuleEntry } from '../../types'
import { pageText } from '../../utils/format'

export type RuleEditTarget =
  | { kind: 'new' }
  | { kind: 'correct'; of: RuleEntry }
  | { kind: 'edit'; rule: RuleEntry }

interface Props {
  target: RuleEditTarget
  onClose: () => void
  /** 保存できた。embedded が false なら埋め込みに失敗 */
  onSaved: (embedded: boolean | undefined) => void
}

export default function RuleEditDialog({ target, onClose, onSaved }: Props) {
  const init = target.kind === 'edit' ? target.rule : null
  const [sourceType, setSourceType] = useState<'houserule' | 'correction'>(target.kind === 'correct' ? 'correction' : 'houserule')
  const [tag, setTag] = useState(init?.tag ?? (target.kind === 'correct' ? target.of.tag : ''))
  const [text, setText] = useState(init?.text ?? (target.kind === 'correct' ? target.of.text : ''))
  const [overrides, setOverrides] = useState(target.kind === 'correct' ? String(target.of.id) : '')
  const [saving, setSaving] = useState(false)
  const [err, setErr] = useState('')

  const save = async () => {
    if (!text.trim()) { setErr('本文を入れてください'); return }
    setSaving(true)
    setErr('')
    try {
      if (target.kind === 'edit') {
        await patch(`/api/rules/${target.rule.id}`, { text, tag, enabled: target.rule.enabled })
        onSaved(undefined)
      } else {
        const r = await post<RuleEntry>('/api/rules', {
          source_type: sourceType, tag, text, overrides_id: overrides ? Number(overrides) : null,
        })
        onSaved(r?.embedded)
      }
    } catch (e) {
      setErr(`保存できませんでした: ${errorText(e)}`)
    } finally {
      setSaving(false)
    }
  }

  const title = target.kind === 'edit' ? `ルール #${target.rule.id} を直す` : target.kind === 'correct' ? `ルール #${target.of.id} の訂正を作る` : 'ハウスルール・訂正を足す'

  return (
    <Modal
      title={title}
      onClose={onClose}
      footer={
        <>
          {err && <span className="err grow">{err}</span>}
          <button onClick={onClose}>やめる</button>
          <button className="primary" disabled={saving} onClick={() => void save()}>{saving ? '保存しています…' : '保存'}</button>
        </>
      }
    >
      {target.kind === 'correct' && (
        <div className="rule-text">
          <div className="ui-muted">元の本文 {target.of.section} {pageText(target.of)}</div>
          {target.of.text}
        </div>
      )}
      {target.kind === 'new' && (
        <label className="field">種類
          <select value={sourceType} onChange={(e) => setSourceType(e.target.value as 'houserule' | 'correction')}>
            <option value="houserule">ハウスルール (いちばん優先)</option>
            <option value="correction">訂正 (ルールブックより優先)</option>
          </select>
        </label>
      )}
      {target.kind === 'new' && sourceType === 'correction' && (
        <label className="field">訂正する元のルールの番号 (任意)
          <input inputMode="numeric" value={overrides} onChange={(e) => setOverrides(e.target.value.replace(/\D/g, ''))} />
        </label>
      )}
      <label className="field">タグ (検索の手掛かり)
        <input value={tag} onChange={(e) => setTag(e.target.value)} placeholder="例: 戦闘, かばう" />
      </label>
      <label className="field">本文
        <textarea rows={8} value={text} onChange={(e) => setText(e.target.value)} />
      </label>
      <div className="ui-sys">保存すると検索用に埋め込みます (埋め込みモデルが止まっていると、文字の一致でしか見つかりません)。</div>
    </Modal>
  )
}
