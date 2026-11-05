# Chat visual design

How onett's web chat should look: the orchestrator chat at `/chat` and the task-agent threads at
`/chat/task/:id`. The reference is the native chat in [Orca](https://github.com/stablyai/orca)
(MIT, Copyright 2026 Lovecast Inc.). We borrow its **layout, rhythm, hierarchy and calm density**.
We do not borrow its logo, marks, illustrations or name.

**Chat takes Orca's shape, type and layout: its font (Geist), radius scale, spacing and type scale
and colour-mix percentages. Colours and the andon status lamps are onett's** (the green/grey
palette from `styles.css`, the lamp squares with text labels). The rest of the app keeps Barlow,
square corners and 1px rules (`docs/ui.md`, "Tokens"). Where a section below still describes
square corners or Barlow in chat, it predates the token change and is rewritten with the component
restyling PRs; the token tables here are current. Every place where chat departs from Orca to stay
onett is marked **Adaptation**.

The tokens in `web/src/chat-tokens.css` are the contract. The chat components read them through
`var(--chat-*)`; new chat styles must not invent colours or sizes, and must not rename a token.
`web/src/lib/chatCss.test.ts` fails on a hex colour or a non-chat variable in a chat stylesheet,
and `web/src/lib/chatContrast.test.ts` recomputes the contrast table under "Accessibility".

How each item compares with Orca once built, with before/after screenshots: [chat-orca-comparison.md](chat-orca-comparison.md).

Orca files cited below are under `src/renderer/src/components/native-chat/` unless a full path is
given; styles are `src/renderer/src/assets/main.css`, rules are `docs/styleguide/native-chat.md`.

## Reference images

All hosted by Orca; none is committed here.

- Hero, desktop with the mobile companion: <https://raw.githubusercontent.com/stablyai/orca/main/docs/assets/readme-hero.jpg>
- Mobile companion showcase: <https://raw.githubusercontent.com/stablyai/orca/main/docs/assets/feature-wall/mobile-companion-app-showcase.gif>
- Site card: <https://www.onorca.dev/og-image.png>

Every visual PR attaches `pr-shots.sh` before/after tables, next to the Orca reference (see "Screenshots for UI PRs" in `docs/ui.md`).

## Principles (from Orca's `native-chat.md`)

1. **One reading column.** Transcript and composer share one maximum width (`--chat-content-max-width: 46rem` in Orca).
2. **Prose first, machinery second.** Assistant text is plain on the canvas; tool calls are one quiet line each, expandable.
3. **Three text tones, no more:** strong (titles, the user's words), normal (prose), faint (labels, timestamps, tool names).
4. **Few boxes.** Only the user's message, code and diffs, and the composer get a surface. Hairlines, not shadows.
5. **A small fixed type scale** (14 / 12 / 11 / code 12) and a 4px spacing unit.

## Colour

Orca derives every chat colour by `color-mix`ing its foreground into its canvas (`main.css`, the
`--chat-*-mix` variables): user surface 4% in light and 7% in dark, hairlines 7%, code surface 2%.
The result is near-monochrome neutral.

**Adaptation.** Onett's palette stays: the chat colour tokens are `var()` references to the app
tokens (`--bg`, `--panel`, `--text`, `--muted`, `--border`, `--field`, `--focus`, `--ok`, `--warn`,
`--danger` in `styles.css`), so chat is a green-tinted grey, not Orca's neutral grey, dark mode is
onett's green-black, and each pair can still be measured exactly (the contrast test resolves the
references). The derived surfaces use Orca's `color-mix` form and percentages with onett's `--text`
over onett's `--bg`: user surface 4% (7% dark), user border 7% (6%), code block 2% (3.5%), inline
code 4% (6%), inline code border 9% (8%), composer border 11% (9%). Borders mix toward
`transparent`, as Orca's do; Orca's dark block mixes toward `transparent` for code and inline
code too, which here is `--bg` so the fill stays opaque and measurable. Text colours are not
mixed: `--chat-fg` stays onett's own value, the strong and faint tones are `--text` and `--muted`.
Orca's 82% / 92% text mixes are not used. The canvas is the page ground
(`--chat-bg` = `--bg`); cards sit on the lighter `--panel` (`--chat-surface`). Status text uses
onett's readable status colours (`--run`, `--warn`, `--danger`), not raw lamp fills. Light is the
default and dark follows `prefers-color-scheme`; there is no manual theme switch.

