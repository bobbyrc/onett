# Web UI design (v0)

The web UI is a control room: see what the factory is doing, answer what it needs from you,
and talk to the orchestrator. It is a client of `docs/api.md` and nothing else; native and
mobile apps should follow the same information architecture.

## Stack

Vite + React + TypeScript, react-router, @tanstack/react-query, marked + dompurify for
Markdown. No component library; `src/styles.css` holds the design tokens and shared components, and a page with rules of its own imports a small CSS file next to it (`src/pages/TaskDetail.css`). `npm run build` writes to
`../internal/api/webdist`, which the Go binary embeds. `npm run dev` proxies `/api` to
`http://127.0.0.1:7777`.

## Tokens

The visual world is an **andon board**: the lamp board over a factory line that is green while
work flows, yellow when a station calls for help, and red when the line stopped itself. A
charcoal board carries the navigation and the Overview's lamp board; the page ground is a pale
machine-green enamel. Light is the default; dark follows `prefers-color-scheme: dark`. Structure
is drawn with 1px rules and square corners; content has no shadows.

| Token | Light | Dark | Use |
|---|---|---|---|
| `--bg` | `#e3eae4` | `#111614` | page ground |
| `--panel` | `#f5f8f5` | `#181e1b` | sheets: items, tables, meta grids |
| `--panel-2` | `#eaf0eb` | `#202723` | hover, code |
| `--input` | `#ffffff` | `#0d1210` | form fields |
| `--field` | `#6f7d76` | `#64736b` | form field outlines (3:1 against field and ground) |
| `--border` / `--rule` | `#b9c6bd` / `#cdd7d0` | `#3a4741` / `#2a332f` | outer rules / inner rules |
| `--text` / `--muted` | `#18201c` / `#4b5751` | `#e4ebe6` / `#a1aea7` | body / secondary text |
| `--board` | `#1f2422` | `#0a0d0c` | nav rail, phone top bar, lamp board |
| `--lamp-run` | `#2fa35a` | same | running, in review, merged, deploying |
| `--lamp-call` | `#f2c230` | same | waits on you: approvals; medium risk; the lit segment of the brand mark |
| `--lamp-stop` | `#d7342a` | same | stopped: blocked, failed, denied; high risk |
| `--lamp-done` | `#7f9d8c` | `#6f8e7d` | done, healthy |
| `--lamp-off` | `#3a423e` | `#2c3430` | idle, queued, cancelled, paused |

Status colours as text (`--ok`, `--warn`, `--danger`, ...) are darker or lighter versions of the
lamps chosen for 4.5:1 contrast on the ground. The primary button is ink-filled (`--accent` is
the text colour), not a lamp colour, so lamp colours keep their meaning.

Type: Barlow (text, 15px, line height 1.5) and Barlow Condensed (page titles 30px, section
labels and table headers in 13-15px caps, board window names), both self-hosted from
`src/assets/fonts/` (SIL Open Font License) because the CSP allows same-origin fonts only.
Monospace (`ui-monospace, SFMono-Regular, Menlo, monospace`, 13px) only for logs, IDs, SHAs and
config. Radius 0 on sheets, 2px on controls and pills. Numbers in tables and counts use tabular
figures. Motion: lamps switch, never fade: state changes are a two-step at 90ms, and running work
blinks on and off rather than pulsing; both stop under `prefers-reduced-motion`.

## Shared components

- **StatusPill**: a square lamp plus the status in words, on a 1px-ruled chip. Lamp by status:
  queued and cancelled off; running (blinking), review, merged and deploying run green;
  awaiting_approval call yellow; done done-green; blocked and failed stop red. Runs:
  starting/running run (blinking), succeeded done, failed/stalled/timed_out/denied stop,
  cancelled off, interrupted (onett restarted; the task resumes) call yellow. Deployments: pending/deploying/verifying run, healthy done, failed/rolled_back
  stop. The tone names in `src/lib/status.ts` are unchanged; the stylesheet maps them to lamps.
- **RiskBadge**: condensed caps on one fixed ramp, the same everywhere: low outlined, medium on
  call yellow, high on stop red, unrated as quiet muted text.
