# Chat UI: onett vs Orca, item by item

This compares onett's web chat (`/chat`, `/chat/task/:id`) with Orca's native chat
([stablyai/orca](https://github.com/stablyai/orca), `src/renderer/src/components/native-chat/`) one
item at a time, and shows before/after screenshots. It is the last step of the chat UI goal; the
design rules it was built to are in [chat-visual.md](chat-visual.md).

**Status words.** *Matches*: same behaviour, only styling tokens differ. *Adapted*: same idea, built
differently on purpose or by necessity (the line says why). *Left out*: not built.

**How this was checked.** Every claim below was read from the onett code at `main` (`9232627`, the
head of this PR's base) and from the Orca source on its default branch, not copied from earlier PR
descriptions. The scenes were rendered with `npm run shots:chat` (the in-browser mock, no daemon). I
did not re-run `web/e2e/mobile.spec.ts`; where a touch-target claim points at it, the evidence is
that test's source, not a fresh run. Contrast numbers in `chat-visual.md` are measured by hand; no
test computes them (see Accessibility).

## Summary

| # | Item | Status |
|---|---|---|
| P0-1 | Stick-to-bottom and Jump to latest | Adapted |
| P0-2 | Code blocks: header, lazy highlighting, wrap, copy | Adapted |
| P0-3 | Copy on messages | Adapted |
| P1-4 | Tool runs and changed-files summary | Adapted (no inline diff cards) |
| P1-5 | Composer and drafts | Adapted |
| P1-6 | Activity line | Adapted; reasoning disclosure **Left out** |
| P1-7 | Per-frame streaming | Adapted |
| P1-8 | Timestamps and long-message fold | Adapted |
| P2-9 | Tokens and type scale | Adapted |
| P2-10 | Empty, error and loading states | Adapted |
| P2-11 | Long-thread performance | **Left out** |
| P2-12 | Find-in-chat and message rail | **Left out** (slash commands and @mentions out of scope) |

## Items

### P0-1 Stick-to-bottom and Jump to latest: Adapted

- **onett.** A thread follows new content only while the reader is within 80px of the end (page or
  nearest scrolling ancestor). A `ResizeObserver` on the transcript keeps it pinned while a streamed
  reply grows inside a row. Scrolling up stops it. Sending, opening and switching conversations jump
  to the end instantly; the **Jump to latest** pill shows only while away, carries "N new" for entries
  that arrived meanwhile, and scrolls smoothly unless `prefers-reduced-motion` is set. The hook ignores
  the scroll events of its own smooth scroll.
- **onett files.** `web/src/lib/useStickToBottom.ts`, `web/src/components/JumpToLatest.tsx`, wired in
  `web/src/components/Composer.tsx` (`ComposerArea`) and `web/src/pages/Chat.tsx`; tests in
  `useStickToBottom.test.tsx` (8 cases) and `e2e/mobile.spec.ts` ("chat Jump to latest pill…").
- **Orca file.** `NativeChatMessageList.tsx` and `NativeChatJumpToLatest.tsx`.
- **Why different.** Orca's list is windowed (virtualised rows) and its pill is always mounted and
  fades (`inert` + `aria-hidden` while hidden); onett's list is plain DOM and the pill is mounted
  only when needed. The "N new" count is onett's addition. Orca's pill also swallows `mousedown` so
  focus stays in the composer; onett's does not (see Known gaps).

### P0-2 Code blocks: Adapted

- **onett.** Every fenced block gets a header bar with the language (or "text"), a **Copy** and a
  **Wrap** button (`aria-pressed`). Copy copies the block's raw text; "Copied"/"Copy failed" is
  announced in a polite status region for 1.5s. Wrap is off by default (long lines scroll). Highlighting
  is lazy: blocks show plain first, then a separate highlight.js *core* chunk (12 languages: bash, css,
  diff, go, javascript, json, markdown, python, sql, typescript, xml, yaml) loads and colours them;
  unknown languages stay plain, and a failed load is retried on the next block. The highlighted HTML
  is sanitised again, so only `hljs-*` spans survive.
- **onett files.** `web/src/components/CodeBlock.tsx` + `CodeBlock.css`, `web/src/components/Markdown.tsx`
  (wraps each `<pre>`), `web/src/lib/codeHighlight.ts`, `web/src/lib/hljsCore.ts`; tests in
  `CodeBlock.test.tsx`, `CodeBlockCss.test.ts`, and `e2e/mobile.spec.ts` ("chat code blocks…").
- **Orca files.** `NativeChatCodeBlock.tsx`, `NativeChatHighlightedCode.tsx`.
- **Why different.** Orca uses its own incremental tokenizer with per-token light/dark colours and
  icon-only Wrap/Copy buttons that appear on hover; onett is a browser app that already ships
  highlight.js-style tokens as CSS variables, so a small lazy chunk is cheaper than a tokenizer, and
  text buttons are easier to read and tap. Orca keeps the wrap state in its transcript store so
  streaming cannot reset it; onett keeps it in the header component's own state.

### P0-3 Copy on messages: Adapted

- **onett.** Each owner and assistant message has a copy icon button in its action row (assistant
  messages copy their raw Markdown, owner messages their text; an owner message folded on screen still
  copies in full). It shows a check and "Copied" for 1.5s (polite live region) or "Copy failed". On a
  mouse it appears on hover or focus; on touch it is always visible with a 44px target.
- **onett files.** `web/src/components/CopyButton.tsx`, `CopyButton.css`, `Transcript.tsx`
  (`MessageActions`), `web/src/lib/clipboard.ts` (`copyText`, with an `execCommand` fallback); tests
  `CopyButton.test.tsx`, `e2e/mobile.spec.ts` ("chat message copy buttons…").
- **Orca file.** `NativeChatCopyButton.tsx` (also used by the code block).
- **Why different.** Orca writes through Electron's clipboard IPC and swallows failure; a browser has
  to try `navigator.clipboard`, fall back, and say when it failed.

### P1-4 Tool runs: Adapted

- **onett.** Two or more consecutive tool calls fold into one **tool run** line: a sentence
  ("Ran 3 commands, read 2 files, edited 1 file", names for both Claude and Codex tools), the total
  `+n −m` and file count when files were edited, and an error count. While the run is live the line
  names its latest call ("Running go test ./…") with a blinking lamp and settles into the sentence.
  Open, it lists the **changed files** (path and `+n −m`, summed across calls, Codex path lists split)
  and then each call as its own `aria-expanded` row that opens to its input and result (capped at 16rem).
  A lone call stays a single row. **There are no inline diff cards**: edits show `+n −m` counts only,
  and the changed-file list has no diff body.
- **onett files.** `web/src/components/ToolRun.tsx` + `ToolRun.css`, `web/src/components/toolRuns.ts`
  (grouping, sentence, `changedFiles`, `liveLabel`); tests `toolRuns.test.tsx` (12 cases),
  `e2e/mobile.spec.ts` ("chat tool run…").
- **Orca files.** `NativeChatToolRun.tsx`, `NativeChatToolLine.tsx` (and `NativeChatDiffCard.tsx`,
  `NativeChatDiffView.tsx` for the diff, which onett does not have).
- **Why different.** Same fold-into-a-sentence model. onett's rows keep one name/argument line per call
  and a lamp instead of Orca's tool icons and spinner. Diff cards need the edit's old and new text,
  which onett's run log lines do not carry as structured data; only the `+n −m` counts do.

### P1-5 Composer: Adapted

- **onett.** A bordered sheet with an auto-growing field capped at 40dvh (then it scrolls), Enter to
  send, Shift+Enter for a new line, an IME guard, and one button that is **Send** or, while a turn
  runs and the field is empty, **Stop** (Esc stops too; typing turns it back into Send, which queues
  the message). Attachments (paperclip, paste, drop with an overlay, chips with progress) and a photo
  button on touch. Each conversation keeps its own **draft** in `localStorage` (`onett.chat.draft.<key>`),
  saved 300ms after typing stops, restored on open, cleared after a successful send; storage that
  throws is tolerated. Send/Stop and attach buttons are 44px.
- **onett files.** `web/src/components/Composer.tsx` + `Composer.css`, `ComposerAttachments.tsx`,
  `web/src/lib/useDraft.ts`; tests `Composer.test.tsx`, `useDraft.test.tsx`, `pages/ChatComposer.test.tsx`,
  `pages/ChatAttachments.test.tsx`, `e2e/mobile.spec.ts` (composer cases).
- **Orca files.** `NativeChatComposer.tsx` (a thin wrapper), `NativeChatComposerPane.tsx`,
  `NativeChatComposerField.tsx`, `NativeChatComposerActions.tsx`.
- **Why different.** Orca's composer is a full prompt editor (tabs, model and permission pickers, goal
  chip, notices, rich text, slash and @ autocomplete). Onett talks to one orchestrator or one task
  agent, so it has a plain textarea; those extras are out of scope (see the last section). The field
  outline is `--chat-field-border` (3:1) where Orca uses a faint hairline.

### P1-6 Activity line: Adapted. Reasoning disclosure: Left out

- **onett.** One line at the tail of the transcript. While a turn runs: a lamp and "Working · 1m 12s"
  (the ticking digits are `aria-hidden`, so the live region's spoken text only changes at transitions),
  a "output Ns ago" hint after 30s of silence, and a "view run" link. After it: "Worked for 1m 40s",
  "Stopped after 40s" or "Failed after 1m 02s". It is one polite `role="status"` region.
- **onett files.** `web/src/components/ActivityLine.tsx` + `ActivityLine.css`, used by `pages/Chat.tsx`
  (`TurnActivity`); tests `ActivityLine.test.tsx` (9 cases), `e2e/mobile.spec.ts` ("chat activity line…").
- **Orca file.** `NativeChatTurnActivityLine.tsx`.
- **Why different.** A lamp replaces Orca's spinner (onett identity). Orca shows the *current activity*
  there ("Working…", or what the agent is doing) and keeps the clock in a separate turn bar; onett puts
  the elapsed time in the line itself.
- **Reasoning disclosure: Left out**, because neither harness records reasoning: the Codex parser drops
  `reasoning` items (`internal/harness/codex/parse.go`, `case "reasoning": return nil`) and the Claude
  parser only reads `text` and `tool_use` content (`internal/harness/claude/parse.go`), so run logs
  never carry any. Orca's counterpart is `NativeChatReasoningDisclosure.tsx` / `NativeChatReasoningRow.tsx`.

### P1-7 Per-frame streaming: Adapted

- **onett.** Live run-log events are queued and applied in one batch per animation frame, so a fast
  stream causes one state update and one render per frame, not one per line. Because
  `requestAnimationFrame` does not fire in a hidden tab, a timer also flushes (64ms after a frame was
  requested, 16ms when no frame can be); order is kept, nothing is dropped, and the batch is flushed
  at once when the run ends and dropped when the run changes.
- **onett files.** `web/src/lib/frameBatch.ts`, used by `web/src/runLogs.ts`; tests `frameBatch.test.ts`
  (8 cases).
- **Orca file.** None named for this item. Orca's streaming counterpart is a *paced reveal* of text
  (`NativeChatPacedMarkdown.tsx` with `use-native-chat-paced-text`), which animates words in; onett
  does not animate, it only coalesces updates. Treat this row as "same goal, different mechanism".

### P1-8 Timestamps and long-message fold: Adapted

- **onett.** Each message has a `RelTime` ("5m ago", tabular numerals) in its action row, with the full
  local time in the `title` and in screen-reader-only text. A mouse reveals it on hover or focus of the
  message; touch always shows it. An **owner** message (and a task prompt) longer than 12 lines or
  1200 characters shows about eight lines under a fade with **Show more / Show less**
  (`aria-expanded`, `aria-controls`, 44px on touch). Assistant messages never fold.
- **onett files.** `web/src/components/Transcript.tsx` (`MessageActions`, `FoldedText`, `needsFold`),
  `Transcript.css`, `RelTime.tsx`; tests `Transcript.test.tsx`, `e2e/mobile.spec.ts` ("chat messages:
  sheet width, timestamps and the fold button on touch").
- **Orca files.** `NativeChatMessageTimestamp.tsx`, `NativeChatUserMessageFold.tsx`.
- **Why different.** Orca prints a clock time with the full date in a tooltip and `aria-label`; onett
  keeps its app-wide relative time (`RelTime`, "5m ago") so a thread reads like the rest of the UI.
  Orca measures whether the text really overflows its clip and skips the fold if it fits; onett
  decides from the line and character counts alone, so a few messages just over the limit fold with
  little hidden. Orca's button says "Show full message"; onett's says "Show more".

### P2-9 Tokens and type scale: Adapted

- **onett.** All chat colours, sizes, spacing, radii and the composer shadow are `--chat-*` custom
  properties in `web/src/chat-tokens.css` (light, and dark through `prefers-color-scheme`): type 14 / 12 /
  11px with a 12px code size, a 4px space unit, a 46rem column, radii 2 / 3 / 4px, plus syntax and diff
  tints. The rules and measured contrast ratios live in `docs/design/chat-visual.md`. A guard test
  (`web/src/lib/chatCss.test.ts`) fails if `Chat.css`, `Transcript.css`, `CopyButton.css`, `ToolRun.css`,
  `ActivityLine.css`, `ChatState.css` or `CodeBlock.css` contain a hex/`rgb()`/`hsl()` colour, read a
  variable other than `--chat-*` / `--lamp-*`, or use `--font-cond`; `CodeBlockCss.test.ts` repeats the
  colour check for the code block. #234 moved the sidebar, thread head, notice, pill and attachments
  onto the tokens.
- **onett files.** `web/src/chat-tokens.css`, `docs/design/chat-visual.md`, the guard tests above.
- **Orca files.** `src/renderer/src/assets/main.css` (the `--chat-*` colour mixes) and
  `docs/styleguide/native-chat.md` (the rules).
- **Deliberate adaptations.** A green-grey palette with fixed hex pairs instead of Orca's neutral
  `color-mix` of foreground into canvas (so each pair's contrast can be measured exactly); Barlow
  (Barlow Condensed is not used inside chat); 2–4px radii instead of Orca's 10px base;
  lamps instead of spinners; and the **Terminal view is exempt**: it keeps the andon-board look on the
  app's `--board-*` tokens, cut out of the guard by `terminal-view:start/end` markers. The one rounded
  element is the Jump to latest pill (22px radius), kept round like Orca's.

### P2-10 Empty, error and loading states: Adapted

- **onett.** `ChatState` is a centred column with a 22px square-style line icon (chat, error, select,
  missing), a title, one line of copy and an optional action; the error variant is `role="alert"` and
  the load failure and not-found states carry Retry / Back (44px). `ChatSkeleton` draws placeholder rows
  (a right-aligned user sheet and full-width lines, or list rows) only after 150ms so a fast load never
  flashes; it is `aria-busy` with a screen-reader label from the first render and static under
  reduced motion.
- **onett files.** `web/src/components/ChatState.tsx` + `ChatState.css`, used by `pages/Chat.tsx`;
  tests `ChatState.test.tsx`, `pages/ChatStates.test.tsx`, `e2e/mobile.spec.ts` ("chat error and loading
  states…").
- **Orca files.** `NativeChatEmptyState.tsx` (empty, error, loading, not-agent) and
  `NativeChatLoadingCue.tsx` (spinner after a delay).
- **Why different.** Orca uses a 48px round icon tile and a delayed spinner; onett uses a small square
  icon with no circle and a skeleton, in line with its square corners.

### P2-11 Long-thread performance: Left out

Onett renders the whole thread as plain DOM. `content-visibility: auto` was the obvious cheap
option, but it makes the scroll height change as rows are measured, which fights stick-to-bottom,
and nothing measured shows a need for it. (Orca windows its list, which is a much larger change.)

### P2-12 Find-in-chat and message rail: Left out

Optional. Orca has `NativeChatFindBar.tsx` and `NativeChatMessageRail.tsx`; onett has neither (the
browser's own find works on the page). Slash commands and @mentions
(`NativeChatAutocompleteMenus.tsx`) are out of scope.

## Visual elements from the spec

| Element | Status | What onett does | Orca |
|---|---|---|---|
| User and assistant layout | Adapted | Owner messages are right-aligned sheets (`--chat-user-bg`, max `min(72ch, 85%)`, 4px radius); the assistant is plain Markdown at column width under the agent's name; no avatars. | `NativeChatUserMessageRow.tsx`, `NativeChatAssistantMessageRow.tsx` (a rounded bubble for the owner, plain text for the assistant) |
| Diff cards | **Left out** | Only `+n −m` counts on a call and per changed file in a run. No inline diff body, no line numbers, no hunks. `chat-visual.md` still describes a diff card (see Known gaps). | `NativeChatDiffCard.tsx`, `NativeChatDiffView.tsx` |
| Composer look | Adapted | `--chat-surface` field with a `--chat-field-border` outline and focus ring, hint line, 44px Send/Stop. | `NativeChatComposerPane.tsx` (hairline border) |
| Phone layout | Adapted | Under 760px the gutters shrink to 12px, the sidebar becomes a row of chips, sheets use up to 90% width, the composer clears the home indicator, targets are 44px. Per `chat-visual.md`, Orca's phone chat is a separate React Native app (`mobile/src/session/native-chat/`); onett uses one responsive layout. | `mobile/src/session/native-chat/` |

## Accessibility checklist

| Requirement | How onett meets it | Covered by |
|---|---|---|
| `aria-live` regions | Polite status regions on the copy note (`CopyButton`, `CodeBlockHeader`), the tool-run summary, and the activity line (`role="status"`, digits `aria-hidden`); `ChatState` error is `role="alert"`; the skeleton is `role="status"` + `aria-busy`. | `CopyButton.test.tsx` (aria-live is `polite`), `ActivityLine.test.tsx` ("keeps the live region text constant between ticks", live region attribute), `toolRuns.test.tsx` ("names the latest call while live…"), `ChatState.test.tsx` (`aria-busy`) |
| `aria-expanded` disclosures | Tool run header, each tool row, the message fold button (also `aria-controls`), clamped approval text. `aria-pressed` on Wrap. | `toolRuns.test.tsx` ("toggles aria-expanded…"), `Transcript.test.tsx` ("toggles aria-expanded and the folded class"), `CodeBlock.test.tsx` ("toggles wrap with aria-pressed") |
| Labelled icon buttons | Copy (`aria-label`), Attach / Add a photo, Send / Stop (`aria-label` plus a visible word), code Copy/Wrap (`aria-label` with the language). Icons are `aria-hidden`. | `Composer.test.tsx` (buttons found by name), `CodeBlock.test.tsx`; no test asserts every icon button has a label |
| Focus ring contrast | `outline: 2px solid var(--chat-focus-ring)` on every chat control; `--chat-focus-ring` measured at 10:1 or better on the canvas, card and user sheet in both themes. | **Hand-measured** in `chat-visual.md`; `chatCss.test.ts` only guarantees the CSS reads tokens. No test computes contrast. |
| 44px targets | `min-height/width: 44px` on coarse pointers for copy, code buttons, tool rows, Send/Stop, attach, Jump pill, fold button, error action, chips. | `web/e2e/mobile.spec.ts` (cases named above, `>= 44` assertions on the iPhone project); not re-run for this PR |
| Reduced motion | `prefers-reduced-motion` blocks in `ToolRun.css`, `ActivityLine.css`, `ChatState.css`, `Composer.css` and `Transcript.css` stop the lamp blink, the skeleton pulse and the Send and message-action transitions; Jump to latest scrolls instantly. | `useStickToBottom.test.tsx` ("scrolls instantly when the reader prefers reduced motion"). The CSS blocks have no test. |

## Orca features not covered by this goal

Not in scope, and not planned here: the approvals card (`NativeChatApprovalCard.tsx`; onett has
its own Approvals page), background tasks (`NativeChatBackgroundTasksStatus.tsx`), the context-usage
ring (`NativeChatContextUsageRing.tsx`), slash and @ autocomplete, queued-message cards, rewind, resume
in CLI, subagent sections, model and permission pickers, goal banner, link peeks, inline visuals and
mermaid, selection quote, and text-attachment previews.

## Known gaps found while verifying

Nothing was changed for these; each is for a follow-up.

1. **`chat-visual.md` describes diff cards that do not exist.** Its "Diff cards" section and the
   "Applies to" row ("Diff cards: the edit rows in `Transcript.tsx`") read as if added and removed lines
   are tinted; the code only has `+n −m` counts and the `--chat-diff-*` tokens are used for those.
2. **`chat-visual.md` promises "Waiting for you" and "Blocked" activity labels** (warning and danger
   lamps). `ActivityLine.tsx` only produces Working, Worked for, Stopped after and Failed after.
3. **Stale wording in two places.** `web/src/chat-tokens.css` says "Nothing reads them yet" and
   `chat-visual.md` says "This PR changes no visuals: nothing reads the tokens yet"; both predate
   the restyle PRs.
4. **The token guard does not cover `Composer.css`** (nor `ComposerAttachments`/`MessageAttachments`
   styles), so hard-coded colours there would pass `chatCss.test.ts`. A scan of `Composer.css` at this
   commit found none.
5. **Contrast is not tested.** The ratios in `chat-visual.md` are a one-time hand measurement; changing
   a token value is not caught by any test.
6. **Jump to latest takes focus on a pointer press.** Orca prevents `mousedown` on its pill so focus
   stays in the composer; onett's `JumpToLatest` has no such handler. (Read from the code; not
   reproduced in a browser.)
7. **Fold decides by counts only**, so a 13-line message of short lines folds with almost nothing
   hidden (Orca measures first). Minor.

## Gallery

Screenshots come from `npm run shots:chat` (`docs/ui.md`, "Screenshots for UI PRs"): six scenes in four
variants (desktop 1280x800 and iPhone 390x844, light and dark), clock frozen, animations off, published
with `scripts/pr-shots.sh` to `pr-assets/onett/49553634-chat-ui-final-onett-vs-orca-comparison-d/<label>`.
The page is scrolled to the bottom before each shot, which is why a tall desktop thread shows the page
scrolled under the top bar.

**Orca reference for each scene** (images are hosted by Orca; see `chat-visual.md`): the hero
`docs/assets/readme-hero.jpg` and the mobile showcase `docs/assets/feature-wall/mobile-companion-app-showcase.gif`
for the overall look.

| Scene | Orca component it follows |
|---|---|
| `chat-thread` | `NativeChatUserMessageRow`, `NativeChatAssistantMessageRow`, `NativeChatToolRun`, `NativeChatCodeBlock`, `NativeChatComposerPane` |
| `chat-running` | `NativeChatTurnActivityLine`, live `NativeChatToolRun` |
| `task-thread` | same as `chat-thread` (Orca has no task-agent view) |
| `chat-empty` | `NativeChatEmptyState` (empty) + `NativeChatComposerPane` |
| `chat-error` | `NativeChatEmptyState` (error) |
| `chat-loading` | `NativeChatLoadingCue` / `NativeChatEmptyState` (loading) |

The fixture thread has three tool calls (grep, read, test) and no edit, so the shots show the tool-run
sentence but not the changed-files list or a `+n −m` total. Those are covered by `toolRuns.test.tsx`.
The mock has no long owner message either, so the fold is not in a shot.

### Overall: before (#199's parent) and after (base head)

`before` is `07d1114`, the parent of #199 "Stick-to-bottom chat scrolling". `shots:chat` did not exist
there (it arrived in #221), so I ran the current `playwright.shots.config.ts` and
`e2e/chat-shots.spec.ts` in a temporary worktree of that commit. Four scenes rendered (`chat-thread`,
`chat-running`, `task-thread`, `chat-empty`). **`chat-error` and `chat-loading` could not render** there:
`ChatState` and `ChatSkeleton` did not exist yet (#229), so those eight tests timed out waiting for
`.chat-state` / `.chat-skel` and have no before image. Port 4174 was in use, so I ran with a throwaway
config copy on port 4181 that is not committed.

**Before (`07d1114`)**

| scene | desktop light | desktop dark | iPhone light | iPhone dark |
|---|---|---|---|---|
| chat-empty | ![chat-empty desktop-light](https://raw.githubusercontent.com/bobbyrc/onett/71ebad3d3a3c5f24c94a9034298a1200df83db47/chat-empty-desktop-light.png) | ![chat-empty desktop-dark](https://raw.githubusercontent.com/bobbyrc/onett/71ebad3d3a3c5f24c94a9034298a1200df83db47/chat-empty-desktop-dark.png) | ![chat-empty iphone-light](https://raw.githubusercontent.com/bobbyrc/onett/71ebad3d3a3c5f24c94a9034298a1200df83db47/chat-empty-iphone-light.png) | ![chat-empty iphone-dark](https://raw.githubusercontent.com/bobbyrc/onett/71ebad3d3a3c5f24c94a9034298a1200df83db47/chat-empty-iphone-dark.png) |
| chat-running | ![chat-running desktop-light](https://raw.githubusercontent.com/bobbyrc/onett/71ebad3d3a3c5f24c94a9034298a1200df83db47/chat-running-desktop-light.png) | ![chat-running desktop-dark](https://raw.githubusercontent.com/bobbyrc/onett/71ebad3d3a3c5f24c94a9034298a1200df83db47/chat-running-desktop-dark.png) | ![chat-running iphone-light](https://raw.githubusercontent.com/bobbyrc/onett/71ebad3d3a3c5f24c94a9034298a1200df83db47/chat-running-iphone-light.png) | ![chat-running iphone-dark](https://raw.githubusercontent.com/bobbyrc/onett/71ebad3d3a3c5f24c94a9034298a1200df83db47/chat-running-iphone-dark.png) |
| chat-thread | ![chat-thread desktop-light](https://raw.githubusercontent.com/bobbyrc/onett/71ebad3d3a3c5f24c94a9034298a1200df83db47/chat-thread-desktop-light.png) | ![chat-thread desktop-dark](https://raw.githubusercontent.com/bobbyrc/onett/71ebad3d3a3c5f24c94a9034298a1200df83db47/chat-thread-desktop-dark.png) | ![chat-thread iphone-light](https://raw.githubusercontent.com/bobbyrc/onett/71ebad3d3a3c5f24c94a9034298a1200df83db47/chat-thread-iphone-light.png) | ![chat-thread iphone-dark](https://raw.githubusercontent.com/bobbyrc/onett/71ebad3d3a3c5f24c94a9034298a1200df83db47/chat-thread-iphone-dark.png) |
| task-thread | ![task-thread desktop-light](https://raw.githubusercontent.com/bobbyrc/onett/71ebad3d3a3c5f24c94a9034298a1200df83db47/task-thread-desktop-light.png) | ![task-thread desktop-dark](https://raw.githubusercontent.com/bobbyrc/onett/71ebad3d3a3c5f24c94a9034298a1200df83db47/task-thread-desktop-dark.png) | ![task-thread iphone-light](https://raw.githubusercontent.com/bobbyrc/onett/71ebad3d3a3c5f24c94a9034298a1200df83db47/task-thread-iphone-light.png) | ![task-thread iphone-dark](https://raw.githubusercontent.com/bobbyrc/onett/71ebad3d3a3c5f24c94a9034298a1200df83db47/task-thread-iphone-dark.png) |

**After (`9232627`, this PR's base head)**

| scene | desktop light | desktop dark | iPhone light | iPhone dark |
|---|---|---|---|---|
| chat-empty | ![chat-empty desktop-light](https://raw.githubusercontent.com/bobbyrc/onett/f90490adc7c691d3b4f92701400843bfd38ac2fd/chat-empty-desktop-light.png) | ![chat-empty desktop-dark](https://raw.githubusercontent.com/bobbyrc/onett/f90490adc7c691d3b4f92701400843bfd38ac2fd/chat-empty-desktop-dark.png) | ![chat-empty iphone-light](https://raw.githubusercontent.com/bobbyrc/onett/f90490adc7c691d3b4f92701400843bfd38ac2fd/chat-empty-iphone-light.png) | ![chat-empty iphone-dark](https://raw.githubusercontent.com/bobbyrc/onett/f90490adc7c691d3b4f92701400843bfd38ac2fd/chat-empty-iphone-dark.png) |
| chat-error | ![chat-error desktop-light](https://raw.githubusercontent.com/bobbyrc/onett/f90490adc7c691d3b4f92701400843bfd38ac2fd/chat-error-desktop-light.png) | ![chat-error desktop-dark](https://raw.githubusercontent.com/bobbyrc/onett/f90490adc7c691d3b4f92701400843bfd38ac2fd/chat-error-desktop-dark.png) | ![chat-error iphone-light](https://raw.githubusercontent.com/bobbyrc/onett/f90490adc7c691d3b4f92701400843bfd38ac2fd/chat-error-iphone-light.png) | ![chat-error iphone-dark](https://raw.githubusercontent.com/bobbyrc/onett/f90490adc7c691d3b4f92701400843bfd38ac2fd/chat-error-iphone-dark.png) |
| chat-loading | ![chat-loading desktop-light](https://raw.githubusercontent.com/bobbyrc/onett/f90490adc7c691d3b4f92701400843bfd38ac2fd/chat-loading-desktop-light.png) | ![chat-loading desktop-dark](https://raw.githubusercontent.com/bobbyrc/onett/f90490adc7c691d3b4f92701400843bfd38ac2fd/chat-loading-desktop-dark.png) | ![chat-loading iphone-light](https://raw.githubusercontent.com/bobbyrc/onett/f90490adc7c691d3b4f92701400843bfd38ac2fd/chat-loading-iphone-light.png) | ![chat-loading iphone-dark](https://raw.githubusercontent.com/bobbyrc/onett/f90490adc7c691d3b4f92701400843bfd38ac2fd/chat-loading-iphone-dark.png) |
| chat-running | ![chat-running desktop-light](https://raw.githubusercontent.com/bobbyrc/onett/f90490adc7c691d3b4f92701400843bfd38ac2fd/chat-running-desktop-light.png) | ![chat-running desktop-dark](https://raw.githubusercontent.com/bobbyrc/onett/f90490adc7c691d3b4f92701400843bfd38ac2fd/chat-running-desktop-dark.png) | ![chat-running iphone-light](https://raw.githubusercontent.com/bobbyrc/onett/f90490adc7c691d3b4f92701400843bfd38ac2fd/chat-running-iphone-light.png) | ![chat-running iphone-dark](https://raw.githubusercontent.com/bobbyrc/onett/f90490adc7c691d3b4f92701400843bfd38ac2fd/chat-running-iphone-dark.png) |
| chat-thread | ![chat-thread desktop-light](https://raw.githubusercontent.com/bobbyrc/onett/f90490adc7c691d3b4f92701400843bfd38ac2fd/chat-thread-desktop-light.png) | ![chat-thread desktop-dark](https://raw.githubusercontent.com/bobbyrc/onett/f90490adc7c691d3b4f92701400843bfd38ac2fd/chat-thread-desktop-dark.png) | ![chat-thread iphone-light](https://raw.githubusercontent.com/bobbyrc/onett/f90490adc7c691d3b4f92701400843bfd38ac2fd/chat-thread-iphone-light.png) | ![chat-thread iphone-dark](https://raw.githubusercontent.com/bobbyrc/onett/f90490adc7c691d3b4f92701400843bfd38ac2fd/chat-thread-iphone-dark.png) |
| task-thread | ![task-thread desktop-light](https://raw.githubusercontent.com/bobbyrc/onett/f90490adc7c691d3b4f92701400843bfd38ac2fd/task-thread-desktop-light.png) | ![task-thread desktop-dark](https://raw.githubusercontent.com/bobbyrc/onett/f90490adc7c691d3b4f92701400843bfd38ac2fd/task-thread-desktop-dark.png) | ![task-thread iphone-light](https://raw.githubusercontent.com/bobbyrc/onett/f90490adc7c691d3b4f92701400843bfd38ac2fd/task-thread-iphone-light.png) | ![task-thread iphone-dark](https://raw.githubusercontent.com/bobbyrc/onett/f90490adc7c691d3b4f92701400843bfd38ac2fd/task-thread-iphone-dark.png) |

### Token pass alone: #234 before and after

`before` is `ebc3199`, the parent of #234 "Finish --chat-* token pass on chat sidebar, thread head, notice,
pill, attachments"; `after` is its merge commit `9232627`, which is the same code as the overall *after*
above, so the same images are used. #234 shipped without screenshots. All 24 scenes rendered at
`ebc3199`. #234 swapped literal colours and sizes for `--chat-*` tokens with the same values, so the
two sets look the same to the eye (I compared the desktop and iPhone `chat-thread` light pairs side by
side; the files are not byte-identical, but I found no visible difference). The Jump pill, queued
notice and attachment chips it also touched appear in none of the six scenes.

**Before (`ebc3199`)**

| scene | desktop light | desktop dark | iPhone light | iPhone dark |
|---|---|---|---|---|
| chat-empty | ![chat-empty desktop-light](https://raw.githubusercontent.com/bobbyrc/onett/aa60f314889f530c5d0a1cd119f303a68ba6e80d/chat-empty-desktop-light.png) | ![chat-empty desktop-dark](https://raw.githubusercontent.com/bobbyrc/onett/aa60f314889f530c5d0a1cd119f303a68ba6e80d/chat-empty-desktop-dark.png) | ![chat-empty iphone-light](https://raw.githubusercontent.com/bobbyrc/onett/aa60f314889f530c5d0a1cd119f303a68ba6e80d/chat-empty-iphone-light.png) | ![chat-empty iphone-dark](https://raw.githubusercontent.com/bobbyrc/onett/aa60f314889f530c5d0a1cd119f303a68ba6e80d/chat-empty-iphone-dark.png) |
| chat-error | ![chat-error desktop-light](https://raw.githubusercontent.com/bobbyrc/onett/aa60f314889f530c5d0a1cd119f303a68ba6e80d/chat-error-desktop-light.png) | ![chat-error desktop-dark](https://raw.githubusercontent.com/bobbyrc/onett/aa60f314889f530c5d0a1cd119f303a68ba6e80d/chat-error-desktop-dark.png) | ![chat-error iphone-light](https://raw.githubusercontent.com/bobbyrc/onett/aa60f314889f530c5d0a1cd119f303a68ba6e80d/chat-error-iphone-light.png) | ![chat-error iphone-dark](https://raw.githubusercontent.com/bobbyrc/onett/aa60f314889f530c5d0a1cd119f303a68ba6e80d/chat-error-iphone-dark.png) |
| chat-loading | ![chat-loading desktop-light](https://raw.githubusercontent.com/bobbyrc/onett/aa60f314889f530c5d0a1cd119f303a68ba6e80d/chat-loading-desktop-light.png) | ![chat-loading desktop-dark](https://raw.githubusercontent.com/bobbyrc/onett/aa60f314889f530c5d0a1cd119f303a68ba6e80d/chat-loading-desktop-dark.png) | ![chat-loading iphone-light](https://raw.githubusercontent.com/bobbyrc/onett/aa60f314889f530c5d0a1cd119f303a68ba6e80d/chat-loading-iphone-light.png) | ![chat-loading iphone-dark](https://raw.githubusercontent.com/bobbyrc/onett/aa60f314889f530c5d0a1cd119f303a68ba6e80d/chat-loading-iphone-dark.png) |
| chat-running | ![chat-running desktop-light](https://raw.githubusercontent.com/bobbyrc/onett/aa60f314889f530c5d0a1cd119f303a68ba6e80d/chat-running-desktop-light.png) | ![chat-running desktop-dark](https://raw.githubusercontent.com/bobbyrc/onett/aa60f314889f530c5d0a1cd119f303a68ba6e80d/chat-running-desktop-dark.png) | ![chat-running iphone-light](https://raw.githubusercontent.com/bobbyrc/onett/aa60f314889f530c5d0a1cd119f303a68ba6e80d/chat-running-iphone-light.png) | ![chat-running iphone-dark](https://raw.githubusercontent.com/bobbyrc/onett/aa60f314889f530c5d0a1cd119f303a68ba6e80d/chat-running-iphone-dark.png) |
| chat-thread | ![chat-thread desktop-light](https://raw.githubusercontent.com/bobbyrc/onett/aa60f314889f530c5d0a1cd119f303a68ba6e80d/chat-thread-desktop-light.png) | ![chat-thread desktop-dark](https://raw.githubusercontent.com/bobbyrc/onett/aa60f314889f530c5d0a1cd119f303a68ba6e80d/chat-thread-desktop-dark.png) | ![chat-thread iphone-light](https://raw.githubusercontent.com/bobbyrc/onett/aa60f314889f530c5d0a1cd119f303a68ba6e80d/chat-thread-iphone-light.png) | ![chat-thread iphone-dark](https://raw.githubusercontent.com/bobbyrc/onett/aa60f314889f530c5d0a1cd119f303a68ba6e80d/chat-thread-iphone-dark.png) |
| task-thread | ![task-thread desktop-light](https://raw.githubusercontent.com/bobbyrc/onett/aa60f314889f530c5d0a1cd119f303a68ba6e80d/task-thread-desktop-light.png) | ![task-thread desktop-dark](https://raw.githubusercontent.com/bobbyrc/onett/aa60f314889f530c5d0a1cd119f303a68ba6e80d/task-thread-desktop-dark.png) | ![task-thread iphone-light](https://raw.githubusercontent.com/bobbyrc/onett/aa60f314889f530c5d0a1cd119f303a68ba6e80d/task-thread-iphone-light.png) | ![task-thread iphone-dark](https://raw.githubusercontent.com/bobbyrc/onett/aa60f314889f530c5d0a1cd119f303a68ba6e80d/task-thread-iphone-dark.png) |

**After:** the table under "Overall: after" above (`9232627`).
