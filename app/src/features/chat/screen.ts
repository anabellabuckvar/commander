// What the user's app shows as he sends a message rides along with it, so the
// agent knows without asking. The block goes to the agent only: the transcript
// strips it, so the user sees what he typed.

const OPEN = '<user-screen>'
const CLOSE = '</user-screen>'
const BLOCK = /^(<user-screen>[\s\S]*?<\/user-screen>|<workstream-statuses\b[\s\S]*?<\/workstream-statuses>)\s*/

export function withScreen(screen: string, text: string): string {
  if (!screen) return text
  // Workstream statuses are their own tagged block; anything else rides in <user-screen>.
  if (screen.startsWith('<workstream-statuses')) return `${screen}\n\n${text}`
  return `${OPEN}\n${screen}\n${CLOSE}\n\n${text}`
}

export function stripScreen(text: string): string {
  return text.replace(BLOCK, '')
}
