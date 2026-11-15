# SW2.5 Omni-Master — UI

Go オーケストレーター (:8080) の Web UI です。React 19 + TypeScript + Vite。API の取り決め (エンドポイント・WebSocket のイベント・型) は [../docs/api.md](../docs/api.md)、画面の説明は [../docs/spec_ja.md](../docs/spec_ja.md) の「9. フロントエンド画面構成」にあります。

## 準備

Node.js のバージョンは `package.json` の `volta` で固定しています ([Volta](https://volta.sh/) が自動で切り替えます)。

```bash
npm install
```

## コマンド

| コマンド | 内容 |
|---------|------|
| `npm run dev` | Vite 開発サーバー (:5173、HMR 付き) |
| `npm run build` | 型チェック (`tsc -b`) とプロダクションビルド (`dist/`) |
| `npm run lint` | ESLint |
| `npm run preview` | ビルド結果の確認用サーバー |

通常はリポジトリのルートで `pwsh scripts/Start-Omni.ps1` を実行すると、オーケストレーターと一緒に開発サーバー (`npm run dev -- --host`) が起動します。

## オーケストレーターへの接続

開発サーバーは `/api` と `/ws` をオーケストレーターへプロキシします (`vite.config.ts`)。既定の接続先は `localhost:8080` で、環境変数 `OMNI_API` で変えられます。

```bash
OMNI_API=host:port npx vite
```

## モック (`?mock=1`)

開発サーバーで `http://localhost:5173/?mock=1` を開くと、オーケストレーターなしで作り物のセッションとターンを再生します (`src/dev/mock.ts`)。見た目や進行表示の確認用で、本番ビルドでは常に無効です。

## 構成

| パス | 役割 |
|------|------|
| `src/App.tsx` | 画面の切り替え (開始・セッション・クエスト・パーティ・ルール) |
| `src/api.ts` | REST 呼び出し (`?mock=1` のときは `dev/mock.ts` に切り替える) |
| `src/types.ts` | API の型 ([../docs/api.md](../docs/api.md) の「型」と対応) |
| `src/views/StartView.tsx` | 開始画面 (続きから / 新しく) |
| `src/views/session/` | セッション画面。会話ログ (`ChatLog` / `TurnView` / `Bubbles`)、進行中のターン (`LiveTurnView`)、止まったターン (`StoppedTurnView`)、入力欄 (`Composer`)、配置 (`Formation`)、横のパネル (`SidePanel`) |
| `src/views/QuestView.tsx` | クエスト画面 |
| `src/views/party/` | パーティ画面 (NPC の詳細・編集、PC の一覧・編集) |
| `src/views/rules/` | ルール画面 (検索・一覧・ハウスルールと訂正の編集) |
| `src/hooks/useSessionGame.ts` | セッションの状態の読み込みと更新 |
| `src/hooks/useSessionSocket.ts` | WebSocket (`/ws?session_id=`) の接続とつなぎ直し |
| `src/hooks/useSystemStatus.ts` | LLM・埋め込み・画像工房の稼働状態 (`GET /api/system`) |
| `src/live/turnReducer.ts` | WebSocket のイベントから進行中のターンの表示を組み立てる |
| `src/components/` | ヘッダー・モーダル・HP/MP バー |
| `src/dialogs/` | 確認・通知のダイアログ |
| `src/utils/` | NPC の YAML・PC シート・クエストの適性・保存 (localStorage) などの補助 |
| `src/styles/` | テーマ (`theme.css` / `ui.css` は MLX の画面と共通の見た目) と画面ごとの CSS |
| `src/dev/` | `?mock=1` の作り物の API |

## ターンの表示の流れ

1. 入力を `POST /api/sessions/{id}/turn` で送ると、すぐ 202 が返る
2. 以降は WebSocket の `stage` / `delta` / `rules` / `check` を `turnReducer` が受けて、段ごとの進行と本文を逐次表示する
3. `turn` イベントで確定したターンを会話ログに足す
4. `error` イベントに `resumable: true` が付いていれば、セッションの状態を読み直して止まったターン (`stopped_turn`) を表示し、「▶ 続きから」(`/resume`)・「取り消す」(`/discard`)・「✎ 直す」を選ばせる。選ぶまで入力欄と他の操作は使えない
