// screenContext is "workstream statuses": exactly what the user's sidebar shows
// (every workstream row and its tracks, their colours and state text, and the
// NEEDS YOU list) at the current board version, sent ahead of each of his
// messages to Commander or a Manager. cmdrd builds it from the same display
// rows the app renders (GET /api/workstream-statuses, the same body as
// `cmdr workstream-statuses`), so it cannot drift from the sidebar. When the
// board version has not moved since the last one sent to that chat, a one-line
// "unchanged" goes instead. No tabs, panel or screenshots ride along.

import type { TabInfo } from '../../features/inspector/types'
import { useStore } from './store'

/** setPanel is kept for InspectorColumn; the panel no longer rides along with messages. */
export function setPanel(_tabs: TabInfo[], _showing: string) {}

// The board version last sent to each chat, so an unchanged board is one line.
const lastSent = new Map<string, number>()

/** sentVersions is for tests. */
export function resetSentVersions() {
  lastSent.clear()
}

export async function screenContext(role: string): Promise<string> {
  // Operators get no board; Commander and the Managers get the statuses.
  if (role !== 'commander' && !role.startsWith('commander-') && !role.startsWith('manager-')) return ''
  const cfg = useStore.getState().config
  if (!cfg || cfg.demo || !cfg.apiUrl) return ''
  const last = lastSent.get(role) ?? 0
  try {
    const res = await fetch(`${cfg.apiUrl}/api/workstream-statuses?role=${encodeURIComponent(role)}&last=${last}`, {
      headers: { Authorization: `Bearer ${cfg.token}` },
    })
    if (!res.ok) return ''
    const j = (await res.json()) as { version?: number; block?: string }
    if (!j.block) return ''
    if (j.version) lastSent.set(role, j.version)
    return j.block
  } catch {
    return ''
  }
}
