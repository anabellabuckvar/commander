import { create } from 'zustand'

// Which board the app shows: the tree board (the default) or the classic one.
// Each has a control to switch to the other; the choice is kept per browser.

export type BoardMode = 'tree' | 'classic'
const key = 'commander.board'

function load(): BoardMode {
  try {
    return localStorage.getItem(key) === 'classic' ? 'classic' : 'tree'
  } catch {
    return 'tree'
  }
}

export const useBoardMode = create<{ mode: BoardMode }>(() => ({ mode: load() }))

export function setBoardMode(mode: BoardMode): void {
  try {
    localStorage.setItem(key, mode)
  } catch {
    // the choice just won't stick
  }
  useBoardMode.setState({ mode })
}
