import { useState, useRef, useEffect } from 'react'
import type { TurnResult, NPCSheet, ChatEntry, SocketStatus, QuestEntry } from '../types'
import { questEligibility, ELIGIBILITY_LABEL, ELIGIBILITY_COLOR } from '../utils/questEligibility'

const SESSION_ID = 1

interface Props {
  turns: TurnResult[]
  status?: SocketStatus
  npcSheets: NPCSheet[]
  quests: QuestEntry[]
  partyAvgLevel: number | null
  onSheetsChange: () => void
  onQuestAccept: (id: number) => void
  devMode?: boolean
  onRemoveTurn?: (turn: number) => void
}

type ProcessState = 'idle' | 'processing' | 'error'
type LaneKey = 'enemy' | 'front' | 'party'

function npcLaneKey(s: NPCSheet): LaneKey {
  if (s.PositionY <= 2) return 'enemy'
  if (s.PositionY <= 5) return 'front'
  return 'party'
}

const LANE_DEFS: { key: LaneKey; cls: string; label: string }[] = [
  { key: 'enemy', cls: 'enemy-rear', label: 'Enemy Rear — 敵後衛' },
  { key: 'front', cls: 'front-line', label: 'Front Line — フロント' },
  { key: 'party', cls: 'party-rear', label: 'Party Rear — 味方後衛' },
]