## Type scale and font stacks

Orca: `text-sm` 14px for everything the user reads, `text-xs` 12px for labels and timestamps,
`text-2xs` 11px for captions, `text-chat-code` 12px for code and tool output; sans for UI, mono for
code (`native-chat.md`, "Chat type scale").

Same four steps, in Orca's font stacks: `--chat-font` is `'Geist', -apple-system,
BlinkMacSystemFont, 'Segoe UI', sans-serif` and `--chat-font-mono` is `'SF Mono', SFMono-Regular,
ui-monospace, Menlo, monospace`. Geist (SIL OFL 1.1, from the npm `geist` package) is self-hosted
at weights 400, 500 and 600 in `web/src/assets/fonts` (licence: `OFL-Geist.txt`) with
`font-display: swap`; chat no longer inherits Barlow, and the rest of the app is unchanged. Line
height is 1.625 (Tailwind `leading-relaxed`, which Orca uses for prose). Headings inside assistant
Markdown stay the same size as prose and are told apart by weight (600), as Orca does.
`Barlow Condensed` is not used in chat.

## Spacing rhythm

Orca ties its unit to the text size (4px at 14px): rows are 24–26px high (`min-h-6`, `min-h-6.5`),
blocks have 12px of vertical margin (`my-3` on code), message cells are 14px (`px-3.5`) by 10px
(`py-2.5`) and messages are separated by a few rem.

The unit is the same: `--chat-space-1..6` are 4, 8, 12, 16, 24, 32px. `--chat-msg-gap` is 1.25rem
(20px, Orca's `gap-5` between transcript rows); tool rows stay about 26px tall.
`--chat-max-width` is 46rem (736px) against 760px now; the 24px difference is not worth a media query.

## Radii, borders and shadows

Orca: radius 10px base; user bubble `rounded-xl`, code/diff/tool output `rounded-lg`, controls
`rounded-md`; borders are 1px hairlines at 6–11% foreground; no shadow on messages.

Chat takes the scale: `--chat-radius` is 0.625rem (10px) and `-sm`, `-md`, `-lg`, `-xl` are 0.6,
0.8, 1 and 1.4 times it (6, 8, 10 and 14px), so a component picks the same step Orca does.
Components are restyled onto the scale in later PRs; until then one that reads a `--chat-radius-*`
token gets the larger corner. Borders are 1px hairlines. Orca's composer has no shadow, so
`--chat-shadow-composer` is `none` (the token stays so the two rules that read it keep working).

## User and assistant messages

Orca (`NativeChatUserMessageRow.tsx`, `NativeChatAssistantMessageRow.tsx`): the user's message is a
**bubble** on the right, `max-w-[80%]`, `rounded-xl border border-chat-user-border bg-chat-user-surface px-3.5 py-2.5`,
`text-sm` in the strong tone, with its timestamp and actions revealed on hover below it. The
assistant has **no bubble**: full column width, `text-sm leading-relaxed` in the normal tone, text
selectable, with attachments and a turn summary after it.

**Adaptation.** Same structure. The user message is a right-aligned sheet at `--chat-user-bg`,
`--chat-user-fg`, `--chat-radius-lg`, 1px `--chat-border`, padding `--chat-space-3` by
`--chat-space-4`, width capped at `min(72ch, 85%)`. The assistant is plain Markdown on
`--chat-bg` at full column width, introduced by the agent's name as a `--chat-text-sm` faint
label (kept from the current UI, which Orca lacks). Neither side shows an avatar.

## Tool runs and tool lines

Orca (`NativeChatToolRun.tsx`, `NativeChatToolLine.tsx`): a run of tool calls collapses to one
line, 24–26px high, with a 12px icon, the verb in the faint tone (it brightens on hover), the main
argument truncated, a chevron, and a pulsing label while running. Opening it shows each call
indented 24px (`ml-6`), output in a `rounded-lg` bordered code surface capped at `max-h-64`.

**Adaptation.** Keep onett's one-row-per-call (name, main argument, `+n −m` for edits) and
fold consecutive tool calls into one run when there are **two or more** of them; a lone call stays a single tool line. The run header is a plain sentence ("Ran 3 commands, read 2 files, edited 1 file"), shows the latest call while the run is going, and carries a total `+n −m` when the run edits files. Status stays a lamp square with a text
label, never colour alone (`docs/ui.md`); the row uses `--chat-fg-faint` and `--chat-text-md`.
Expanded output is `--chat-code-bg`, `--chat-text-code`, `--chat-radius-md`, max 16rem tall. The
running pulse is the existing two-step lamp switch, not Orca's fade, and it stops under
`prefers-reduced-motion`.

## Code blocks

Orca (`NativeChatCodeBlock.tsx`): `my-3 rounded-lg border border-chat-code-border bg-chat-code-surface`, a 30px header with the
language in the faint tone, mono 12px, `px-3.5 pb-3`, and a copy button that appears on hover or
keyboard focus and always shows on touch.

**Adaptation.** `--chat-code-bg` background, 1px `--chat-border`, `--chat-radius-md`, header row
with the language in `--chat-text-sm` faint. Text is `--chat-fg` in `--chat-font-mono` at
`--chat-text-code`, padded `--chat-space-3`, with `--chat-space-3` of margin above and below. The
header holds the language label (`--chat-text-sm`, faint) and two quiet buttons, **Copy** and
**Wrap**, in the faint tone; they brighten to `--chat-fg-strong` on hover and keyboard focus and keep
a 44px target on coarse pointers. Wrap is off by default, so long lines scroll horizontally; turning
it on wraps them (the button shows `aria-pressed` and an underline, not colour alone). Code is
highlighted lazily: it shows plain first, and colours arrive once the highlighter chunk has loaded
(unknown languages stay plain). Token colours are the `--chat-syntax-*` tokens below, each measured
against `--chat-code-bg`. Inline code uses `--chat-code-bg` with no border and a `--chat-radius-sm` corner.

## Diff counts

Onett has no inline diff cards. Orca (`NativeChatDiffCard.tsx`) shows a bordered code surface with
line numbers and tinted added and removed lines; that needs each edit's old and new text, which the
run log does not carry. Onett shows only the `+n −m` counts, on a tool call and per changed file in a
run's summary (`.tl-diff` in `ToolRun.css`). If a diff body is added later, this is where it is specified.

**Adaptation.** The added count uses `--chat-diff-add-fg` and the removed count `--chat-diff-del-fg`.
The `+` and `−` characters stay, so colour is never the only cue. The `-bg` tokens are reserved for a
future diff body and nothing reads them yet.

## Composer

Orca (`NativeChatComposerPane.tsx`, `NativeChatComposerField.tsx`): a rounded field in the same
column width as the transcript, a hairline border (`--chat-composer-border`, 9–11%), no fill
change from the canvas, with attachment chips above, an action row below and notices docked above.

**Adaptation.** The composer stays sticky at the bottom, at `--chat-max-width`. Field fill is
`--chat-surface`, outline 1px `--chat-composer-border` (Orca's hairline; the 2px focus ring is what
marks the field while you type), radius `--chat-radius-xl`, `--chat-shadow-composer` on the sticky wrapper. Send and
Attach stay square-cornered buttons. On focus the outline becomes the `--chat-focus-ring`.

## Status and activity line

Orca (`NativeChatTurnActivityLine.tsx`): one line under the transcript, `min-h-6`, faint text with
a spinner or shimmer, naming what the agent is doing now; the verb is normal tone, the rest faint.

**Adaptation.** One line at the tail of the transcript: a lamp (`--chat-accent` when working,
`--chat-warning` when waiting for the owner, `--chat-danger` when stopped), then a text label
("Working", "Waiting for you", "Blocked") in `--chat-text-md`. While a turn runs the label also
shows elapsed time ("Working · 1m 12s"); once it finishes the line reads "Worked for 1m 40s".
"Waiting for you" and "Blocked" carry no timer. The lamp switches in two steps like
every onett lamp. No spinner. Errors are one line in `--chat-danger`, as Orca's `text-destructive/80`
row, but at full strength to pass 4.5:1.

Orca's reasoning disclosure is deferred until the harnesses record reasoning: the Codex parser drops
`reasoning` items and the Claude parser keeps no thinking, so run logs never carry it.

## Timestamps

Orca (`NativeChatMessageTimestamp.tsx`): `text-xs`, faint, tabular numerals, no wrap, revealed on
hover or focus of the message and always visible on touch.

**Adaptation.** `RelTime` in `--chat-text-sm` faint with `font-variant-numeric: tabular-nums`,
visible on touch, on focus and on hover on desktop. The full time stays in the `title` attribute.

## Empty state

Orca (`NativeChatEmptyState.tsx`): centred column, a 48px round icon, a 14px medium-weight title
in the strong tone and a `max-w-sm` 12px faint explanation.

**Adaptation.** Centred under the top of the thread (as the current new-chat page is), a title in
`--chat-fg-strong` at `--chat-text-md` weight 600, one `--chat-fg-faint` line of explanation and
the example prompts as quiet rounded chips. A small line icon (22px, `currentColor`), no
circle, no illustration. Loading shows a skeleton (a right-aligned user sheet and full-width
assistant lines) only after 150ms, static under `prefers-reduced-motion`.

## Phone

Orca's phone chat (`mobile/src/session/native-chat/`) is the same design in React Native: the same
three tones, bubbles right-aligned, assistant text full-width, composer docked above the keyboard,
floating "jump to latest" control. Onett's phone layout (`< 760px`) follows suit: the column loses
its side gutters to 12px, user sheets may use up to 90% of the width, the composer clears the home
indicator (already covered by `web/e2e/mobile.spec.ts`), and tap targets stay at least 44px.

## Tokens

All defined in `web/src/chat-tokens.css` on `:root`; dark values sit in `@media (prefers-color-scheme: dark)`.
Names marked * are additions to the frozen list from the brief.
Colours written `var(--x)` are references to `styles.css`, not copies; the hex beside them is what they resolve to today.

| Token | Light | Dark | Purpose |
|---|---|---|---|
| `--chat-bg` | `#e3eae4` | `#111614` | chat canvas (= `var(--bg)`) |
| `--chat-surface` | `#f5f8f5` | `#181e1b` | assistant/tool cards, composer field (= `var(--panel)`) |
| `--chat-user-bg` | `#dbe2dc` | `#202523` | user message sheet: `--text` 4% (dark 7%) over `--bg` |
| `--chat-user-fg` | `#18201c` | `#e4ebe6` | user message text (= `var(--text)`) |
| `--chat-user-border`* | `--text` 7% | `--text` 6% | user message edge, mixed toward transparent |
| `--chat-fg` | `#2a342e` | `#cfd8d2` | assistant prose |
| `--chat-fg-strong` | `#18201c` | `#e4ebe6` | titles, emphasis (= `var(--text)`) |
| `--chat-fg-faint` | `#4b5751` | `#a1aea7` | labels, tool names, timestamps (= `var(--muted)`) |
| `--chat-border` | `#b9c6bd` | `#3a4741` | decorative 1px rules and card edges (= `var(--border)`) |
| `--chat-field-border`* | `#6f7d76` | `#64736b` | outline of the composer input (= `var(--field)`) |
| `--chat-composer-border`* | `--text` 11% | `--text` 9% | Orca's composer hairline, mixed toward transparent; the composer card's border |
| `--chat-accent` | `#1d6f3d` | `#5fd08b` | working lamp, links, selection (= `var(--ok)`) |
| `--chat-warning` | `#7a5800` | `#f2c230` | waiting-for-owner text (= `var(--warn)`) |
| `--chat-warning-bg`* | `#fbf3d4` | `#2b2308` | queued-message notice fill (text is `--chat-warning`) |
| `--chat-danger` | `#b0251c` | `#ff7a70` | errors, blocked (= `var(--danger)`) |
| `--chat-diff-add-fg` / `-bg` | `#1d6f3d` / `#d3e8d9` | `#7fdca1` / `#16301f` | added lines |
| `--chat-diff-del-fg` / `-bg` | `#a3231a` / `#f1d6d2` | `#ff9a92` / `#3a1a17` | removed lines |
| `--chat-code-bg` | `#dfe6e0` | `#181d1b` | code blocks, tool output: `--text` 2% (dark 3.5%) over `--bg` |
| `--chat-inline-code-bg`* | `#dbe2dc` | `#1e2321` | inline code: `--text` 4% (dark 6%) over `--bg` |
| `--chat-inline-code-border`* | `--text` 9% | `--text` 8% | inline code edge, mixed toward transparent |
| `--chat-syntax-keyword` | `#a3236b` | `#ff9ac8` | keywords, built-ins, literals |
| `--chat-syntax-string` | `#0b6b35` | `#8fd6a8` | strings, regular expressions |
| `--chat-syntax-comment` | `#56625b` | `#94a39a` | comments, quotes |
| `--chat-syntax-number` | `#8a4b00` | `#f2c230` | numbers, symbols, variables |
| `--chat-syntax-function` | `#1d4fa3` | `#8fbaff` | function and section titles |
| `--chat-syntax-type` | `#0b6674` | `#6fd3e0` | types and class names |
| `--chat-syntax-meta` | `#6a3fa0` | `#c9a6ff` | attributes, tags, selectors, meta lines |
| `--chat-focus-ring` | `#18201c` | `#f2c230` | focus outline (= `var(--focus)`) |
| `--chat-font` | `'Geist', -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif` | same | chat sans (Orca's stack; Geist self-hosted) |
| `--chat-font-mono` | `'SF Mono', SFMono-Regular, ui-monospace, Menlo, monospace` | same | chat mono (Orca's stack) |
| `--chat-text-md` / `-sm` / `-xs` | 14 / 12 / 11px | same | body / labels / captions |
| `--chat-text-code` | 12px | same | code, diffs, tool output |
| `--chat-leading` | 1.625 | same | line height (`leading-relaxed`) |
| `--chat-space-1` … `-6` | 4, 8, 12, 16, 24, 32px | same | spacing scale |
| `--chat-msg-gap` | 1.25rem (20px) | same | between messages (Orca `gap-5`) |
| `--chat-max-width` | 46rem | same | reading column cap |
| `--chat-radius` | 0.625rem | same | base of the radius scale |
| `--chat-radius-sm` / `-md` / `-lg` / `-xl` | ×0.6 / ×0.8 / ×1 / ×1.4 (6 / 8 / 10 / 14px) | same | controls / cards / user sheet / large panels |
| `--chat-shadow-composer` | `none` | same | Orca's composer has no shadow |

## Applies to

| Section | Restyles |
|---|---|
| Messages, tool rows, status line, timestamps | `web/src/components/Transcript.css` (+ `Transcript.tsx`) |
| Page layout, conversation list, composer, empty state | `web/src/pages/Chat.css` |
| Code blocks, inline code, Markdown headings | `web/src/components/Markdown.tsx` and its styles |
| Diff counts (`+n −m`) | `web/src/components/ToolRun.css` (`.tl-diff`) |
| Not covered | The Terminal view (`TerminalView`, `TermLine`, `TerminalTool` rules in `Transcript.css`) is exempt by design: it keeps its andon-board look on the app's `--board-*` tokens. |
| Dark mode | the dark block of `Chat.css` folds into `chat-tokens.css` as each rule moves |

## Accessibility

Targets: text at least 4.5:1, large text and UI boundaries at least 3:1, focus ring at least 3:1
on both backgrounds. Measured with the WCAG 2.x relative-luminance formula on the resolved values above
(light / dark; the test resolves `var()` and `color-mix` with a plain sRGB mix):

| Pair | Light | Dark |
|---|---|---|
| `--chat-fg` on `--chat-bg` | 10.53 | 12.54 |
| `--chat-fg` on `--chat-surface` | 12.05 | 11.61 |
| `--chat-fg` on `--chat-code-bg` | 10.15 | 11.71 |
| `--chat-fg` on `--chat-inline-code-bg` | 9.77 | 10.93 |
| `--chat-fg` on `--chat-user-bg` | 9.77 | 10.67 |
| `--chat-fg-strong` on `--chat-bg` / `--chat-surface` | 13.59 / 15.55 | 15.08 / 13.97 |
| `--chat-fg-faint` on `--chat-bg` | 6.17 | 7.95 |
| `--chat-fg-faint` on `--chat-surface` | 7.06 | 7.36 |
| `--chat-fg-faint` on `--chat-code-bg` | 5.94 | 7.42 |
| `--chat-fg-faint` on `--chat-inline-code-bg` | 5.73 | 6.93 |
| `--chat-user-fg` on `--chat-user-bg` | 12.62 | 12.83 |
| `--chat-accent` on `--chat-bg` / `--chat-surface` | 5.06 / 5.78 | 9.47 / 8.77 |
| `--chat-warning` on `--chat-bg` / `--chat-surface` | 5.32 / 6.09 | 10.91 / 10.11 |
| `--chat-warning` on `--chat-warning-bg` | 5.85 | 9.31 |
| `--chat-danger` on `--chat-bg` / `--chat-surface` | 5.49 / 6.28 | 7.20 / 6.67 |
| `--chat-diff-add-fg` on `--chat-diff-add-bg` | 4.81 | 8.57 |
| `--chat-diff-del-fg` on `--chat-diff-del-bg` | 5.44 | 7.68 |
| `--chat-fg` on `--chat-diff-add-bg` / `-del-bg` | 10.01 / 9.39 | 9.76 / 10.75 |
| `--chat-syntax-keyword` on `--chat-code-bg` | 5.49 | 8.70 |
| `--chat-syntax-string` on `--chat-code-bg` | 5.22 | 10.04 |
| `--chat-syntax-comment` on `--chat-code-bg` | 5.02 | 6.47 |
| `--chat-syntax-number` on `--chat-code-bg` | 5.36 | 10.19 |
| `--chat-syntax-function` on `--chat-code-bg` | 6.12 | 8.64 |
| `--chat-syntax-type` on `--chat-code-bg` | 5.22 | 9.81 |
| `--chat-syntax-meta` on `--chat-code-bg` | 5.84 | 8.44 |
| `--chat-diff-add-fg` / `-del-fg` on `--chat-code-bg` (diff lines in code) | 4.87 / 5.88 | 10.28 / 8.36 |
| `--chat-focus-ring` on `--chat-bg` | 13.59 | 10.91 |
| `--chat-focus-ring` on `--chat-surface` | 15.55 | 10.11 |
| `--chat-focus-ring` on `--chat-user-bg` | 12.62 | 9.28 |
| `--chat-field-border` on `--chat-bg` | 3.52 | 3.66 |
| `--chat-field-border` on `--chat-surface` | 4.03 | 3.39 |

`--chat-border` is a decorative separator (1.44:1 on the light canvas, 1.88:1 on the dark, like Orca's hairlines).
Nothing may rely on it alone: cards are also set apart by their surface, and the one boundary the
user must find, the composer input, uses `--chat-field-border`. Other rules:

- Focus uses `outline: 2px solid var(--chat-focus-ring)` with a 2px offset; inside a scroll
  container use an inset outline so it is not clipped (Orca's rule).
- State is never colour alone: lamps carry text, diffs carry `+`/`−`.
- Motion stops under `prefers-reduced-motion`.
- Re-measure when a token value changes; the numbers above are the check.
