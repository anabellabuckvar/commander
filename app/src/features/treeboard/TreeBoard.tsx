// TreeBoard: the tree board as the app's main view. The board itself is drawn
// imperatively (board.ts) from cmdrd's tree (GET /api/tree), refetched when the
// snapshot version moves. Below the right panel sits the Commander's chat: what
// Eric has open rides along with each message (selection.ts).

import { useEffect, useMemo, useRef, useState, type PointerEvent as ReactPointerEvent } from 'react'
import { createPortal } from 'react-dom'
import { MyProgress, progressWorkstreams } from '../linear/MyProgress'
import { board as boardVM } from '../../renderer/src/viewmodel'
import { ChatSlot } from '../../renderer/src/components/ChatSlot'
import { host } from '../../renderer/src/host'
import { useStore } from '../../renderer/src/store'
import { linearUrl } from '../linear/Ticket'
import { useIntegrations } from '../linear/settings'
import { mountBoard, type Board } from './board'
import { setBoardMode } from './mode'
import { setTreeSelection } from './selection'
import type { Selection, Tree, TreeRow } from './types'
import './treeboard.css'

const NARROW = 900
const CHAT_W_KEY = 'commander.tree.chatWidth'
const clampChat = (w: number) => Math.min(640, Math.max(340, Math.round(w)))
function loadChatWidth(): number {
  try {
    const v = Number(localStorage.getItem(CHAT_W_KEY))
    if (v) return clampChat(v)
  } catch {
    // no storage: the default
  }
  return 400
}

export async function fetchTree(apiUrl: string, token: string): Promise<Tree> {
  const res = await fetch(`${apiUrl}/api/tree`, { headers: { Authorization: `Bearer ${token}` } })
  if (!res.ok) throw new Error(`tree: HTTP ${res.status}`)
  return (await res.json()) as Tree
}

export function TreeBoard() {
  const ref = useRef<HTMLDivElement>(null)
  const board = useRef<Board | null>(null)
  const [sel, setSel] = useState<Selection | null>(null)
  const [chat, setChat] = useState(false)
  const [error, setError] = useState('')
  const cfg = useStore((s) => s.config)
  const version = useStore((s) => s.snapshot?.version ?? 0)
  const snapshot = useStore((s) => s.snapshot)
  // The Linear progress card, as the classic sidebar shows it: the whole cycle in the header,
  // and a top-level workstream's own tickets in its panel.
  const progress = useMemo(() => progressWorkstreams(snapshot?.workstreams ?? [], snapshot?.display ? boardVM(snapshot).workstreams : []), [snapshot])
  const [linearSlot, setLinearSlot] = useState<HTMLElement | null>(null)
  const [wsSlot, setWsSlot] = useState<{ el: HTMLElement; row: TreeRow } | null>(null)
  const openProgress = () => {
    setBoardMode('classic')
    useStore.getState().select({ kind: 'progress' })
  }

  useEffect(() => {
    // The chat below the panel is the Commander's.
    if (useStore.getState().selected.kind !== 'commander') useStore.getState().goHome()
    const b = mountBoard(ref.current as HTMLDivElement, {
      onSelect: (s) => {
        setTreeSelection(s)
        setSel(s)
      },
      onClassic: () => setBoardMode('classic'),
      onChat: () => setChat((c) => !c),
      linearUrl: (id) => {
        const ws = useIntegrations.getState().linearWorkspace
        return ws ? linearUrl(id, ws) : null
      },
      openUrl: (url) => {
        if (host?.webview) host.webview.openExternal(url)
        else window.open(url, '_blank', 'noopener,noreferrer')
      },
      onPanel: (el, row) => setWsSlot(el && row && !row.parent ? { el, row } : null),
    })
    board.current = b
    setLinearSlot(b.linearSlot)
    return () => {
      setTreeSelection(null)
      b.destroy()
      board.current = null
    }
  }, [])

  useEffect(() => {
    if (!cfg || cfg.demo || !cfg.apiUrl) return
    let live = true
    fetchTree(cfg.apiUrl, cfg.token).then(
      (t) => {
        if (!live) return
        setError('')
        board.current?.update(t)
      },
      (e) => live && setError(e instanceof Error ? e.message : String(e)),
    )
    return () => {
      live = false
    }
  }, [cfg, version])

  // Two panes: the board on the left, the Commander chat on the right (resizable).
  // In a narrow window or side tab the chat is a toggle that opens over the board.
  const [winW, setWinW] = useState(() => window.innerWidth)
  useEffect(() => {
    const onResize = () => setWinW(window.innerWidth)
    window.addEventListener('resize', onResize)
    return () => window.removeEventListener('resize', onResize)
  }, [])
  const narrow = winW < NARROW
  const [chatW, setChatW] = useState(loadChatWidth)
  const startDrag = (e: ReactPointerEvent) => {
    e.preventDefault()
    const x0 = e.clientX,
      w0 = chatW
    const move = (m: PointerEvent) => setChatW(clampChat(w0 - (m.clientX - x0)))
    const up = () => {
      window.removeEventListener('pointermove', move)
      window.removeEventListener('pointerup', up)
      setChatW((w) => {
        try {
          localStorage.setItem(CHAT_W_KEY, String(w))
        } catch {
          // the width just won't stick
        }
        return w
      })
    }
    window.addEventListener('pointermove', move)
    window.addEventListener('pointerup', up)
  }
  const chatShown = !narrow || chat

  return (
    <div className={`tb-root${narrow ? ' narrow' : ''}`} style={{ ['--chat-w' as string]: `${chatW}px` }}>
      <div className="tb-pane">
        <div className="tb" ref={ref} />
      </div>
      {linearSlot && createPortal(<MyProgress workstreams={progress} onOpen={openProgress} cycles={false} />, linearSlot)}
      {wsSlot && createPortal(<MyProgress workstreams={progress} only={wsSlot.row.key} onOpen={openProgress} cycles={false} />, wsSlot.el)}
      {error && (
        <p className="tb-error" role="alert">
          Board unavailable: {error}
        </p>
      )}
      {!narrow && <div className="tb-split" role="separator" aria-orientation="vertical" aria-label="Resize the chat" onPointerDown={startDrag} />}
      <aside className="tb-chatcol" hidden={!chatShown} aria-label="Message the Commander">
        <div className="tb-chathead">
          {sel ? (
            <span className="tb-selchip" title="Each message carries what you have open">
              <span className="tb-selchip-t">
                {sel.label} {sel.title}
              </span>
              <button type="button" aria-label="Clear the selection" onClick={() => board.current?.clear()}>
                ×
              </button>
            </span>
          ) : (
            <span className="tb-chatname">Commander</span>
          )}
          {narrow && (
            <button type="button" className="tb-chatclose" onClick={() => setChat(false)}>
              Close
            </button>
          )}
        </div>
        <section className="col col-middle" style={{ ['--chat-col' as string]: `${Math.max(280, chatW - 40)}px` }}>
          <div className="chat-slot">{chatShown && <ChatSlot />}</div>
        </section>
      </aside>
    </div>
  )
}
