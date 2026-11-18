// The Rules and Workflows views, as approved in the prototype (poc/workstream-graph-v2.html).
// Rule numbers are what a refused tree edit cites (cmdr plan apply).

export type Owner = 'you' | 'agent' | 'review' | 'person'
export interface Workflow {
  title: string
  name: string
  what: string
  steps: [Owner, string, string, boolean?, string?, string?][]
}

export const RULES_HTML = `
    <p>Every bead write is checked against these rules, whoever makes it: the Commander, an agent, the system, or Eric in the app. That covers creating, editing, a state change, an owner change, adding or removing a dependency, and superseding. A write that breaks a rule is refused with the rule's number, and nothing changes. An audit checks stored beads against the rules every minute.</p>
    <div><h2>Two kinds of bead</h2>
    <table><tr><th>Kind</th><th>Children</th><th>Owner</th></tr>
    <tr><td><b>Workstream</b></td><td>Only Workstreams or only Standards</td><td>None. Its state comes from its children.</td></tr>
    <tr><td><b>Standard</b></td><td>None</td><td>Exactly one: a Person (You, or a named person or team), an Agent (Implementer or Reviewer), or Automatic (waits for one event and closes itself)</td></tr></table></div>
    <div><h2>Owners and colours</h2>
    <table><tr><td><span class="sw red"></span></td><td>You</td><td>A decision, approval or action only you can take. Always a card on your list.</td></tr>
    <tr><td><span class="sw green"></span></td><td>Implementer agent</td><td>Owns the goal end to end, including waiting for CI or a deploy.</td></tr>
    <tr><td><span class="sw purple"></span></td><td>Review agent</td><td>Checks other steps' results. Never the agent that did the work. Can look across workstreams.</td></tr>
    <tr><td><span class="sw yellow"></span></td><td>Named person or team</td><td>Someone outside the system, e.g. Matt or IT.</td></tr>
    <tr><td><span class=\"sw slate\"></span></td><td>Automatic</td><td>No person or agent: it waits for one event the system sees (a deploy, a PR, CI, a Slack message, a time) and closes itself when it happens. The step after it then starts.</td></tr></table>
    <p>Upcoming: the owner's colour, no border, flat. Active: lifted, with a shadow. Done: grey. Steps side by side in one column run in parallel. When an agent is running on a step right now, the owner line names its model, e.g. "Implementer agent · Opus 5.5" or "Review agent · GPT-5 Codex", and the dot pulses. A step that is asleep until its event has no agent on it: it sits flat with a dashed edge, a still dot, and says what it is asleep until. Your own cards always pulse.</p></div>
    <div><h2>Structure</h2><ol>
      <li>Every bead is a Workstream or a Standard.</li>
      <li>A Workstream has at least one child.</li>
      <li>A Workstream's children are all Workstreams or all Standards, never mixed.</li>
      <li>A Standard has no children.</li>
      <li>"Blocked by" never forms a cycle.</li></ol></div>
    <div><h2>Owners</h2><ol start="6">
      <li>A Workstream has no owner.</li>
      <li>A Standard has exactly one owner: a Person, an Agent, or Automatic.</li>
      <li>A Person owner is You or a named person or team.</li>
      <li>An Agent owner is an Implementer or a Reviewer.</li>
      <li>Nothing is owned by the Commander.</li>
      <li>A Reviewer Standard is blocked by at least one Standard it checks.</li>
      <li>A Reviewer's agent is never the agent that did the work it checks.</li>
      <li>A You Standard that comes straight after an Implementer's work has a Reviewer between them.</li></ol></div>
    <div><h2>Waits and events</h2><ol start="14">
      <li>Only an Automatic Standard waits, for exactly one event, and closes itself when it happens. A Standard that needs an event first comes after an Automatic step.</li>
      <li>The event is one the system can see: a CI result, a deploy of a commit, a PR merged, green or approved, a Slack message, or a date.</li>
      <li>Nothing polls. The event closes the Automatic step, and the step after it starts.</li></ol></div>
    <div><h2>Titles and text</h2><ol start="17">
      <li>A Standard's title is its goal, stated as an outcome.</li>
      <li>A You Standard's title is the decision or approval itself, never "Confirm done".</li>
      <li>The text shown for a step names its real owner, or the event it waits for.</li>
      <li>No text names the Commander as the one doing a step. There is no fallback text.</li></ol></div>
    <div><h2>Lifecycle (done by the system, not the Commander)</h2><ol start="21">
      <li>A watcher checks every Standard that is unblocked, not done, and owned by an Agent. If no live agent is on it, the watcher starts one. That is the only way an agent starts, and the watcher does nothing else.</li>
      <li>An agent's clean final report closes its Standard, with the result stored on it.</li>
      <li>Your answer on your card closes your Standard.</li>
      <li>If an agent dies, the watcher's next check starts a new one. If three in a row die without a report, the Standard turns red with a card for you.</li>
      <li>A Standard has at most one live agent. Something that needs separate work (a problem found, a fix, a follow-up) becomes its own Standard, which can run at the same time, or a nested Workstream. Workstreams nest as deep as the work needs.</li>
      <li>A Workstream is Done when all its children are Done.</li>
      <li>A replaced Standard is Superseded, with a reason, never deleted.</li></ol></div>
    <div><h2>Content (proposed, not agreed yet)</h2><ol start="28">
      <li>A Workstream has acceptance criteria: a short list of what must be true for it to count as done. Each one can be checked.</li>
      <li>Acceptance criteria come first. Whatever else is written on the bead comes after them.</li>
      <li>A Standard's title starts with the action and states one outcome in plain words, in 100 characters or fewer. It carries no build history and no internal labels.</li>
      <li>A Standard says what is being owned: the one result its owner is answerable for.</li></ol>
    <p>How these would be checked, on any write that changes a bead's title, criteria or text. First, fixed checks: length, a count of criteria, required fields, and banned wording such as "Confirm done". Then a fast model (Haiku) grades the content against a rubric: is the outcome clear, are the criteria checkable, is it plain who owns what. A write that fails is refused with the rule's number, the reason and a suggested rewrite, so the agent that wrote it fixes it and tries again. The grade takes about a second.</p>
    <table><tr><th>The grader is given</th><th>It checks (the rubric)</th></tr>
    <tr><td>The bead being written: title, criteria, text, owner</td><td>Would someone outside the work know what this is from the title alone?</td></tr>
    <tr><td>Its parent workstream's title and acceptance criteria</td><td>Is there one outcome, and is it plain when it has been reached?</td></tr>
    <tr><td>The titles of the steps beside it</td><td>Can each acceptance criterion be checked as true or false?</td></tr>
    <tr><td>These rules</td><td>Is it clear what is owned, and by whom?</td></tr>
    <tr><td></td><td>Is it free of build history, internal labels and unexplained ids?</td></tr></table></div>
`