export default function SessionView({ turns, npcSheets, quests, partyAvgLevel, onSheetsChange, onQuestAccept, devMode, onRemoveTurn }: Props) {
  const [text, setText] = useState('')
  const [gmText, setGmText] = useState('')
  const [currentTurn, setCurrentTurn] = useState(1)
  const [processState, setProcessState] = useState<ProcessState>('idle')
  const [elapsed, setElapsed] = useState(0)
  const [errorMsg, setErrorMsg] = useState('')
  const [comfyAvailable, setComfyAvailable] = useState<boolean | null>(null)

  // Battle map edit mode
  const [editMode, setEditMode] = useState(false)
  const [selectedNPC, setSelectedNPC] = useState<string | null>(null)

  // Guild panel
  const [guildOpen, setGuildOpen] = useState(false)

  const chatEndRef = useRef<HTMLDivElement>(null)
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null)

  const loading = processState === 'processing'

  function startTimer() {
    setElapsed(0)
    timerRef.current = setInterval(() => setElapsed((s) => s + 1), 1000)
  }
  function stopTimer() {
    if (timerRef.current) { clearInterval(timerRef.current); timerRef.current = null }
  }

  useEffect(() => {
    fetch('/api/comfy/status', { signal: AbortSignal.timeout(3000) })
      .then((r) => r.json())
      .then((d) => setComfyAvailable(d.available))
      .catch(() => setComfyAvailable(false))
    return () => stopTimer()
  }, [])

  // Exit edit mode whenever loading starts
  useEffect(() => {
    if (loading) { setEditMode(false); setSelectedNPC(null) }
  }, [loading])

  const chatEntries: ChatEntry[] = []
  for (const t of turns) {
    chatEntries.push({ role: 'player', speaker: 'Player', content: t.gm_narration ? '(アクション送信)' : '', turn: t.turn })
    if (t.gm_narration) {
      chatEntries.push({ role: 'gm', speaker: 'GM', content: t.gm_narration, turn: t.turn, image_url: t.image_url })
    }
    for (const npc of t.npc_actions ?? []) {
      if (npc.Dialogue) {
        chatEntries.push({ role: 'npc', speaker: npc.Name, content: `[${npc.Action}] ${npc.Dialogue}`, turn: t.turn })
      }
    }
  }

  useEffect(() => {
    chatEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [chatEntries.length])

  async function deleteTurn(turn: number) {
    await fetch(`/api/session-log/turn/${turn}?session_id=${SESSION_ID}`, { method: 'DELETE' })
    onRemoveTurn?.(turn)
  }

  async function sendRequest(endpoint: string, bodyText: string) {
    if (!bodyText.trim() || loading) return
    setProcessState('processing')
    setErrorMsg('')
    startTimer()
    try {
      const res = await fetch(endpoint, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ session_id: SESSION_ID, turn: currentTurn, text: bodyText }),
        signal: AbortSignal.timeout(180000),
      })
      if (!res.ok) {
        const msg = await res.text().catch(() => `HTTP ${res.status}`)
        throw new Error(msg || `HTTP ${res.status}`)
      }
      setCurrentTurn((n) => n + 1)
      onSheetsChange()
      setProcessState('idle')
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err)
      setErrorMsg(msg.includes('AbortError') || msg.includes('timed out') ? 'タイムアウト（180秒）' : msg)
      setProcessState('error')
    } finally {
      stopTimer()
    }
  }

  async function sendTurn() {
    if (!text.trim() || loading) return
    const body = text.trim()
    setText('')
    await sendRequest('/api/turn', body)
  }

  async function sendGM() {
    if (!gmText.trim() || loading) return
    const body = gmText.trim()
    setGmText('')
    await sendRequest('/api/gm-channel', body)
  }

  async function moveNPC(name: string, lane: LaneKey) {
    await fetch('/api/npc-position', {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ session_id: SESSION_ID, name, lane }),
    })
    setSelectedNPC(null)
    onSheetsChange()
  }

  function handleChipClick(name: string) {
    if (!editMode) return
    setSelectedNPC((prev) => (prev === name ? null : name))
  }

  function handleLaneClick(lane: LaneKey) {
    if (!editMode || !selectedNPC || lane === 'enemy') return
    moveNPC(selectedNPC, lane)
  }

  const activeQuest = quests.find((q) => q.status === 'active') ?? null
  const availableQuests = quests.filter((q) => q.status === 'available')

  async function acceptQuestFromGuild(id: number) {
    onQuestAccept(id)
    setGuildOpen(false)
    const body = 'ギルドの受付で新しいクエストを受注した。'
    await sendRequest('/api/gm-channel', body)
  }

  return (
    <div style={{ flex: 1, display: 'flex', overflow: 'hidden' }}>

      {/* ── LEFT SIDEBAR ── */}
      <div id="sidebar-left">

        {/* TURN */}
        <div className="sidebar-section">
          <div className="turn-display">
            <div className="turn-number">{currentTurn}</div>
            <div className="turn-label">ROUND</div>
            <div className="turn-phase">探索フェーズ</div>
          </div>
        </div>

        {/* ACTIVE QUEST */}
        <div className="sidebar-section">
          <div className="sidebar-section-title">Active Quest</div>
          {activeQuest ? (
            <div className="active-quest-card">
              <div className="active-quest-header">
                <span className={`quest-rank rank-${activeQuest.rank.toLowerCase()}`}>{activeQuest.rank}</span>
                <span className="active-quest-title">{activeQuest.title}</span>
              </div>
              <div style={{ fontSize: 10, color: 'var(--text-dim)', marginTop: 3 }}>{activeQuest.target}</div>
              <div style={{ fontSize: 10, color: 'var(--amber)', marginTop: 2 }}>⬡ {activeQuest.reward}</div>
            </div>
          ) : (
            <div style={{ fontSize: 11, color: 'var(--text-dim)' }}>クエスト未設定</div>
          )}
        </div>

        {/* PLAYER CHARACTER */}
        <div className="sidebar-section">
          <div className="sidebar-section-title">Player Character</div>
          <div className="pc-card">
            <div className="pc-name-row">
              <span className="pc-name">PC</span>
              <span className="pc-class-badge">—</span>
            </div>
            <div className="bar-row">
              <span className="bar-label">HP</span>
              <div className="bar-track"><div className="bar-fill hp" style={{ width: '100%' }} /></div>
              <span className="bar-value">—</span>
            </div>
            <div className="bar-row">
              <span className="bar-label">MP</span>
              <div className="bar-track"><div className="bar-fill mp" style={{ width: '100%' }} /></div>
              <span className="bar-value">—</span>
            </div>
            <div style={{ marginTop: 5, display: 'flex', gap: 6 }}>
              <div className="npc-status-badge normal">正常</div>
              <div style={{ fontSize: 9, color: 'var(--text-dim)', padding: '1px 5px', border: '1px solid var(--border)', borderRadius: 2 }}>front</div>
            </div>
          </div>
        </div>

        {/* PARTY NPC */}
        <div className="sidebar-section">
          <div className="sidebar-section-title">Party NPC</div>
          {npcSheets.length === 0 && (
            <div style={{ fontSize: 11, color: 'var(--text-dim)' }}>NPCなし</div>
          )}
          {npcSheets.map((s) => (
            <div className="npc-card-mini" key={s.ID}>
              <div className="npc-name-mini">
                <span>{s.Name}</span>
                <span className="npc-class-badge">—</span>
              </div>
              <div className="bar-row">
                <span className="bar-label">HP</span>
                <div className="bar-track"><div className="bar-fill hp" style={{ width: '100%' }} /></div>
                <span className="bar-value">{s.HP}</span>
              </div>
              <div className="bar-row">
                <span className="bar-label">MP</span>
                <div className="bar-track"><div className="bar-fill mp" style={{ width: '100%' }} /></div>
                <span className="bar-value">{s.MP}</span>
              </div>
              <div className="npc-status-badge normal">正常</div>
            </div>
          ))}
        </div>

        {/* ENEMIES */}
        <div className="sidebar-section">
          <div className="sidebar-section-title">Enemies</div>
          <div style={{ fontSize: 11, color: 'var(--text-dim)' }}>遭遇なし</div>
        </div>

      </div>

      {/* ── CENTER: CHAT ── */}
      <div style={{ flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
        <div id="chat-log" style={{ flex: 1, overflowY: 'auto', padding: '20px', display: 'flex', flexDirection: 'column', gap: 14 }}>
          {chatEntries.length === 0 && (
            <div className="msg msg-sys"><div className="msg-body">セッションを開始してください</div></div>
          )}
          {chatEntries.map((e, i) => {
            const isFirstInTurn = i === 0 || chatEntries[i - 1].turn !== e.turn
            return (
              <div key={i} className={`msg msg-${e.role}`}>
                <div className="msg-header">
                  <span className={`speaker ${e.role}`}>{e.speaker}</span>
                  {' · '}Turn {e.turn}
                  {devMode && isFirstInTurn && (
                    <button
                      className="dev-turn-del-btn"
                      onClick={() => deleteTurn(e.turn)}
                      title={`Turn ${e.turn} のログを削除`}
                    >✕</button>
                  )}
                </div>
                <div className="msg-body" style={{ whiteSpace: 'pre-wrap' }}>{e.content}</div>
                {e.role === 'gm' && e.image_url && (
                  <img
                    src={e.image_url}
                    alt="scene"
                    loading="lazy"
                    style={{ width: '100%', marginTop: 8, borderRadius: 4, border: '1px solid var(--border)' }}
                  />
                )}
              </div>
            )
          })}
          {processState === 'processing' && (
            <div className="thinking-indicator">
              <div className="thinking-dots"><span /><span /><span /></div>
              <span>GM が応答を生成中… <span style={{ fontVariantNumeric: 'tabular-nums', color: 'var(--text-dim)' }}>{elapsed}s</span></span>
            </div>
          )}
          {processState === 'error' && (
            <div className="thinking-indicator" style={{ color: 'var(--red)', gap: 8 }}>
              <span style={{ fontSize: 14 }}>!</span>
              <span>エラー: {errorMsg}</span>
              <button
                style={{ fontSize: 10, background: 'none', border: '1px solid var(--red)', color: 'var(--red)', padding: '2px 8px', borderRadius: 3, cursor: 'pointer', marginLeft: 8 }}
                onClick={() => setProcessState('idle')}
              >閉じる</button>
            </div>
          )}
          <div ref={chatEndRef} />
        </div>

        <div className="input-area">
          <div className="input-row">
            <textarea
              className="chat-input"
              placeholder="行動を宣言する..."
              value={text}
              onChange={(e) => setText(e.target.value)}
              onKeyDown={(e) => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); sendTurn() } }}
              rows={2}
            />
            <button className="send-btn" onClick={sendTurn} disabled={loading || !text.trim()}>
              {loading ? `${elapsed}s…` : 'DECLARE'}
            </button>
          </div>
          <div className="input-hint">Enter で送信 / Shift+Enter で改行</div>
        </div>

        <div className="gm-channel">
          <span className="gm-channel-label">⚜ GM直訴</span>
          <textarea
            className="gm-channel-input"
            placeholder="状況への異議やルール質問を入力..."
            value={gmText}
            onChange={(e) => setGmText(e.target.value)}
            onKeyDown={(e) => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); sendGM() } }}
            rows={1}
          />
          <button className="gm-channel-btn" onClick={sendGM} disabled={loading || !gmText.trim()}>
            {loading ? `${elapsed}s…` : 'DECLARE'}
          </button>
          <button
            className={`guild-btn ${guildOpen ? 'active' : ''}`}
            onClick={() => setGuildOpen((v) => !v)}
            title="ギルドの受付"
          >
            🏰
          </button>
        </div>

        {guildOpen && (
          <div className="guild-panel">
            <div className="guild-panel-header">
              <span className="guild-panel-title">🏰 ギルド受付</span>
              {partyAvgLevel !== null && (
                <span style={{ fontSize: 10, color: 'var(--text-dim)', marginLeft: 'auto', marginRight: 10 }}>
                  パーティ平均Lv <span style={{ color: 'var(--text-secondary)' }}>{partyAvgLevel.toFixed(1)}</span>
                </span>
              )}
              <button className="guild-close-btn" onClick={() => setGuildOpen(false)}>✕</button>
            </div>
            <div className="guild-panel-body">
              {availableQuests.length === 0 ? (
                <div className="guild-empty">受注可能なクエストはありません</div>
              ) : (
                availableQuests.map((q) => {
                  const tags = q.tags.split(',').map((t) => t.trim()).filter(Boolean)
                  const elig = questEligibility(q, partyAvgLevel)
                  return (
                    <div key={q.id} className="guild-quest-row">
                      <div className="guild-quest-info">
                        <span className={`quest-rank rank-${q.rank.toLowerCase()}`}>{q.rank}</span>
                        <div>
                          <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                            <span className="guild-quest-title">{q.title}</span>
                            {elig !== 'unknown' && (
                              <span style={{ fontSize: 10, color: ELIGIBILITY_COLOR[elig], whiteSpace: 'nowrap' }}>
                                {elig === 'ok' ? '✓' : elig === 'caution' ? '!' : '✗'} {ELIGIBILITY_LABEL[elig]}
                              </span>
                            )}
                          </div>
                          <div className="guild-quest-meta">
                            {q.level} · ⬡ {q.reward}
                            {tags.map((t) => (
                              <span key={t} className={`quest-tag type-${t}`} style={{ marginLeft: 4 }}>
                                {{ combat: '戦闘', explore: '探索', dungeon: 'ダンジョン', social: '交渉' }[t] ?? t}
                              </span>
                            ))}
                          </div>
                          <div className="guild-quest-desc">{q.description}</div>
                        </div>
                      </div>
                      <button
                        className="guild-accept-btn"
                        disabled={loading}
                        onClick={() => acceptQuestFromGuild(q.id)}
                      >
                        受注する
                      </button>
                    </div>
                  )
                })
              )}
            </div>
          </div>
        )}
      </div>

      {/* ── RIGHT PANEL ── */}
      <div id="panel-right">

        {/* BATTLE MAP — 3 lanes */}
        <div className="rpanel-section">
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 10 }}>
            <div className="rpanel-title" style={{ margin: 0 }}>Battle Map</div>
            <button
              className={`map-edit-btn ${editMode ? 'active' : ''}`}
              onClick={() => { setEditMode((v) => !v); setSelectedNPC(null) }}
              disabled={loading}
              title={editMode ? '編集終了' : '隊列を編集'}
            >
              {editMode ? 'DONE' : '✎ EDIT'}
            </button>
          </div>

          {editMode && (
            <div className="map-edit-hint">
              {selectedNPC
                ? `▶ ${selectedNPC} を移動するレーンをクリック`
                : 'キャラクターをクリックして選択'}
            </div>
          )}

          <div className="battle-lanes">
            {LANE_DEFS.map(({ key, cls, label }) => {
              const npcsInLane = npcSheets.filter((s) => npcLaneKey(s) === key)
              const isDropTarget = editMode && selectedNPC !== null && key !== 'enemy'

              return (
                <div
                  key={key}
                  className={`battle-lane ${cls}${isDropTarget ? ' drop-target' : ''}`}
                  onClick={isDropTarget ? () => handleLaneClick(key) : undefined}
                  style={isDropTarget ? { cursor: 'copy' } : undefined}
                >
                  <div className="battle-lane-label">{label}</div>
                  <div className="battle-lane-chips">
                    {/* PC chip — always in front lane, not movable via NPC API */}
                    {key === 'front' && (
                      <div className="lane-chip pc">◉ PC</div>
                    )}
                    {npcsInLane.map((s) => {
                      const isSelected = selectedNPC === s.Name
                      return (
                        <div
                          key={s.ID}
                          className={`lane-chip npc${editMode ? ' editable' : ''}${isSelected ? ' selected' : ''}`}
                          onClick={editMode ? (e) => { e.stopPropagation(); handleChipClick(s.Name) } : undefined}
                        >
                          {s.Name}
                        </div>
                      )
                    })}
                    {npcsInLane.length === 0 && key !== 'front' && (
                      <span style={{ fontSize: 10, color: isDropTarget ? 'var(--text-secondary)' : 'var(--text-dim)' }}>
                        {isDropTarget ? '← ここに移動' : '—'}
                      </span>
                    )}
                  </div>
                </div>
              )
            })}
          </div>

          <div className="map-legend">
            <div className="legend-item"><div className="legend-dot" style={{ background: 'rgba(46,204,113,0.4)' }} />PC</div>
            <div className="legend-item"><div className="legend-dot" style={{ background: 'rgba(232,148,58,0.4)' }} />NPC</div>
            <div className="legend-item"><div className="legend-dot" style={{ background: 'rgba(192,57,43,0.4)' }} />敵</div>
          </div>
        </div>

        {/* VALIDATION LOG */}
        <div className="rpanel-section">
          <div className="rpanel-title">Validation Log</div>
          {turns.length === 0 && <div style={{ fontSize: 11, color: 'var(--text-dim)' }}>ログなし</div>}
          {turns.slice(-5).reverse().map((t, i) => (
            <div key={i} className="vlog-entry ok">
              <span className="vlog-actor">Turn {t.turn}</span>
              <span className="vlog-ok">✓</span>
              {' '}NPC {t.npc_actions?.length ?? 0}体応答
              {t.formation_changes && t.formation_changes.length > 0 && (
                <div style={{ marginTop: 2, fontSize: 10, color: 'var(--amber)' }}>
                  ⚡ 隊列変更: {t.formation_changes.map((c) => `${c.name}→${c.new_lane}`).join(', ')}
                </div>
              )}
              {t.npc_actions && t.npc_actions.length > 0 && (
                <div style={{ marginTop: 2, fontSize: 10, color: 'var(--text-dim)' }}>
                  {t.npc_actions.map((a) => a.Name).join(' / ')}
                </div>
              )}
            </div>
          ))}
        </div>

        {/* SYSTEM LOG */}
        <div className="rpanel-section">
          <div className="rpanel-title">System</div>
          <div style={{ fontSize: 10, color: 'var(--text-dim)', lineHeight: 1.8, fontFamily: 'monospace', display: 'flex', flexDirection: 'column', gap: 2 }}>
            {[
              { label: 'GM (E4B)', port: 11430 },
              { label: 'Support',  port: 11431 },
              { label: 'NPC-A',    port: 11432 },
              { label: 'NPC-B',    port: 11433 },
              { label: 'NPC-C',    port: 11434 },
              { label: 'Embed',    port: 11435 },
            ].map((s) => (
              <div key={s.port} style={{ display: 'flex', justifyContent: 'space-between' }}>
                <span style={{ color: 'var(--text-secondary)' }}>{s.label}</span>
                <span>:{s.port}</span>
              </div>
            ))}
            <div style={{ display: 'flex', justifyContent: 'space-between', marginTop: 4, paddingTop: 4, borderTop: '1px solid var(--border)' }}>
              <span style={{ color: 'var(--text-secondary)' }}>ComfyUI</span>
              <span style={{ color: comfyAvailable ? 'var(--green)' : 'var(--text-dim)' }}>
                {comfyAvailable === true ? '● online' : comfyAvailable === false ? '○ offline' : '…'}
              </span>
            </div>
          </div>
        </div>

      </div>

    </div>
  )
}
