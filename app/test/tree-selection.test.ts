// What the tree board has open rides along with each message to the Commander,
// after the workstream statuses, and never shows in the user's transcript.
import { afterEach, expect, it, vi } from 'vitest'
import { resetSentVersions, screenContext } from '../src/renderer/src/screen'
import { useStore } from '../src/renderer/src/store'
import { stripScreen, withScreen } from '../src/features/chat/screen'
import { selectionBlock, setTreeSelection } from '../src/features/treeboard/selection'

afterEach(() => {
  vi.unstubAllGlobals()
  resetSentVersions()
  setTreeSelection(null)
})

const block = '<workstream-statuses version="9">\nBoard v9: 0 of 1 workstreams done · 1 need you\n</workstream-statuses>'

it('attaches the open step to a message to the Commander and strips it from the transcript', async () => {
  useStore.setState({ config: { apiUrl: 'http://cmdrd', token: 't' } })
  vi.stubGlobal('fetch', vi.fn(async () => ({ ok: true, json: async () => ({ version: 9, block }) })))
  setTreeSelection({ kind: 'step', id: 'w1.1.2', key: 'p:w1.1', label: '1.1', title: 'Merge the "phase 1" PR', row: 'Phase 1' })
  const screen = await screenContext('commander')
  expect(screen).toBe(`${block}\n<selection kind="step" id="w1.1.2" key="p:w1.1" label="1.1" title="Merge the &quot;phase 1&quot; PR">Eric has the step &quot;Merge the &quot;phase 1&quot; PR&quot; open, in workstream 1.1 Phase 1</selection>`)
  // Managers and Operators never get it.
  expect(await screenContext('manager-x')).not.toContain('<selection')
  expect(await screenContext('operator-x')).toBe('')
  const sent = withScreen(screen, 'this is wrong')
  expect(sent.startsWith('<workstream-statuses')).toBe(true)
  expect(stripScreen(sent)).toBe('this is wrong')
})

it('sends the selection on its own when the statuses are unavailable', async () => {
  useStore.setState({ config: { apiUrl: 'http://cmdrd', token: 't' } })
  vi.stubGlobal('fetch', vi.fn(async () => ({ ok: false })))
  setTreeSelection({ kind: 'workstream', id: 'w1', key: 'p:w1', label: '1', title: 'Ship <it>' })
  const screen = await screenContext('commander')
  expect(screen).toBe(selectionBlock())
  expect(screen).toContain('title="Ship &lt;it&gt;"')
  const sent = withScreen(screen, 'why is this still open?')
  expect(sent).not.toContain('<user-screen>')
  expect(stripScreen(sent)).toBe('why is this still open?')
})

it('attaches nothing when nothing is open', async () => {
  useStore.setState({ config: { apiUrl: 'http://cmdrd', token: 't' } })
  vi.stubGlobal('fetch', vi.fn(async () => ({ ok: true, json: async () => ({ version: 9, block }) })))
  expect(await screenContext('commander')).toBe(block)
})
