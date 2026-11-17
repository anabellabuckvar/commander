// What Eric has selected or open in the tree board's right panel. It rides
// along with each of his messages to the Commander as a <selection> block after
// the workstream statuses, so "this is wrong" resolves to that workstream or
// step. The transcript strips it (features/chat/screen.ts), like the statuses.

import type { Selection } from './types'

let current: Selection | null = null

export function setTreeSelection(sel: Selection | null): void {
  current = sel
}

export function treeSelection(): Selection | null {
  return current
}

const attr = (s: string) => s.replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[c] as string)

/** selectionBlock is the <selection> block for the current selection ('' when nothing is open). */
export function selectionBlock(sel: Selection | null = current): string {
  if (!sel) return ''
  const what =
    sel.kind === 'step'
      ? `Eric has the step "${sel.title}" open, in workstream ${sel.label} ${sel.row ?? ''}`.trimEnd()
      : `Eric has workstream ${sel.label} "${sel.title}" open`
  return `<selection kind="${sel.kind}" id="${attr(sel.id)}" key="${attr(sel.key)}" label="${attr(sel.label)}" title="${attr(sel.title)}">${attr(what)}</selection>`
}