- **RelTime**: "3m ago", full timestamp in `title`, with the month as a word ("Aug 7, 2026,
  3:04:05 PM"). A time over 30 days old shows as a date
  with the month as a word ("Aug 7, 2026" or "7 Aug 2026", by the browser's locale), so day and
  month cannot be confused.
- **Agent names**: wherever the UI shows an agent or role as text it uses `agentLabel` (`web/src/lib/agentLabel.ts`):
  the agent's title, else its key in Title Case ("implementer-claude" → "Implementer Claude", "sre" → "SRE").
  URLs, form values, config, the API and the CLI keep the raw key; each Agents page card shows it as a small mono line.
- **IdChip**: last 8 chars of an ID in monospace (IDs are UUIDv7, whose leading chars are a
  timestamp shared by IDs created close together), copies the full ID on click. The chip is 17px
  high; like the text links (`.link-button`), it has an invisible pad above and below, so the
  area that takes a tap is at least 24px high.
- **Modal**: a dialog over the page. Opening it moves keyboard focus to the first control in its
  body (a field with `autoFocus` keeps it), Tab and Shift+Tab stay inside it and wrap at the ends,
  and closing it (Esc, the "×", or a click on the backdrop) puts focus back on the control that
  opened it.
- **Field**: a label wrapped around its control, with an optional hint under it. The hint is
  outside the label and linked with `aria-describedby`, so a screen reader reads "Name" as the
  field's name and the hint as its description.
- **Empty**: muted centred line in a dashed rule saying what would appear here and why it is
  empty.
- **Markdown**: marked + dompurify. A fenced code block gets a header bar with its language (or "text"), a Copy button (copies the raw code; "Copied" shows for 1.5 s) and a Wrap toggle (`aria-pressed`; long lines scroll sideways by default). Colours come from highlight.js, loaded on first use as a separate chunk (`src/lib/hljsCore.ts`: go, js/ts, json, bash, yaml, diff, python, sql, css, html/xml, markdown); other languages stay plain. The highlighter runs on the block's text and its output is sanitised again, allowing only `<span class="hljs-*">`. Token colours are `--tok-*` variables in `CodeBlock.css`, for light and dark.
- **ApprovalCard**: a call item (see Overview). Under the title: status, kind, project, task,
  when requested. Then the gate: the task's risk badge and risk reasons, and the commit the
  approval covers (short SHA, PR link, "a new push withdraws this approval"). Then the reason
  (clamped to 3 lines), the decision (optional note, "Approve merge" for merge approvals or
  "Approve", and Reject), and only then "What changed" and the reviewer's summary, each clamped
  to 3 lines with "show more", so the decision stays in the first screen on a phone.
- Buttons: primary (ink fill), secondary (ink outline), danger (stop-red outline), danger-fill,
  ghost. Every destructive action asks for confirmation inline (button turns into "Confirm?"
  for 3s).

## Shell

- Tab title: every page names its browser tab "<page> · onett" (`src/lib/pageTitle.ts`). The route
  names it first ("Tasks", or "Task 9W4Y6ABC" for a detail page that is still loading); a page
  heading (`PageHeader`) then names it after its own text, so a task or goal page carries the
  task's or goal's title. A run page keeps "Run <short ID>".
- Skip link: the first Tab stop on every page is "Skip to content", hidden until it has focus. It
  moves focus to the main area, past the navigation.
- Left rail 216px in `--board`: wordmark "onett" in Barlow Condensed after the brand mark, a
  loop in the text colour with one call-yellow lit segment (work going round the line); nav items: Overview, Projects, Chat, Goals, Tasks, Approvals, Runs, Deploys,
  Signals, Friction, Loops, Triggers, Agents, Config, and Users (owners only). Approvals carries a
  yellow count badge when approvals wait; Tasks a red one when tasks are blocked (each count
  has screen-reader text). The active item is lit (`--board-2` background, white text).
- Project lamps: between the nav and the footer, one 26px row per project (its lamp square,
  name, and its lamp label in words, right-aligned: "1 needs you" or "1 blocked" in call or stop
  colour, "2 running", "idle", "paused" or "3 queued" in muted text), most urgent first,
  each linking to that project's tasks. More than eight projects show the eight most urgent and
  an "All projects" row. Hidden on Overview, which has the full board, and under 760px.
- Rail footer: the **account panel**, then the **pause switch**. The account panel shows the
  signed-in username, the role in muted lower case in parentheses ("bcraig (owner)"; a screen
  reader hears "role: owner"), a "Sign out" link and a "Change password"
  link. Change password opens a dialog (current password, new password of at least 12
  characters, confirm); on success it says "Password changed." for three seconds. The dialog
  is drawn over the page, not inside the rail. The pause switch is the line's cord: a dark
  window with the line's state on the left (green lamp "Running", or red lamp "Paused" with
  the reason in words, such as "Paused for self-update to 69f1660 since 22:36, waiting for 1 run
  (task 01a1a9c6…).", or "Paused by <user> since 22:36." for your own pause, and a red rule) and the action on the right (an outlined "Pause", or a
  filled "Resume"; viewers see only the state). Below it the live
  indicator: green square "live" when the event stream is connected, yellow "reconnecting…".
- Main area: content up to 1360px wide, 28px top and 32px side padding.
- The rail is as tall as the window. When its items, the project lamps and its footer do not fit
  (a short window with many projects), the rail scrolls; nothing is cut off.
- Under 760px wide the rail becomes a charcoal top bar (wordmark, "1 approval" and "1 blocked"
  badges in words, menu button) with the nav in a full-height drawer; tables become stacked
  sheets (title row, then `label: value` lines).


## Auth

Until someone signs in, the page shows one sheet on the charcoal board. If onett has no
accounts yet, the sheet is **Create the owner account**: a username, a password typed twice
(at least 12 characters) and "Create account". That first account is the owner. The form shows
only when the page was opened from the link `onett open` gives, which carries a setup code after
`#`; opened any other way, the sheet says to run `onett open` on the machine that runs onett.
Once accounts
exist, the sheet is **Sign in**: a username, a password and "Sign in". A wrong username or
password says so, and too many wrong attempts are refused for a few minutes.

Signing in sets a session cookie that the server marks HttpOnly, so page scripts cannot read
it. The browser sends it with every request and with the live event stream. Nothing is kept
in localStorage; the UI only remembers who is signed in (`src/auth.ts`). When a request
returns 401 during a session, the session has ended: the sheet comes back with "Your session
ended. Sign in again." A wrong password at sign-in never shows that line. Sign-out, and an
ended session, clear everything cached from the account so the next person does not see it.

Roles decide what the page draws. The server checks every request as well.

- **viewer** reads everything. No button that changes anything is drawn. In a chat, the
  composer is replaced by "Viewers can read conversations but not send messages."
- **operator** also approves and rejects, retries and cancels tasks and runs, pauses and
  resumes the factory or a project, creates tasks and chat conversations, sends chat messages
  and runs loops now.
- **owner** also manages accounts on the Users page.

A control a role may not use is left out, not shown greyed out.

## Install on a phone

The UI is an installable web app (it has a web app manifest, `/manifest.webmanifest`, and an
icon of the brand mark on charcoal, `/icon.svg`), so it can sit on a phone home screen
and open without browser chrome. Open onett's URL on the phone first. From another network
that needs remote access, such as Tailscale, and the name you use must be in
`server.allowed_hosts`, or the server answers 421. Then:

- iOS (Safari): Share → Add to Home Screen.
- Android (Chrome): menu → Install app (or Add to Home screen).

## Live data

Each tab keeps one WebSocket open, to `/api/v1/ws?after=<last id>` (`wss:` on an https page). A
WebSocket is a connection that stays open in both directions, so the server can push events and
the page can ask for subscriptions over it. The browser sends the session cookie with the
connection, and it starts only once someone is signed in. It reconnects with backoff (1s → 5s),
and at once when the tab is shown again or the network returns. The `onett` CLI uses SSE
(`/api/v1/events`) instead.

Events invalidate react-query keys: `task.*` → tasks, task detail, stats; `run.updated` → runs, run
detail; `approval.*` → approvals, stats; `deploy.updated` → deployments; `signal.new` → signals;
`friction.reported`/`issue.filed` → friction; `chat.message` → that conversation, every task agent's
conversation and the agents list; `conversation.updated` (a close or reopen) → that task agent's
conversation and the agents list. `trigger.updated` and `trigger.deleted` → the triggers list and
that trigger's page; `trigger.fired` → those and both firing lists (the log on the Triggers page and
the trigger's own history). `run.updated` also refreshes the conversations, the agents list and that
run's cached log.

`run.log` goes only to tabs subscribed to that run, and `chat.delta` only to tabs subscribed to
that conversation; every other event goes to every tab. A page subscribes with `watch()` in
`web/src/events.ts`. The log viewer watches its run and, once the server confirms, fetches the
lines it missed. The chat thread watches the run that is live in it the same way. After a reconnect the live indicator
shows "live" only once the server has confirmed every subscription the page holds. A watch added
on a connection that is already open does not change the indicator. Lists also refetch every 30s
as a fallback.

## Pages

**Overview** (`/`): the board says where to start, then one lane per project.
- **The sentence on the board** (`BoardVerdict`, words from `src/lib/verdict.ts`): a charcoal band
  with a lamp, a headline and one sentence that names the most urgent thing and how many more
  follow: "Work is stopped on widgets. 7 tasks on widgets are blocked: workspace setup failed. Start
  there. 4 more things follow." With nothing waiting it says "Nothing needs you" and whether runs
  are active or the line is paused; with no project, "Not set up yet". It waits for both lists
  (problems and approvals) before it says anything. If one of them cannot be loaded it says
  "Cannot tell what needs you" instead of guessing. At its right end is the line window: "Line
  running" or "Line paused", active runs, queued tasks and cost today.
- **What needs you** (`src/lib/needs.ts`): problems from `GET /problems` and pending approvals
  from `GET /approvals`, in one order: problems first (blocked tasks with the most tasks first,
  then failing loops, then blocked goals, each oldest first), then approvals, oldest first. The
  first one carries a "Start here" tag when there is more than one.
- **Owner-unreachable banner**: under the sentence, a red callout "Nobody will be told" while
  `/stats` says `owner_unreachable` (only the log notifier, no channel). For an owner it links
  to the fix, Config's Setup tab (`/config?mode=setup`), where Telegram, Discord or ntfy is set up;
  anyone else is told an owner can do that. It lists each blocked goal as "Goal X is blocked and you
  have not been notified: reason". With no project it is not shown: the setup step "Reach me"
  says the same.
- **Rate-limit holds**: under the sentence, an info note per harness held for a rate limit (from
  `holds` in `/stats`): "claude is rate limited. New runs wait until 15:04." Gone once the hold lifts.
- **Lanes** (`AndonBoard`, order from `src/lib/lanes.ts`): one row per project, the projects
  that need you first. A lane has three parts.
  - Its board window (from `/projects`). The lamp bar is lit by what the project needs
    (`src/lib/lamp.ts`): red when a task is blocked, yellow when approvals wait, off when paused,
    green when tasks run or are in review, off when only queued or idle. Under the project name
    the reason in words ("1 blocked", "2 need you", "3 running") and the other non-zero counts,
    then at the foot the last deploy from `/deployments` ("Deployed · 2d ago", "Rolled back · 1h
    ago" in the stop colour, "No deploys yet", or "No deployer" when the project has no
    `deploy`; `src/lib/boardDeploy.ts`). The window links to the project's tasks.
  - What needs you there, one row each (`NeedRow`): a lamp, a title that links to the thing, a
    muted line, the evidence (an error's first line and its last two) and the actions. Seven
    tasks blocked by one cause are one row: "Workspace setup failed", "7 tasks blocked
    · since 10:21 (3h ago) · told you 10:36 · last blocked 2m ago", the underlying error in
    monospace, **Check
    again** (retries one task, the one blocked most recently, to see whether the cause is gone)
    and **Retry all 7** (asks to confirm, then retries each). One blocked task: "Blocked:
    title", "attempt 1 of 3 · 8h ago", the error or the reason, **Retry** and **Open**. Failing
    loops: "2 loops are failing: Issue Intake, Friction", since when, how many passes failed,
    "told you 10:40" or "not told yet", the error, **Open Loops**. A blocked goal:
    "Goal blocked: title", its reason, **Open goal**. An approval: "Merge approval: title", the
    risk badge, the PR number, "asked 12m ago", the risk reasons and **Review**, which opens the
    task page; approving happens there, where the risk and the commit are shown in full. "told
    you" is the time onett last notified the owner after the problem stayed open (the
    heartbeat's escalation or the loop alert). A lane with nothing says "Nothing needs you
    here." The retry buttons need the operate action.
  - In flight: the project's live runs (`RunItem`): task title (links to the run), Cancel, the
    role, elapsed time and "output 12s ago", then a bar whose length is the elapsed time on one
    scale for the whole page (`src/lib/runbar.ts`): green up to the last output, yellow after it
    once the run has been quiet for 5 minutes ("quiet for 7m" in the text). Under them "3
    queued", or "Nothing running."
  - Projects with nothing going on (no need, no run, nothing queued, not paused) fold into one
    line: "Quiet: dotfiles, notes · no work in flight". Live runs of no configured project (a
    chat with no project) get a last lane named "No project". Empty: "No projects yet", with a
    link to the Projects page, where an owner adds one (anyone else is told an owner can).
  - On a phone a lane stacks: the window as a bar with the lamp at its side, then the rows with
    their actions under the text, then In flight.
- **Finish setting up onett** (owners only, `SetupChecklist`), while setup is incomplete. The
  steps, each linking to where it is done: Add a project (`/projects`), Agents ready (at least
  one enabled agent; `/config?section=agents`), Choose a policy (a project with no policy uses
  the default, which counts; `/config?section=policies`), Reach me (not `owner_unreachable` in
  `/stats`; `/config?mode=setup`), Secrets set (every secret the config refers to is set, from
  `/secrets`; `/config?mode=secrets`). With no project they are a row of five stations between
  the sentence and the lanes: "Step 1 · next" is outlined and holds the filled button, a done
  step says "done ✓", and Choose a policy "waits for a project". Once a project exists the card
  is one line under the lanes ("4 of 5 done · left: Secrets set (Not set: SLACK_BOT_TOKEN)")
  with "Show steps" to open the list and a Dismiss button (remembered in the browser). It hides
  itself when all are done. Logic is in `src/lib/setupChecklist.ts`.
- Below, two columns (one on a phone):
- Left: the **Orchestrator** card (`src/components/HeartbeatCard.tsx`): whether the heartbeat is
  on, disabled or in quiet hours and when it next checks; one sentence saying what it does; the
  last check; how often it told you and acted itself in 24h (added up over projects); the last
  error; and "Open Operations" once the Operations conversation exists. With no heartbeat entry
  in `/loops` it says "The heartbeat starts once a project exists."
- Right: **Activity** (`src/lib/activity.ts`): the event feed, newest first, as a time, one
  sentence and the project. The time is the clock for today, the weekday and clock for the last
  six days, then the date. The sentence names its subject as a link ("Senior Engineer started on
  Fix flaky widget cache test", "Blocked: Publish agent screenshots", "Merge approval asked:
  …", "Issue Intake failed · 13 times since 10:24", "Told you: blocked task on widgets").
  Stopped things carry a red lamp and approvals a yellow one, always with the words. Repeats
  within ten minutes fold into one row: the same thing again keeps its sentence and says how
  often ("… · 3 times"), and many things of one kind become a count ("7 tasks blocked:
  workspace setup failed · 10:21 to 10:27", "21 Senior Engineer runs failed"). Plumbing is left
  out: chat messages, a run that is running or succeeded, chat turns that did not fail, and
  the notification a block sends (its "Blocked" row is already there). Ten rows show, with
  "Show all N" to expand in place. Live events prepend.
- **Blocked reason** (`BlockedReason`, on the task page): the reason is clamped to three lines
  with a Show more / Show less toggle (only when it overflows), keeps the agent's line breaks
  and wraps long flags and paths inside the box, so Retry and Cancel stay in view.
- Not built yet: a compact lamp row on pages other than Overview, and fuller window details on
  focus. The nav badges carry "needs you" and "blocked" everywhere meanwhile.

**Projects** (`/projects`) — one sheet per project, in a grid (one column on a phone). Each
sheet is a window of the Overview's board. A 10px lamp bar runs across its top in the colour the
Overview shows for that project: green while work runs, yellow when something waits on the
owner, red when work stopped itself, grey when idle or paused. Under the project name are the
repo (a muted link to `https://github.com/<repo>`) and the lamp's label in words, such as
"1 needs you". On the right is the pause toggle (secondary "Pause" / danger-fill
"Paused — Resume"). When the project is paused in onett.yaml, the toggle is replaced by a muted
"Paused in onett.yaml" label, because the file wins and the UI cannot unpause it. Below the name
is a row of five counts: queued, running, review, needs you (awaiting approval) and blocked, with
12px labels at every width. Each
non-zero count links to `/tasks`, filtered by that project and status. Blocked is red and needs
you is yellow. Zero counts are muted and are not links. One muted line follows with the default
branch, the deployer type (or "no deployer") and the observers (or "no observers"). The last line
is the loops: "Loops: 5 on, 2 off", where "Loops" links to `/loops`. If any loop's last run
failed, its name follows in red ("pr_shepherd failing"), and the error shows as a tooltip.
Owners also get an **Add project** button in the page header and, on each sheet, **Edit settings**
(opens the Config page at that project's card) and **Remove**. Add opens a form: name, repository
(`owner/name`), local path, default branch, a policy dropdown of the configured policies and an
instructions box. Empty optional fields keep their defaults. Onett does not clone the repository:
the clone must already be at the path (default `<data_dir>/repos/<name>`). Problems the server
finds show under the form. Remove asks first, naming the project; when the project has queued or
running tasks the question says so and the removal is forced. It only edits `onett.yaml`; no file
on disk is deleted. Other roles see neither control. The sidebar gets **Projects** between Overview and Chat.

**Chat** (`/chat`, `/chat/:id`, `/chat/task/:taskId`) — talk to the orchestrator or to any task
agent, and read what an agent did as it works. With no conversation open, a centred block 640px
wide sits 8vh from the top: the heading "Talk to the orchestrator", a muted line on what it can and
cannot do, the composer, and three example prompts as chips. The composer is the thread's: the
same attach button and permission and model pickers, but no context ring: there is no run yet to
measure, so the ring first appears in the conversation once it has one. Choices come from
`GET /new-conversation/settings` and are held in the page, as are attached files, until you send.
Sending creates a conversation, stores the chosen settings on it, opens it, uploads the files and
only then sends the message, so the first turn runs with what you picked. If storing the settings
fails the draft stays and a retry reuses the conversation just created.

**Composer.** The field grows with what you type up to 40% of the screen height (`40dvh`, so the
phone keyboard is counted), then scrolls inside itself, and shrinks again after a send. Below it,
a 44px button reads **Send**; while a turn is running and the field is empty it reads **Stop**
instead (the same action as the status-bar button and Esc), and after a click it reads "Stopping…",
disabled and `aria-busy`, until the run ends. Typing turns it back into Send, which sends or queues
the message as before. The draft text is kept per conversation in the browser (`localStorage`, key
`onett.chat.draft.<conversation id>` or `onett.chat.draft.task:<task id>`, saved about 300ms after
typing stops), restored when you come back and cleared when a send succeeds; a failed send keeps it.
Attachments are not kept. If the browser refuses storage, the draft lives in memory only.
While you type, the composer card's edge steps up from its faint hairline to muted ink; there is
no focus ring around the card. Its buttons and menus show a 2px ink ring when the keyboard reaches
them (in dark mode too: chat does not use the app's call-yellow focus ring).

**Composer controls.** The footer follows Orca's layout: **+** (attach) and a permission picker on
the left; a model and effort picker ("Opus · High ⌄"), a small context ring and the round Send/Stop
button on the right. The pickers are compact popovers that open upward: arrow keys, Home and End
move between items, Esc closes and returns focus to the button. Each has a "Reset to role default"
item. Choices come from `GET /conversations/{id}/settings` (the harness's own list, plus models
named in the agents config); a pick is saved at once and applies from the next turn. Choosing a
permission mode that skips safety checks (Claude's `bypassPermissions`, Codex's
`dangerously-bypass-approvals-and-sandbox`) asks for confirmation first, once per conversation. The
ring is the last run's context size (the prompt of its final model request, not the input tokens summed over the run) over the model's context window (turns amber at 70% and red at
90%); it is hidden when the window is unknown. On a phone the permission picker shows only its icon.
In a task agent's thread the same pickers appear once the task has a conversation, next to the
agent's role; before that, and for people who cannot operate, the footer shows the role, model,
effort and permission mode read-only, from its last run. Only people who can operate see the pickers.

**Attachments.** The composer of an open orchestrator conversation takes files three ways: the
paperclip button ("Attach files", a file picker that allows several), pasting files from the
clipboard into the field (a pasted screenshot, for one; pasted text is left alone), and dragging
files over the composer, which shows a "Drop files to attach" overlay. A file starts uploading the
moment it is chosen (`POST /conversations/{id}/attachments`) and shows as a chip above the field: a
thumbnail for an image or a document icon otherwise, the name, a progress bar while it uploads and
then its size, and a × to remove it (which cancels a running upload). A file that fails shows its
error in red on the chip instead, for example "Too large" with the server's limit (413), and is not
sent. Only `chat.attachments.max_per_message` files (10 until the config says otherwise) count; the
extra ones are added as red chips reading "Only N files fit in one message". Send waits while any
upload is running, and a message may be only attachments with no text. Task-agent conversations and
the start page (no conversation yet) have no attach controls: the first message there is text only.
A sent message shows its files under the text: a PNG, JPEG, GIF or WebP as an inline thumbnail (up
to 320px wide on a desktop, the full width of the bubble on a phone, aspect ratio kept), anything
else as a chip with the name and size, each opening the file in a new tab. If the file is gone
(retention deleted it) or the browser cannot draw it, the thumbnail falls back to the plain chip.

Scrolling: a thread follows new messages and a streaming reply only while you are within 80px of the
end of the page. Scroll up and it stays put; a **Jump to latest** pill (with "N new" for the entries
that arrived meanwhile) floats above the composer, and clicking it scrolls to the end. Sending a
message, opening a conversation and switching conversations always go to the end. With
`prefers-reduced-motion` the jump is instant instead of smooth.

The 260px sidebar has a "+ New conversation" button (always the orchestrator; operators only), an
**Orchestrator** section of conversations (title, RelTime), then a quieter **Agents** section from
`GET /agent-conversations`. Each task with an agent shows its title, and under it a row with a lamp,
the role and a text label: a blinking green lamp and "working" while a run is live; a dim lamp and
"closed" for a closed conversation; a red lamp and "blocked" for a blocked task; a dim lamp and
the task's status otherwise. On desktop the sidebar is as tall as the window at most: a long list scrolls inside it and it stays
pinned beside the thread, so it never sets the page's height. Under 760px an open thread replaces the sidebar with one bar: a "Conversations" back link to
`/chat` (where the full list and New conversation show) and the open conversation's title, muted and
cut with an ellipsis on one line.

The header names who you are talking to ("Orchestrator", or the role such as "Implementer"), with
harness · model (from the latest run) and the conversation title, or for a task agent its task as
a link, its branch and status. On the right sit Close (task agents, operators, while open) and the
**Conversation / Terminal** switch; the choice is remembered in the browser. The transcript is the
same one the stored run logs give: owner messages as right-aligned sheets, agent prose as Markdown
under the agent's name, each tool call as one row (name, main argument, `+n −m` for edits) that
opens to its input and result, and a "Denied" line in the danger colour for a refused call. Tool
rows are quiet single lines: onett's lamp (blinking while live, then done or failed), faint text, a
chevron, and on the right the status and, when known, how long it took ("completed · 1m 44s"; the
log has no per-call token counts, so none are shown, and on a phone the duration drops first). Two
or more tool calls in a row fold into one **tool run** line: a sentence about what was done ("Ran 3
commands, read 2 files, edited 1 file"), with the total `+n −m` and file count when it edited files.
While the run is live the line names its latest call ("Running go test ./...") with a lamp, then
settles into the sentence. Opening a run lists the changed files, then each call as its own row; the
detail sits in one rounded surface with a hairline. Once a turn is done, the work between the owner's
message and the answer folds under a faint **"Worked for 1m 40s ›"** line with a hairline after it;
click it to open the work. A live turn is always open. Denials and errors stay visible when folded. Terminal view shows one
line per entry with a time gutter, `›` for owner and prompt, `●` for prose and tool calls and `⎿` for
results clipped to three lines with "show more". A task agent's view shows each run in order, with a
divider ("New session") where a retry started a new session; a run with no recorded prompt starts with the task body
labelled "Task (prompt not recorded)". A run that did not succeed ends with a system line,
"Run ended: <status>" and the error when there is one, because a stalled or denied run has no last
words of its own. Lines of the live run arrive as `run.log` events, so tool
rows appear as they happen. When an orchestrator turn created tasks, a chip under that turn shows
each task's lamp, title and status, with "open agent" (`/chat/task/:id`) and "task" links.

While a run is live a line shows "Working · 3m 41s · output 2s ago" with a link to the run and a
**Stop** button (Esc in the field does the same); both cancel the live run. The composer stays
enabled: a message sent mid-run reaches a Claude agent at once (the orchestrator, or a task agent
whose harness takes input), and is queued only for a harness that cannot take input. Queued messages show at the end in a dashed box, "Queued · sends when the agent
finishes this run", with edit and cancel (no longer possible once delivered). When messages are
queued and no run is live, for example after a failed run, **Send now** starts the follow-up.

A task agent's view adds a yellow note above the box, "You're talking directly to this task's agent.
For anything beyond this task, ask the orchestrator." (the last words link to `/chat`). Close asks
for a reason. A closed conversation replaces the composer with "Closed by <who>: <reason>", shown as
plain text, and a **Reopen** button for operators and owners. When the server refuses a message (a
409) the composer shows its reason. Retrying a cancelled task does not reopen its conversation: the
owner reopens it. Send now, and editing or cancelling a queued message are for signed-in people
only; an API token gets 403. The agent token (the orchestrator) can send a message, which the thread
labels "Orchestrator". Viewers read the thread and see a muted line, "Viewers can read conversations
but not send messages.", in place of the composer. A trigger's conversation (titled "Trigger:
<name>") shows no model or permission pickers, even to an operator: the composer shows the model
read-only, and a muted line under it says "A trigger's turns always run with the orchestrator's
default model and permissions, so they cannot be changed here." The server would store settings
there but never apply them.

**Goals** (`/goals`) — table: status, goal (links to the detail), project, turns, updated and the
controls. Filters: project and status. Controls follow the API's rules: Pause on an active goal,
Resume on a paused or blocked one, Block on an active or paused one, Mark done on any goal not
done. Done and Block open a text box first, because the API requires a summary or a reason. The
controls need the operate action.
With no goals the page says how to start one: ask the orchestrator in Chat, or run
`onett goal create --project P --title T`; with a filter set it says
"No goals match these filters."
**Goal detail** (`/goals/:id`): title and status, the controls,
the project, the turn count, a link to the goal's conversation in Chat, the summary (a callout
when blocked), the goal text and the tasks queued for it under a Tasks section heading. The goal
text is rendered Markdown, 20px below the meta line; there, as in task bodies, a heading has 20px
above it and 6px below, and paragraphs and lists 8px below. An ID the server does not know shows
"Goal not found", one line saying why, and a Back to goals
button. `goal.updated` events refresh both.

**Tasks** (`/tasks`) — filter bar: project select, status chips (multi-select; default
"active" = queued, running, review, awaiting_approval, merged, deploying, blocked), kind
select, an Order select (Pickup order, the default, or Newest created), text search (client-side
over title). In pickup order the table is split into sections: Running now (oldest start first);
In review, CI or deploy; Queued, in the order they will start, each with its place beside the
status ("#1 next", "#2", plus "#N in project" when the project's own count differs); Waiting on
other tasks (says what for); Blocked; and Finished, collapsed. "New task" opens a modal: project, kind,
title, body (textarea, Markdown), role (optional; lists agents), priority (number). Table
columns: status, title (with kind tag and source muted under it), project, role, risk, PR
(#123 link), attempts (`1/3`), updated.
Each column but the title has a set width, so the columns line up from one section to the next;
the title takes the rest.

**Task detail** (`/tasks/:id`) — a header row with the title, StatusPill and the Cancel and Retry
actions (role-gated as before). Under it, one muted line: kind · project · role · from source, then
the IdChip and, once the task has run, an "Open conversation" link to `/chat/task/:id`. Below that is the task's line: seven lamps (Queued, Running, Review, Approval, Merged,
Deploying, Done) joined by a rule. The station names are 12px at every width; under 520px they
give up their letter-spacing so all seven fit. Passed stations show the done lamp; the current one lights in
its tone (running blinks); a stop is red and its label reads Blocked, Failed or Cancelled; skipped
stations are hollow; stations not yet reached are dim. From 960px the page has two columns: the
main column, and a 300px spec sheet that stays in view. The sheet lists risk with its reasons,
pull request and branch, attempts, project, kind, role, source, parent, created, updated and ID.
Under 960px it is one column, with the sheet directly after "Needs you". The main column, top to
bottom: Needs you (only when something waits: the blocked reason with Retry and Cancel, a note
that the project is paused, and pending approvals with their decide controls); Summary and Body
(Markdown, when present); Runs (# is the attempt number in start order, then status, harness ·
model, started, duration, cost and link); Subtasks (when there are any); Decided approvals (one
compact row each: decision, title, time and note).
An ID the server does not know (deleted, or a wrong link) shows "Task not found", one line saying
why, and a Back to tasks button instead.

**Run detail** (`/runs/:id`) — header: "role · project" with the StatusPill, and Cancel run on
the right while the run is active (operators only). Under it, muted: harness · model, then the
task title as a link and "Open conversation" (`/chat/task/:taskId`). Then the time strip, the run's bar on its own axis: the same bar as the
Overview's live runs, drawn to the run's elapsed time, green up to the last output and yellow
after 5 minutes without output. The axis ticks sit above it, and at the right of the ticks the
elapsed time with "output 4s ago" or "quiet for 7m". A finished run is one solid bar and no
yellow: grey when it succeeded, red when it stopped itself (failed, stalled, timed out, denied),
light grey when cancelled. Under the strip, a row of facts (label above value): branch, cost,
tokens, started, duration, and exit code once the run has ended. "More" expands workdir, session
ID and run ID. A run's error shows in a red callout above the log. The log takes the rest of the
screen (at least 360px on a desktop; 60% of the screen height on a phone). Its toolbar has "Log",
the line count, and a "Follow" toggle (default on) that keeps the view at the newest line. Log
lines: `text` plain; `tool_use` with a green drawn chevron before it; `tool_result` muted,
collapsed to 3 lines with "show more"; `denial` on a red-tinted line with a "DENIED" tag before
the text; `prompt` (what onett gave the agent to start the run) a muted block labelled "Prompt"; `owner` (a message from the owner) a line labelled "You"; `stderr` muted italic; `error` red; `system` muted. The time gutter is 64px (52px on a
phone). Logs load from `/runs/:id/logs` and then append `run.log` events for this run,
deduplicated by `seq`.
An ID the server does not know shows "Run not found", one line saying why, and a Back to runs
button instead.

**Approvals** (`/approvals`) — pending by default, with a Pending / All control in the header.
The list is at most 920px wide so the agent-written prose stays readable. Pending with nothing
waiting shows one muted line: "Nothing waits on you. Approvals appear here when a change needs
your sign-off." "All" adds decided approvals as cards, each with its status pill. Cards: title,
kind, project, task link, reason (Markdown), created; Approve (primary) / Reject (danger) with an
optional note field. Full width under 760px.

**Runs** (`/runs`) — table: status, task, role, harness · model, started, duration, cost.
The task cell shows the task's title as a link to the run's page, with a smaller line under it
giving the project and the task's short ID (a link to the task). Active runs (starting or
running) come first; the rest keep the server's order, newest first. Filters: project, status.
On a phone the status is the card's title row and the task comes second, because the stacked
table always takes the first column as the title. With no runs at all the page says
"No runs yet" and what a run is; "No runs match these filters" appears only when a filter is set.

**Deploys** (`/deploys`) — table: project, ref (short SHA mono), state (the deploy's notes sit
under it in muted text), URL, task, started, updated and Log (a View button that opens the
release's log in a dialog; a dash when there is none). Filter: project.

**Signals** (`/signals`) — table: project, source, title (links to URL), level, count, last
seen, task link.

**Friction** (`/friction`) — table: project, title, count, issue (link), updated.

**Loops** (`/loops`) — a table with one row per loop and project, grouped by loop: Loop,
Project, Status, Schedule, Last run, and a Run button for operators (viewers see no Run column).
The loop cell shows the id as Title Case words ("Issue Intake", "PR Shepherd") with the id itself
under it in small mono. Loops that run across all projects say "all projects" in the Project
column. Status is a 10px lamp and a word: "OK" (green), "Failed" (red), "Off" or "Not run yet"
(grey). Schedule reads "every 30s" (muted when off) and, for a loop that reports it (heartbeat),
"next in 12m" under it. Last run reads "3m ago" (with " · 3 items" when reported) or "never", and a
failed loop's error under it in red, cut to one line, with the full text on hover. Run is an
outlined button that reads "Running…" and is disabled until the run returns; then a line above
the table says "Ran Issue Intake for onett." Above the table, one muted line counts loops on, off
and failing (failing in red when any). When the main column is under 880px (a phone, or a narrow
window beside the rail), the table becomes one sheet per loop, with a row for each project it runs
in. With no loops configured the page says so and links to Config's Loops group
(`/config?section=loops`).

**Triggers** (`/triggers`) — rules the owner writes that start the orchestrator on their own: on a
schedule, or when a new issue is opened in a project's repository. Two columns. On the left,
**Rules**: one card per trigger, sorted by name with switched-off triggers last, and beside the
heading "5 on · 1 off · 1 failing" (failing in red when any). A card has a lamp bar down its left
edge and the same thing in words under its name: green "On" (with "next in 17h 00m", or "waits for
the next issue"), yellow "Skipped its last firing", red "Last firing failed", grey "On · not fired
yet" or "Off". The name opens the trigger's own page; the project is a tag beside it. Three rows
follow. **When**: the schedule in words and its time zone ("Every day at 05:00 · UTC", "daemon's
time zone" when none is set), or "A new issue in owner/repo · label bug" ("any label" without one).
**Ask**: the prompt, cut at two lines. **Last**: the last firing, as "6h ago · scheduled 05:00 ·
Started", with "Open conversation" when it started, the reason in yellow or red when it was skipped
or failed ("Skipped: the factory was paused"), and "All firings", which also opens the trigger's
page; "never" before the first firing. On the right, **Recent firings**: the newest 50 firings of
every trigger from `GET /trigger-firings`, grouped under Today, Yesterday and dates, each with its
clock time, a lamp and "Started", "Skipped" or "Failed", the trigger's name (taken from the list of
triggers), what caused it ("scheduled 09:30", "run now by bcraig", or "issue #412" as a link to the
issue), and under it "Open conversation" or the reason. Under 760px the two columns are shown one at
a time, chosen with two buttons, Triggers and Firings; the choice is in the URL (`?tab=firings`).
With no triggers the left column says "No triggers. A trigger starts the orchestrator on its own, on
a schedule or when a new issue is opened." and, for an owner, offers two examples ("Every day at
05:00" and "A new issue is opened"), each with **Use this**; the right column says "Nothing has
fired yet. Each firing is listed here with what started it and what came of it." Only an owner sees
**New trigger** in the header and, on each card, **Run now** (schedule triggers only: an issue
trigger cannot be fired by hand), **Edit** and **Turn off** or **Turn on**; everyone else sees "Only
an owner can add or change triggers." and no controls. Run now reads "Running…" until the server
answers, then a line says "Fired <name>.", or "Skipped <name>: <reason>." when the firing was
skipped. Turn off and Turn on save at once. Clock times are the browser's; the time in "scheduled
05:00" is in the trigger's own zone. A trigger with no zone of its own runs in the daemon's zone,
which the browser does not know, so its times are shown in the browser's zone: right when the two
are the same.

**New trigger / Edit trigger** — one dialog for both. A new trigger has Name and Project, then
**Starts**: "On a schedule" or "On a new issue". Kind and project cannot be changed later, so
editing shows them as one sentence instead ("Schedule trigger for home-lab. The kind and the
project cannot be changed; make a new trigger for another one."). A schedule has **Repeats**:
Every day (with a time), On chosen days (a time and seven day buttons), Every few hours (1, 2, 3,
4, 6, 8 or 12), Every few minutes (5, 10, 15, 20 or 30), or Custom (a five-field cron
expression, checked by the server on save); and **Time zone**, a list that starts with "Daemon's
time zone" and, for a new trigger, has the browser's zone chosen. The time is typed as 24-hour
HH:MM. Under the fields a note says the schedule back in words and shows the cron it saves
("Fires Monday to Friday at 09:30, UTC time. Saved as cron 30 9 * * 1-5"), or what is still
missing ("Choose at least one day."). The page writes and reads only these shapes
(`src/lib/triggerSchedule.ts`); any other stored expression opens as Custom and is shown as it
is. An issue trigger has **Label** (empty for every new issue) and a note with the rules: only
issues opened by an owner, member or collaborator count, only those opened after the trigger was
last turned on, and never issues that issue intake takes or onett's own friction issues. Then
the **Prompt**, and an **On** tick, which is set for a new trigger and always sent (the API's own
default is off). Editing sends only the fields that changed, because sending "on" again would
restart an issue trigger's watch from now. A problem the server finds (a bad cron or zone) shows
under the form.

**Trigger detail** (`/triggers/:id`) — the name with its status pill (OK, Skipped, Failed, Not
fired yet or Off), and for an owner **Run now** (schedule triggers only), **Edit**, **Turn off**
or **Turn on**, and **Delete**. Delete asks first: the button reads "Confirm?" for three seconds
and a line says "Deletes the trigger and its firing history. Its conversation stays in Chat.";
afterwards the page goes back to the list. Under the header: the kind and project, and "Open
conversation" once the trigger has fired (every firing of a trigger goes to one conversation). A
sheet of facts: for a schedule, When (in words, or "Custom schedule"), Time zone, Next firing
(in the trigger's zone, with "in 17h 00m"; "none while it is off") and Cron; for an issue
trigger, When, Repository (a link), Label and "Issues since", the moment from which new issues
count. Then the **Prompt**, as plain text, and **Firings**, newest first, the newest 200: When
(date and time, with "6h ago"), Why ("Scheduled for 05:00", "Run now by bcraig", or "Issue #88"
as a link) and Outcome (a pill, started, skipped or failed, with the reason beside it). Before
the first firing: "This trigger has not fired yet." An unknown ID shows "Trigger not found" and a
way back to the list.

**Users** (`/users`, owners only; anyone else is sent to Overview) — a table of accounts:
username (your own row says "(you)"), role, added (a relative time, or a calendar date once it is 30 days old; the full timestamp shows on hover) and actions. The role is a
select that saves as soon as it changes; changing your own role reloads your session so the
menus match it. "Set password" opens a field under that row with Save and Cancel; your own row has none, because "Change password" in the account panel asks for the current password. "Remove"
asks for confirmation, and is not offered on the only owner's row, because the server always
refuses that. "Add user" opens a sheet above the table with username, password and
role (default operator) and "Create user" or "Cancel". The server's message is shown under the
table when it refuses a change, such as demoting or removing the last owner.

**Agents** (`/agents`) — card grid of the configured roles. Each card shows the title (or the name when it has none, with the name beneath), a badge
(`built-in`, `edited`, `custom` or `removed`: where its settings come from, or that it is switched
off), the description, its **Context** files and **Tools** rules when set, the harness and its type, the model, effort and permission mode (each "harness default" when unset), and **Used for**:
one line per task kind or route that uses the agent, with the project when one is named. The prompt
shows its first 8 lines with "Show full prompt" to expand it. An edited built-in that no longer
receives onett's prompt updates says so, and "Compare with built-in" puts the built-in text beside
the customised one (stacked on a phone). Enabled agents come first; removed ones sit under a
"Removed" heading. When the config file's routing (global or a project's) names an agent, its card says
before Remove which routes go with it and which agent each kind then uses; Remove drops those routes
in the same save, since the server refuses routing that names a missing or disabled agent. Anyone can read the page. An owner also sees:
"New agent", which opens a sheet with name (refused when taken), description, harness (the
configured harnesses), model, effort, permission mode, args (one per line), and the prompt, either a reference such as
`builtin:triager` or a path, or text written here. "Start from" copies another agent's settings
into the sheet, or starts blank. An enabled card has "Edit"; "Remove" (a built-in or edited agent is
switched off, a custom one is deleted); and "Reset to built-in" (an edited one). Remove and reset ask
for a second click ("Confirm?"). A removed card shows "Restore" only, with no Edit: restore it first,
then edit it. A refused change, such as removing an agent a route still uses, shows the server's
reason on the page and changes nothing.

**Config** (`/config`) — everyone sees the file path and the redacted config as syntax-highlighted
JSON (keys `--info`, strings `--ok`, numbers `--warn`), with the note "Only an owner can change
settings. Secrets are hidden." Operators also get a Reload button. An owner gets the editor, which
opens on the **Visual** tab. The settings of `onett.yaml` sit in groups in a list on the left, in
the order Projects, Agents & routing, Policies, Limits & budget, Loops, Goals, Self-update,
Channels & notifiers, Chat, Server, then Users (a link to the Users page; users are not in the file). A
square `--lamp-call` after a group means unsaved edits, `--lamp-stop` problems. The table that says
which settings each group shows is `web/src/lib/configGroups.ts`; its test fails when a setting is in no group
or in two. The active group's settings are on the right. A setting
with a built-in default shows it as the input's placeholder ("default: 15m") while unset, and once
it is set a "Reset to default" link removes the key again. A number, text or duration setting is an input; a
checkbox is "On", with a "Default: on" note while unset when leaving it out of the file means on (a loop's `enabled`, friction's `auto_file`, a deploy's `auto_rollback`); clearing an input removes the key. A value written as `${NAME}` shows as text with
a note that onett reads that environment variable. A map is one card per entry, with Rename and
Remove (Remove asks first), and a form at the end to add one. The Agents map also names the built-in agents the file does not write (they are in effect without an entry), so a fresh install's empty list is not read as no agents. A list has a row or a card per item,
with Remove, and an Add button. A setting with fixed choices, such as a notifier's `type`, is a
dropdown. The `options` of a notifier, channel, deploy block or observer show the fields of the
selected `type`. A setting that names an environment variable for a secret is a picker: a box that
suggests the known names (marked set or not set), accepts a new name written in capitals, and offers
a suggested name when empty (such as `TELEGRAM_BOT_TOKEN`). Beside it, Set value (or Replace) stores
the secret through the Secrets API, with the same write-only rules. That is separate from the config
edit: the name still needs the Save bar. Changing `type` keeps the options the new type also has and asks before dropping the rest;
with no `type`, or one this onett does not know, `options` is edited as JSON. Any other value with no
simple form is edited as JSON. Each entry of `notify` and `channels` has a **Send test** button (see below). Edits stay on the
page until saved: a bar at the bottom says how many there are, with Save and Discard. A save writes
nothing when the file changed on disk since the page opened (a notice offers to load the latest,
which discards your edits) or when the result would be invalid (the problems are listed above the
editor, and each is also shown under its field). Settings that take effect only after a restart are
named in a note until dismissed; the note has a **Restart now** button that asks first, restarts the daemon (`POST /restart`) and waits for it, at the new address when `server.addr` changed, then reloads the page there (it says so if the daemon is not back within a minute). A link can open one map entry: `/config?section=projects&entry=onett` (`section` is a group id; an older top-level key such as `routing` still opens its group)
opens that group and scrolls to the `onett` card, which gets an `--info` outline (Projects'
Edit settings uses it). **Send test** sends one clearly labelled test notification (a notifier) or message (a chat
channel) through the saved entry and shows the result beside the button; a failure shows the reason
without any secret. It is disabled with "Save first" while that entry has unsaved edits, because the
daemon tests what is saved. The **Setup** tab (`?mode=setup`) is a guided flow for Telegram,
Discord and ntfy: it explains how to get the bot token and your user ID, takes the token in a
password field and stores it through the Secrets API (as `TELEGRAM_BOT_TOKEN`, `DISCORD_BOT_TOKEN`
or `NTFY_TOKEN`, or the variable the entry already names), writes the same config entry `onett init`
writes, and has a test button. The token is sent once, the field is emptied at once, and nothing
shows it again. An existing entry of that type is edited instead of duplicated, and the server's
validation problems are listed under the Save button. The **YAML** tab edits the raw text of the file, with the same bar
and checks, and switching tabs with unsaved edits asks first. The **Secrets** tab manages
`secrets.env` and is write-only: each name shows set or not set, where the value comes from
(`secrets.env` or the daemon's own environment, which wins and is marked "shadowed") and which
config paths use it. Set and Replace take the value in a password field that is cleared after
saving; Delete asks to confirm; no value is ever shown. A saved secret applies at once, including
to chat bots and notifiers. "Reload from file" makes the daemon read the file again.

## Screenshots for UI PRs

Every PR that touches `web/` shows before and after screenshots at desktop and phone width, and
the author (and the reviewer) looks at them. The two Playwright configs for the mocked checks
(`playwright.config.ts`, `playwright.shots.config.ts`) each pick a free port when they start, so
runs in parallel worktrees never collide; set `ONETT_MOBILE_PORT` or `ONETT_SHOTS_PORT` to pin one.

Every chat UI PR shows before and after screenshots. One command takes them; who publishes them
depends on who is working:

- **An agent that onett runs** (`ONETT_OUTBOX` is set in its environment) holds no GitHub access
  and cannot run the script below. It copies the PNGs into `$ONETT_OUTBOX/shots/before/` and
  `$ONETT_OUTBOX/shots/after/`, and onett publishes them with the run; see "Agents that onett
  runs" below.
- **A person, or an interactive agent that can push to the repository**, publishes them with
  `scripts/pr-shots.sh`:

```sh
cd web && npm run shots:chat && cd ..    # on the base commit
scripts/pr-shots.sh before
cd web && npm run shots:chat && cd ..    # on the PR head
scripts/pr-shots.sh after
```

`npm run shots:chat` builds the UI and runs `web/e2e/chat-shots.spec.ts` against `vite preview`
with the same in-browser mock as the mobile check, so no daemon is needed. It captures fifteen
scenes (among them `chat-thread`, `chat-running`, `task-thread`, `chat-empty`, `chat-error`,
`chat-loading`, and the focus scenes `composer-focus`, `composer-keyfocus`, `task-thread-focus` and
`chat-empty-focus`, which click into the composer and type, or Tab to its first control) in four
variants each (desktop 1280x800 and iPhone 390x844, light and dark): 60 PNGs in
`web/e2e-results/chat-shots/<scene>-<variant>.png`. The clock is frozen and animations are off, so
two runs of the same code give the same pixels. It is not part of `npm run test:mobile`.

`scripts/pr-shots.sh <label>` copies those PNGs to a branch `pr-assets/<current-branch>/<label>`
(an orphan branch, created on first use) in a temporary git worktree, pushes it, and prints a
Markdown table with permanent `raw.githubusercontent.com/<repo>/<commit-sha>/...` image links.
Paste the `before` and `after` tables into the PR description. Your own branch and working tree
are not touched, and the worktree is removed when the script exits. The script reads only
`web/e2e-results/chat-shots/`, so it publishes the chat shots and not those of `npm run shots`.
Never merge a `pr-assets` branch.

### Any other page: `npm run shots`

`npm run shots:chat` covers only the Chat page. For every other page (Overview, Projects, Goals,
Tasks, Runs, Approvals, Deploys, Signals, Friction, Loops, Triggers, Agents, Config, Users, and a
task, run and goal detail page), use the same mocked approach, which needs no daemon and no token:

```sh
cd web && npm run shots
```

It builds the UI and runs `web/e2e/page-shots.spec.ts` (config: `playwright.page-shots.config.ts`,
free port, pin it with `ONETT_PAGE_SHOTS_PORT`). The spec reads the routes from `src/App.tsx`, so a
new page is shot without editing it, except that a route with a `:param` needs an id in its `PARAMS`
table. It signs in as an owner, so Agents, Config and Users show data. Each page is shot at desktop
(1440x900) and phone (390x844), dark and light, into
`web/e2e-results/shots/<route>-<desktop|phone>-<dark|light>.png`; `/` is `overview`, and a detail
route carries its id, for example `tasks_<id>-phone-light.png`. Extra shots show an empty Tasks and
Runs page (`tasks_empty`, `runs_empty`), an Overview with one project (`overview_one_project`), a
chat run with no task (`runs_<id>`) and a goal check-in conversation (`chat_<id>`). For Triggers
there are an empty page (`triggers_empty`), the read-only list and trigger page a viewer sees
(`triggers_readonly`, `trigger_readonly`), the phone's Firings tab (`triggers_firings`), the dialog
for a new trigger and for editing one of each kind (`triggers_new`, `triggers_edit_schedule`,
`triggers_edit_issue`: a scene's `press` names a button to press before the shot), and an issue
trigger's page.

The shots cut each page at the window height, so anything below the fold is not in them, and a
cut-off edge can look like a bug. For long pages, lists and anything below the fold, run
`SHOTS_FULL=1 npm run shots`: it captures the whole scrolling page (the same as `shoot-live.mjs
--full`) into `web/e2e-results/shots-full/` with the same file names, and leaves the default shots
untouched. A chat thread scrolls inside its own panel, so it is not lengthened by this.

The data is `web/e2e/page-fixtures.ts`: the shapes a real daemon holds (chat runs with a
conversation and no task, goal check-ins started by the daemon, 60+ character branch names, long
role names, an image attachment, timestamps from minutes to days old, done, blocked and active
goals, deploys with and without notes, snake_case loop names, multi-paragraph agent prompts). Add
a case there when a page breaks on a data shape the fixtures do not have. `fixtures.ts` is the
opposite, layout-stress data for `npm run test:mobile`.

The clock is frozen, the timezone is UTC, animations are off and the browser rasterises in software,
so two runs of the same code give identical PNGs, and it can run at the same time as
`npm run test:mobile`. Take the shots on the base commit and on your head and look at the pages you
changed. An agent that onett runs then copies those PNGs to `$ONETT_OUTBOX/shots/before/` and
`.../after/` (see below; at most 48 files, so only touched pages). `shoot-live.mjs` shows the same
pages with real data but needs an API token that worker agents do not hold.

### Agents that onett runs: `$ONETT_OUTBOX/shots/`

Worker agents (the agents that onett runs, as opposed to a person or an interactive agent) hold
no GitHub access and cannot run `scripts/pr-shots.sh`. They copy the PNGs, from either command,
into `$ONETT_OUTBOX/shots/before/` and `$ONETT_OUTBOX/shots/after/` instead. When onett publishes
a run, it pushes each label directory (only plain `*.png` files, at most 48 of 8 MB, no symlinks)
to the same `pr-assets/<branch>/<label>` branch and adds a "Screenshots: <label>" section of
`raw.githubusercontent.com` image links to the PR description. This needs a GitHub project
(`repo: owner/name`); a failure is logged and does not fail the run.

### Real-app shots: `scripts/shoot-live.mjs`

`npm run shots:chat` shows mocked data. To see the real app with real data, point
`web/scripts/shoot-live.mjs` at a running daemon:

```sh
cd web
ONETT_API=http://127.0.0.1:7777 ONETT_TOKEN="$(onett token)" node scripts/shoot-live.mjs
```

It writes `<route>-<desktop|phone>-<dark|light>.png` to `web/e2e-results/live-shots/` (`--out DIR`
to change it): 1440x900 and 390x844, in both colour schemes. The default routes are every route
in `src/App.tsx`, plus one detail page each (task, run, goal, chat) using the first id the API
returns; a detail page with no data is skipped with a note. `--routes /tasks,/runs` picks
routes, and `--full` captures the whole scrolling page instead of the first screen.

The live event stream needs a session cookie, which a token is not, so the script answers the
WebSocket locally (it accepts and confirms subscriptions). The sidebar light therefore reads "live"
in these shots, but no live events arrive.

An API token cannot read owner-only data, so Agents, Config and Users show "forbidden" in these
shots. That is on purpose: giving the token (or a screenshot mode) owner powers would break the
rule that an agent cannot reach the owner's settings. The script prints a `note:` for each route
that got a 403, so it is not mistaken for a UI bug; use `npm run shots:chat`-style mocks to
design those pages.

It cannot change anything: the browser aborts every request that is not a GET. It sends
`Authorization: Bearer $ONETT_TOKEN` on API calls and never prints the token. A token is not a
session, so `GET /api/v1/auth/state` is answered locally with a signed-in owner; pages that need
a real owner session (Agents, Config, Users) therefore show `forbidden` for their data. It needs
Chromium from `npx playwright install chromium` (or the cache Playwright already has). Worker
agents hold no API token, so they use `shots:chat`; use this against your own daemon, never a
shared one you do not own.
