// TreeBoard: the tree board as the app's main view. The board itself is drawn
// imperatively (board.ts) from cmdrd's tree (GET /api/tree), refetched when the
// snapshot version moves. Below the right panel sits the Commander's chat: what
// Eric has open rides along with each message (selection.ts).

import { useEffect, useRef, useState } from 'react'
import { ChatSlot } from '../../renderer/src/components/ChatSlot'
import { host } from '../../renderer/src/host'
import { useStore } from '../../renderer/src/store'
import { linearUrl } from '../linear/Ticket'
import { useIntegrations } from '../linear/settings'
import { mountBoard, type Board } from './board'
import { setBoardMode } from './mode'
import { setTreeSelection } from './selection'
import type { Selection, Tree } from './types'
import './treeboard.css'

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
    })
    board.current = b
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

  const dock = chat || !!sel
  useEffect(() => board.current?.setChatOpen(dock), [dock])

  return (
    <div className="tb-root">
      <div className="tb" ref={ref} />
      {error && (
        <p className="tb-error" role="alert">
          Board unavailable: {error}
        </p>
      )}
      <aside className={`tb-chatdock${sel ? '' : ' full'}`} hidden={!dock} aria-label="Message the Commander">
        <div className="tb-chathead">
          <span>{sel ? `Commander · about ${sel.kind === 'step' ? `this step in ${sel.label}` : `workstream ${sel.label}`}` : 'Commander'}</span>
          {!sel && (
            <button type="button" onClick={() => setChat(false)}>
              Close
            </button>
          )}
        </div>
        <section className="col col-middle" style={{ ['--chat-col' as string]: '380px' }}>
          <div className="chat-slot">{dock && <ChatSlot />}</div>
        </section>
      </aside>
    </div>
  )
}
