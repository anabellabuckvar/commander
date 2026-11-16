// @vitest-environment jsdom
import '@testing-library/jest-dom/vitest'
import { afterEach, beforeAll, describe, expect, it } from 'vitest'
import { act, cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ChatPane } from '../ChatPane'
import { Header } from '../Header'
import { ReplayTransport } from '../transport'
import { EngineMapper } from '../engine'
import { compactingNow, deriveStatus, initialState, reduceAll, type ChatEvent, type SystemItem } from '../events'

const RECT = { width: 900, height: 4000 }

beforeAll(() => {
  const orig = Object.getOwnPropertyDescriptor(Element.prototype, 'clientHeight')!
  Object.defineProperty(HTMLElement.prototype, 'clientHeight', {
    configurable: true,
    get(this: HTMLElement) {
      return this.classList.contains('cc-list') ? RECT.height : orig.get!.call(this)
    },
  })
})
afterEach(() => cleanup())

let seq = 0
const ev = (e: Record<string, unknown>) => ({ seq: ++seq, ts: 1_000 + seq, ...e }) as ChatEvent
const full = (tokens: number) => ev({ kind: 'turn_result', subtype: 'success', isError: false, contextWindow: 1_000_000, contextTokens: tokens })
const dividers = (s: ReturnType<typeof reduceAll>) => s.order.map((id) => s.items[id]).filter((it): it is SystemItem => it.type === 'system' && it.variant === 'compact')

describe('compaction status', () => {
  it('reads the CLI compacting status as compacting its memory', () => {
    const s = reduceAll(initialState(), [ev({ kind: 'status', state: 'requesting' }), ev({ kind: 'status', state: 'compacting' })])
    expect(compactingNow(s)).toBe(true)
    expect(deriveStatus(s)).toMatchObject({ state: 'compacting', label: 'Compacting its memory' })
  })

  it('infers a compaction from a silent turn near a full context, and drops it at the first output', () => {
    const near = reduceAll(initialState(), [full(950_000), ev({ kind: 'status', state: 'requesting' })])
    expect(compactingNow(near)).toBe(true)
    const roomy = reduceAll(initialState(), [full(400_000), ev({ kind: 'status', state: 'requesting' })])
    expect(compactingNow(roomy)).toBe(false)
    const talking = reduceAll(near, [ev({ kind: 'text_delta', message: 'm', index: 0, text: 'Hi' })])
    expect(compactingNow(talking)).toBe(false)
  })

  it('draws the divider with how full it was and is now, and names an early compaction', () => {
    const s = reduceAll(initialState(), [full(970_000), ev({ kind: 'compact', trigger: 'auto', preTokens: 969_555, postTokens: 13_311 })])
    expect(dividers(s)[0]).toMatchObject({ text: 'Memory compacted (it was full)', detail: 'was 97% full, now 1% · 970k → 13k tokens' })
    expect(s.contextTokens).toBe(13_311)
    const early = reduceAll(initialState(), [
      full(760_000),
      ev({ kind: 'compact', trigger: 'manual', preTokens: 760_000, postTokens: 20_000 }),
      ev({ kind: 'notice', level: 'info', text: 'Compacted its memory early (was 76% full, now 2%)', key: 'early_compact' }),
    ])
    expect(dividers(early).map((d) => d.text)).toEqual(['Memory compacted early'])
    expect(early.order.map((id) => early.items[id]).filter((it) => it.type === 'system' && it.variant === 'notice')).toEqual([])
  })
})

describe('cmdrd frames', () => {
  it('takes the context from the last call of a turn, and compacting from the agent state', () => {
    const m = new EngineMapper()
    const b = { role: 'commander', session_id: 's', source: 'live' }
    const evs = m.map([
      {
        ...b,
        seq: 1,
        kind: 'turn_result',
        result: {
          subtype: 'success',
          num_turns: 7,
          usage: { input_tokens: 14, cache_read_input_tokens: 815_063, iterations: [{ input_tokens: 2, cache_read_input_tokens: 119_829, cache_creation_input_tokens: 315 }] },
          model_usage: { 'claude-opus-5-5': { contextWindow: 1_000_000 } },
        },
      },
      { ...b, seq: 2, kind: 'agent_state', state: { state: 'working', detail: 'Compacting its memory' } },
    ])
    const s = reduceAll(initialState(), evs)
    expect(s.contextTokens).toBe(120_146)
    expect(s.contextWindow).toBe(1_000_000)
    expect(s.phase).toBe('compacting')
  })
})

describe('the chat while it compacts', () => {
  it('says so, and Stop asks once before throwing the compaction away', async () => {
    const user = userEvent.setup()
    const transport = new ReplayTransport([], { echo: true })
    render(<ChatPane role="commander" transport={transport} initialRect={RECT} />)
    act(() => transport.emit([{ kind: 'status', state: 'requesting' }, { kind: 'status', state: 'compacting' }] as never))
    expect(await screen.findByText('Compacting its memory — about a minute.')).toBeInTheDocument()
    expect(screen.getByText(/Stopping now throws the work away/)).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Stop' }))
    expect(transport.calls.some((c) => c.verb === 'interrupt')).toBe(false)
    expect(screen.getByRole('group', { name: 'Stop the compaction?' })).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Keep waiting' }))
    expect(screen.queryByRole('group', { name: 'Stop the compaction?' })).toBeNull()
    await user.click(screen.getByRole('button', { name: 'Stop' }))
    await user.click(screen.getByRole('button', { name: 'Stop anyway' }))
    await waitFor(() => expect(transport.calls.at(-1)).toEqual({ verb: 'interrupt', role: 'commander' }))
  })

  it('shows how full its memory is in the header', () => {
    const s = reduceAll(initialState(), [full(420_000)])
    render(<Header state={s} />)
    expect(screen.getByText('Memory 42%')).toBeInTheDocument()
  })
})
