// What Eric has selected or open in the tree board's right panel. It rides
// along with each of his messages to the Commander as a <selection> block after
// the workstream statuses, so "this is wrong" resolves to that workstream or
// step. The transcript strips it (features/chat/screen.ts), like the statuses.

import { create } from 'zustand'
import type { Selection } from './types'

/** useTreeSelection: what the board has open, and how to close it (the chip's ×). */
export const useTreeSelection = create<{ sel: Selection | null; clear: (() => void) | null }>(() => ({ sel: null, clear: null }))

export function setTreeSelection(sel: Selection | null): void {
  useTreeSelection.setState({ sel })
}

export function treeSelection(): Selection | null {
  return useTreeSelection.getState().sel
}

const attr = (s: string) => s.replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[c] as string)

/** selectionBlock is the <selection> block for the current selection ('' when nothing is open). */
export function selectionBlock(sel: Selection | null = treeSelection()): string {
  if (!sel) return ''
  const what =
    sel.kind === 'step'
      ? `Eric has the step "${sel.title}" open, in workstream ${sel.label} ${sel.row ?? ''}`.trimEnd()
      : `Eric has workstream ${sel.label} "${sel.title}" open`
  return `<selection kind="${sel.kind}" id="${attr(sel.id)}" key="${attr(sel.key)}" label="${attr(sel.label)}" title="${attr(sel.title)}">${attr(what)}</selection>`
}
