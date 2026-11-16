import { Layers } from 'lucide-react'
import { duration, useNow } from './motion'

// The agent is compacting its memory: a quiet row where the thinking row would
// be. Stopping throws the compaction away, so Stop asks here first.
export function CompactingRow({ since, confirming, onStop, onKeep }: { since?: number; confirming: boolean; onStop: () => void; onKeep: () => void }) {
  const now = useNow(true)
  return (
    <div className="ct-compacting-wrap">
      <div className="ct-thinking ct-compacting" role="status" aria-label="Compacting its memory">
        <Layers size={14} className="ct-retry-icon" />
        <span className="ct-shimmer">Compacting its memory — about a minute.</span>
        <span className="ct-elapsed">
          {since ? ` · ${duration(Math.max(0, now - since))}` : ''} · Stopping now throws the work away.
        </span>
      </div>
      {confirming && (
        <div className="ct-compact-confirm" role="group" aria-label="Stop the compaction?">
          <span>Stop anyway? The compaction is lost and starts over on its next turn.</span>
          <button type="button" className="ct-compact-stop" onClick={onStop}>
            Stop anyway
          </button>
          <button type="button" className="ct-compact-keep" onClick={onKeep}>
            Keep waiting
          </button>
        </div>
      )}
    </div>
  )
}
