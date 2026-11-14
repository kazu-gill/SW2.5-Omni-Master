// 入力欄: 行動 / GM直訴 の切り替え、Enter で送る (Shift+Enter で改行)、進行中は ⏹。
import { useLayoutEffect, useRef } from 'react'
import type { TurnMode } from '../../types'

interface Props {
  text: string
  setText: (v: string) => void
  mode: TurnMode
  setMode: (m: TurnMode) => void
  /** このタブでターンが進行中 (⏹ を出す) */
  running: boolean
  /** 送れない (他のタブの進行・要約中・接続待ちなど) */
  blocked: boolean
  blockedReason: string
  onSend: () => void
  onStop: () => void
}

export default function Composer({ text, setText, mode, setMode, running, blocked, blockedReason, onSend, onStop }: Props) {
  const ref = useRef<HTMLTextAreaElement>(null)
  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    el.style.height = 'auto'
    el.style.height = `${el.scrollHeight + 2}px`
  }, [text])

  const disabled = running || blocked
  return (
    <div className="composer-wrap">
      {!running && blockedReason && <div className="ui-sys center">{blockedReason}</div>}
      <div className="composer">
        <div className="seg" role="group" aria-label="入力の種類">
          <button className={mode === 'action' ? 'on' : ''} aria-pressed={mode === 'action'} onClick={() => setMode('action')}
            title="PC の行動を宣言する。GM・NPC・数値確認が動く">行動</button>
          <button className={mode === 'gm' ? 'on gm' : ''} aria-pressed={mode === 'gm'} onClick={() => setMode('gm')}
            title="GM にだけ質問・相談する。NPC と HP/MP は動かない">GM直訴</button>
        </div>
        <textarea
          ref={ref}
          rows={1}
          value={text}
          disabled={disabled}
          placeholder={mode === 'gm' ? 'GM への質問・相談 (NPC と数値は動きません)' : 'あなたの行動を宣言 (Enter で送る)'}
          aria-label="入力"
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
              e.preventDefault()
              if (!disabled && text.trim()) onSend()
            }
          }}
        />
        {running ? (
          <button className="danger" onClick={onStop} title="このターンを止める (続きから再開できる)">⏹ 止める</button>
        ) : (
          <button className="primary" disabled={disabled || !text.trim()} onClick={onSend}>送る</button>
        )}
      </div>
    </div>
  )
}
