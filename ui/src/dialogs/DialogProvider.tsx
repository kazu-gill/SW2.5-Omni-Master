import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { DialogContext, type ConfirmOptions, type Dialogs } from './context'
import { errorText } from '../api'

interface Open extends ConfirmOptions {
  resolve: (ok: boolean) => void
}

export default function DialogProvider({ children }: { children: ReactNode }) {
  const [open, setOpen] = useState<Open | null>(null)
  const [typed, setTyped] = useState('')
  const [err, setErr] = useState('')
  const [running, setRunning] = useState(false)
  const ref = useRef<HTMLDialogElement>(null)
  const okRef = useRef<HTMLButtonElement>(null)
  const typeRef = useRef<HTMLInputElement>(null)
  const openRef = useRef<Open | null>(null)

  const show = useCallback((opts: ConfirmOptions) => new Promise<boolean>((resolve) => {
    openRef.current?.resolve(false) // 前の確認が開いたままなら、やめた扱い
    const next = { ...opts, resolve }
    openRef.current = next
    setTyped('')
    setErr('')
    setRunning(false)
    setOpen(next)
  }), [])

  const dialogs = useMemo<Dialogs>(() => ({
    confirm: (opts) => show(typeof opts === 'string' ? { message: opts } : opts),
    alert: (message, title = '') => show({ title, message, ok: '閉じる', cancel: '' }).then(() => {}),
  }), [show])

  useEffect(() => {
    const dlg = ref.current
    if (!dlg) return
    if (open && !dlg.open) {
      dlg.showModal()
      ;(open.typeName ? typeRef.current : okRef.current)?.focus()
    }
    if (!open && dlg.open) dlg.close()
  }, [open])

  function finish(ok: boolean) {
    const cur = openRef.current
    openRef.current = null
    setOpen(null)
    cur?.resolve(ok)
  }

  async function onOk() {
    if (!open) return
    if (open.typeName && typed.trim() !== open.typeName) return
    setRunning(true)
    try {
      if (open.run) await open.run()
      finish(true)
    } catch (e) {
      setErr('できませんでした: ' + errorText(e))
      setRunning(false)
    }
  }

  const blocked = running || (!!open?.typeName && typed.trim() !== open.typeName)

  return (
    <DialogContext.Provider value={dialogs}>
      {children}
      <dialog ref={ref} className="ui-dialog" onClose={() => { if (openRef.current) finish(false) }}>
        {open && (
          <form method="dialog" onSubmit={(e) => { e.preventDefault(); void onOk() }}>
            {open.title && <h2>{open.title}</h2>}
            {open.message && <div className="ui-msg">{open.message}</div>}
            {open.typeName && (
              <label>
                <span>確認のため「{open.typeName}」と入力してください</span>
                <input ref={typeRef} type="text" autoComplete="off" value={typed} onChange={(e) => setTyped(e.target.value)} />
              </label>
            )}
            {err && <div className="ui-err">{err}</div>}
            <div className="ui-acts">
              {open.cancel !== '' && (
                <button type="button" onClick={() => finish(false)}>{open.cancel ?? 'やめる'}</button>
              )}
              <button ref={okRef} type="submit" className={open.danger ? 'ui-ok danger' : 'ui-ok'} disabled={blocked}>
                {open.ok ?? 'OK'}
              </button>
            </div>
          </form>
        )}
      </dialog>
    </DialogContext.Provider>
  )
}
