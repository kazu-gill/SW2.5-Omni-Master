// 会話ログ。下端から 120px 以内にいるときだけ、描き直しのたびに下へ送る (読み返し中は動かさない)。
import { useLayoutEffect, useRef, type ReactNode } from 'react'

interface Props {
  children: ReactNode
  /** 変わるたびに下へ送るか判断する値 (ターン数・本文の長さなど) */
  version: string
  /** 変わったら近さに関係なく下へ送る (送信した直後) */
  jump: number
}

export default function ChatLog({ children, version, jump }: Props) {
  const ref = useRef<HTMLDivElement>(null)
  const near = useRef(true)

  const onScroll = () => {
    const el = ref.current
    if (el) near.current = el.scrollHeight - el.scrollTop - el.clientHeight < 120
  }

  useLayoutEffect(() => {
    const el = ref.current
    if (el && near.current) el.scrollTop = el.scrollHeight
  }, [version])

  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    el.scrollTop = el.scrollHeight
    near.current = true
  }, [jump])

  return (
    <div className="log" ref={ref} onScroll={onScroll}>
      {children}
    </div>
  )
}
