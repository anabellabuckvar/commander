// Each message to Commander carries workstream statuses: the sidebar as JSON at
// the board version, from cmdrd. An unchanged board is one line; no tabs or panel.
import { afterEach, expect, it, vi } from 'vitest'
import { resetSentVersions, screenContext } from '../src/renderer/src/screen'
import { useStore } from '../src/renderer/src/store'
import { stripScreen, withScreen } from '../src/features/chat/screen'

afterEach(() => {
  vi.unstubAllGlobals()
  resetSentVersions()
})

it('attaches the workstream statuses cmdrd builds, then one line while the version holds', async () => {
  useStore.setState({ config: { apiUrl: 'http://cmdrd', token: 't' } })
  const block = '<workstream-statuses version="25456">{"version":25456,"workstreams":[{"n":1,"title":"Commander","color":"grey","text":"x","tracks":[{"letter":".1","title":"Bounded installs","color":"red","text":"Confirm?"}]}],"needs_you":[]}</workstream-statuses>'
  const unchanged = '<workstream-statuses version="25456">workstream statuses unchanged since v25456</workstream-statuses>'
  const urls: string[] = []
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    urls.push(url)
    return { ok: true, json: async () => ({ version: 25456, block: url.includes('last=25456') ? unchanged : block }) }
  }))
  const first = await screenContext('commander')
  expect(first).toBe(block)
  expect(first).not.toMatch(/Tabs open|Right panel/)
  expect(urls[0]).toContain('/api/workstream-statuses?role=commander&last=0')
  expect(await screenContext('commander')).toBe(unchanged)
  expect(await screenContext('operator-x')).toBe('')
  const sent = withScreen(first, 'why is 1.2 still open')
  expect(sent.startsWith('<workstream-statuses version="25456">')).toBe(true)
  expect(sent).not.toContain('<user-screen>')
  expect(stripScreen(sent)).toBe('why is 1.2 still open')
})