// Workflow templates, from Commander's shape templates (internal/beads/shape_templates.go). S is the subject the workflow is stamped out for.
// Each step: owner, the title under the naming rules, the template's original title (shown on hover), and whether it is optional.
export const WORKFLOWS: Workflow[] = [
  { title: 'Ship a PR, fast (admin merge)', name: 'pr-babysit, fast', what: 'When to use: small or urgent changes where you override branch protection and admin-merge, with no team review.', steps: [
    ['agent', 'Build S and open its PR', 'Build S and open its PR (review skill once)'],
    ['agent', 'Get the S PR passing CI and Devin\'s review', 'S PR: get CI green and answer Devin (fix our failures; a red check from main is reported with its cause and owner)'],
    ['review', 'Review: the S PR is green and matches the scope', 'Review: the S PR (green, Devin-clean, matches the scope)'],
    ['you', 'Admin-merge the S PR, with no team review', 'Greenlight admin-merging the S PR (no team review)'],
    ['agent', 'Confirm S works on a real event after the deploy', 'S live: wait on the deploy (event wait on deploy:<merge sha>) and confirm it works on a real event']] },
  { title: 'Ship a PR, with team review', name: 'pr-babysit, slow', what: 'When to use: anything that needs the owning team\'s sign-off before it merges.', steps: [
    ['agent', 'Build S and open its PR', 'Build S and open its PR (review skill once)'],
    ['agent', 'Get the S PR passing CI and Devin\'s review', 'S PR: get CI green and answer Devin (fix our failures; a red check from main is reported with its cause and owner)'],
    ['review', 'Review: the S PR is green and matches the scope', 'Review: the S PR (green, Devin-clean, matches the scope)'],
    ['person', 'Approve the S PR on GitHub', 'The team approves the S PR on GitHub', false, 'an event wait on pr:approved; nobody polls', 'Named person or team'],
    ['you', 'Merge the S PR', 'Greenlight merging the S PR'],
    ['agent', 'Confirm S works on a real event after the deploy', 'S live: wait on the deploy (event wait on deploy:<merge sha>) and confirm it works on a real event']] },
  { title: 'Propose, approve, then build', name: 'propose-then-approve', what: 'For work that needs your say before anything is built: propose, approve, then build and ship.', steps: [
    ['agent', 'Propose S: what, why and how it will be checked', 'Propose S (what, why, and how it will be checked)'],
    ['review', 'Review: the S proposal is complete and matches the scope', 'Review: the S proposal (complete, matches the scope)'],
    ['you', 'Approve S', 'Approve S'],
    ['agent', 'Build S and open its PR', 'Build S and open its PR'],
    ['agent', 'Get the S PR passing CI and Devin\'s review', 'S PR: get CI green and answer Devin'],
    ['review', 'Review: the S PR is green and matches the approved proposal', 'Review: the S PR (green, Devin-clean, matches the approved proposal)'],
    ['you', 'Merge the S PR', 'Greenlight: merge the S PR'],
    ['agent', 'Confirm S works after the deploy', 'S live: wait on the deploy and confirm it works']] },
  { title: 'Investigate and decide a fix', name: 'investigation', what: 'Finds the cause of a problem and brings you a fix to decide on, without changing anything.', steps: [
    ['agent', 'Find the cause of S, with evidence', 'Investigate S: reproduce and find the cause, with evidence'],
    ['review', 'Review: the cause of S is proven, with evidence', 'Review: the S findings (cause proven, evidence attached)'],
    ['you', 'Decide the fix for S', 'Decide the S fix: proposed change, size and risk']] },
  { title: 'Release an app to prod', name: 'app-release', what: 'Ships a set of ready changes to an app in one release.', steps: [
    ['agent', 'Assemble release S on the dev box', 'Release S: assemble the release on the dev box'],
    ['agent', 'Fix or drop every failing change', 'fix or drop every failing change (no weakened tests; dropped changes move to the next release with the reason)'],
    ['agent', 'Run one full validate on the final tree and write the receipt', 'one full validate on the final tree, receipt written (dev box quiet)'],
    ['review', 'Review: release S is validated, with what was dropped and why', 'Review: release S (validate green, receipt written, what was dropped and why)', true],
    ['you', 'OK shipping release S', 'Greenlight shipping release S', true],
    ['agent', 'Deploy release S to prod', 'deploy to prod reusing the receipt (zero downtime, memory gate)'],
    ['agent', 'Watch prod for 15 minutes, then post DEPLOYED', '15-minute prod watch, then post DEPLOYED'],
    ['agent', 'Land release S on the app\'s mainline', 'land the release on the app\'s mainline']] },
];
