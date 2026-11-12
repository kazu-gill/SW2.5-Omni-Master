// 画面共通の確認とお知らせ (MLX の dialogs.js と同じ使い方)。ブラウザの confirm / alert は使わない。
//   const ui = useDialogs()
//   await ui.confirm({ title, message, ok, danger, typeName, run })  → true / false
//     run: OK のあとに実行する処理。失敗したら確認を開いたまま理由を出す (もう一度押せる)
//   await ui.alert(message, title)
import { createContext, useContext } from 'react'

export interface ConfirmOptions {
  title?: string
  message?: string
  ok?: string
  cancel?: string
  danger?: boolean
  /** その文字を打つまで OK を押せない (取り返しのつかない削除に) */
  typeName?: string
  run?: () => Promise<unknown> | unknown
}

export interface Dialogs {
  confirm: (opts: ConfirmOptions | string) => Promise<boolean>
  alert: (message: string, title?: string) => Promise<void>
}

export const DialogContext = createContext<Dialogs | null>(null)

export function useDialogs(): Dialogs {
  const d = useContext(DialogContext)
  if (!d) throw new Error('DialogProvider の外で useDialogs が呼ばれました')
  return d
}
