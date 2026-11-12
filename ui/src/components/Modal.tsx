import { useEffect, useRef, type ReactNode } from 'react'

interface Props {
  title: ReactNode
  onClose: () => void
  children: ReactNode
  /** 下の操作ボタン */
  footer?: ReactNode
  wide?: boolean
}

/** 編集用の大きめのダイアログ (<dialog> の showModal)。Esc と ✕ で閉じる。 */
export default function Modal({ title, onClose, children, footer, wide }: Props) {
  const ref = useRef<HTMLDialogElement>(null)
  useEffect(() => {
    const dlg = ref.current
    if (dlg && !dlg.open) dlg.showModal()
    return () => { if (dlg?.open) dlg.close() }
  }, [])
  return (
    <dialog ref={ref} className={wide ? 'modal wide' : 'modal'} onCancel={(e) => { e.preventDefault(); onClose() }}>
      <div className="modal-head">
        <b>{title}</b>
        <button className="icon-btn" onClick={onClose} aria-label="閉じる">✕</button>
      </div>
      <div className="modal-body">{children}</div>
      {footer && <div className="modal-foot">{footer}</div>}
    </dialog>
  )
}
