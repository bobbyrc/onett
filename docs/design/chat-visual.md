# Chat visual design

**The rule.** Chat (`/chat` and `/chat/task/:id`) takes Orca's shape, type and layout: the Geist and
SF Mono font stacks, the radius scale that starts at 0.625rem, the spacing and type scale, the
centred 46rem column, the rounded user bubble, plain assistant prose, quiet tool lines, the rounded
composer card, the jump pill and the message rail. **The colours and the andon status lamps are
onett's** (the green-grey palette from `styles.css`, and square lamps that always carry a text
label). The rest of the app keeps onett's identity: Barlow and square corners (`docs/ui.md`,
"Tokens").

The values come from [Orca](https://github.com/stablyai/orca) (stablyai/orca, MIT, Copyright 2026
Lovecast Inc.); `chat-tokens.css` carries the licence text. Its logo, marks, illustrations and name
are not used.

The tokens in `web/src/chat-tokens.css` are the contract. Chat components read them through
`var(--chat-*)`; a new chat style must not invent a colour or a size, and must not rename a token.
`web/src/lib/chatCss.test.ts` fails on a hex colour or a non-chat variable in a chat stylesheet, and
`web/src/lib/chatContrast.test.ts` recomputes the contrast table under "Accessibility". The
Terminal view (`.tr-term` in `Transcript.css`) is exempt by design: it keeps its andon-board look on
the app's `--board-*` tokens.

How each element compares with Orca, with screenshots: [chat-orca-comparison.md](chat-orca-comparison.md).
Every visual PR attaches `scripts/pr-shots.sh` before/after tables (see "Screenshots for UI PRs" in
`docs/ui.md`).

## Reference images

None is committed here. Orca hosts public ones: the desktop hero
<https://raw.githubusercontent.com/stablyai/orca/main/docs/assets/readme-hero.jpg> and the mobile
companion <https://raw.githubusercontent.com/stablyai/orca/main/docs/assets/feature-wall/mobile-companion-app-showcase.gif>.
Conformance of the shipped chat was also checked against a private Orca screenshot supplied by the
owner; it is not part of the repository.

## Principles

1. **One reading column.** Transcript and composer share one maximum width, `--chat-max-width`
   (46rem), centred.
2. **Prose first, machinery second.** Assistant text is plain on the canvas; tool calls are quiet
   one-line rows, expandable.
3. **Three text tones.** Strong (titles, the user's words), normal (prose), faint (labels,
   timestamps, tool names).
4. **Few boxes.** Only the user's message, code blocks and the composer get a surface. Hairlines,
   not shadows; no uppercase labels and no heavy rules.
5. **A small fixed scale.** Type 14 / 12 / 11 / code 12 and a 4px spacing unit.

## Colour

Colours are onett's. The colour tokens are `var()` references to the app tokens (`--bg`, `--panel`,
`--text`, `--muted`, `--border`, `--field`, `--ok`, `--warn`, `--danger`), so chat is a
green-tinted grey and dark mode is onett's green-black. Light is the default and dark follows
`prefers-color-scheme`; there is no manual switch.

Only the derived surfaces use Orca's `color-mix` form and percentages, with onett's `--text` over
onett's `--bg`: user bubble 4% (dark 7%), user border 7% (6%), code block 2% (3.5%), inline code 4%
(6%), inline-code border 9% (8%), composer border 11% (9%). Borders mix toward `transparent`, as
Orca's do. The code and inline-code fills mix toward `--bg` instead, so they stay opaque and the
contrast test can measure them. Text is not mixed: `--chat-fg` is onett's own value, strong and
faint are `--text` and `--muted`. Status text uses the readable status colours (`--ok`, `--warn`,
`--danger`), never the raw lamp fills.

## Type

Four steps: `--chat-text-md` 14px (prose, the user's words, titles), `--chat-text-sm` 12px (labels,
timestamps, tool lines, footer), `--chat-text-xs` 11px (captions) and `--chat-text-code` 12px (code
and tool output). Line height is `--chat-leading`, 1.625. Sans is `--chat-font`, `'Geist',
-apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif`; mono is `--chat-font-mono`, `'SF Mono',
SFMono-Regular, ui-monospace, Menlo, monospace`. Geist (SIL OFL 1.1, from the npm `geist` package)
is self-hosted at weights 400, 500 and 600 in `web/src/assets/fonts` (licence `OFL-Geist.txt`) with
`font-display: swap`. Chat does not use Barlow or Barlow Condensed: the conversation list headings
and the thread title are sentence case, 14px, weight 600, in `--chat-font`. The page's own `Chat`
title above the list is the app's heading and stays Barlow.

## Spacing

`--chat-space-1..6` are 4, 8, 12, 16, 24 and 32px. Messages are `--chat-msg-gap` (1.25rem) apart.
Prose blocks have 12px between them, list items 4px, and lists are indented 1.5rem. Tool rows are
26px tall (44px on a coarse pointer, so they stay tappable). The column is `--chat-max-width`
(46rem) and is centred; on a phone the side gutters shrink to 12px.

## Radii, borders and shadows

`--chat-radius` is 0.625rem (10px). `-sm`, `-md`, `-lg` and `-xl` are 0.6, 0.8, 1 and 1.4 times it
(6, 8, 10 and 14px). Where each is used: `-sm` inline code and small controls, `-md` buttons, chips,
the segmented switch's rows and cards, `-lg` code blocks and expanded tool output, `-xl` the user
bubble and the composer card. Round things (the Send button, the `+` button, the jump pill, the
segmented switch) are fully round. Borders are 1px hairlines. Nothing in chat has a shadow except
the jump pill's 1px hairline lift; `--chat-shadow-composer` is `none`.

## User message

A right-aligned bubble: `--chat-user-bg` surface, 1px `--chat-user-border`, `--chat-radius-xl`,
padding 10px by 14px, `--chat-text-md` in `--chat-user-fg`, width at most `min(72ch, 85%)` (90% on a
phone). No avatar. A long message folds at about eight lines under a fade with a "Show more"
button. A queued message is a dashed outline with faint text. Under it, a row holds the time
(`--chat-text-sm`, faint, tabular numerals) and a copy button; on a mouse it appears on hover or
focus, on touch it is always there. The time's `title` carries the full time.

## Assistant prose

Plain text on the canvas at full column width, `--chat-text-md` in `--chat-fg`, no bubble and no
avatar. A faint 12px weight-600 label names the agent above a block ("Orchestrator", "Implementer").
Markdown headings stay at body size and are told apart by weight 600 in `--chat-fg-strong`; bold is
weight 600 in the strong tone; lists keep 4px between items. Inline code is a chip: `--chat-font-mono`
at `--chat-text-code`, `--chat-inline-code-bg`, 1px `--chat-inline-code-border`, `--chat-radius-sm`.
Prose is selectable.

## Code blocks

`--chat-code-bg` surface, 1px `--chat-composer-border`, `--chat-radius-lg`, 12px margin above and
below. A header row shows the language (faint, `--chat-text-sm`) and two quiet buttons, **Copy** and
**Wrap**, which brighten to `--chat-fg-strong` on hover and focus and keep a 44px target on touch.
Wrap is off by default (long lines scroll sideways); when on, the button shows `aria-pressed` and an
underline, not colour alone. The body is mono at `--chat-text-code`, padded `--chat-space-3`. Code is
highlighted lazily, plain first; token colours are the `--chat-syntax-*` tokens, each measured
against `--chat-code-bg`.

## Tool lines and tool runs

A tool call is one quiet row: 26px tall, `--chat-text-sm` in the faint tone (it brightens on hover),
a lamp, the tool name (weight 500), the main argument in mono and truncated, a chevron, and the
metadata right-aligned and faint ("completed · 1m 00s · 131.3k tokens", with tabular numerals). Two
or more consecutive calls fold into one run whose header is a sentence ("Ran 1 command, read 1 file,
searched 1 time") with the run's duration and token total; opening it lists each call indented
`--chat-space-6`. A lone call stays a single line. The lamp is onett's: an 8px square, `--chat-accent`
while running (blinking in two steps, still under `prefers-reduced-motion`), `--lamp-done` when
completed, `--chat-danger` when failed; the state is also written in the metadata, never colour
alone. Expanded detail is one `--chat-code-bg` surface with a 1px `--chat-border` hairline and
`--chat-radius-lg`, mono at `--chat-text-code`, at most 16rem tall. On a narrow screen the metadata
wraps under the row and the duration and tokens drop, so a row never overflows. Edit counts are
`+n −m` (`.tl-diff`) in `--chat-diff-add-fg` and `--chat-diff-del-fg`; the signs mean colour is not
the only cue. Onett has no inline diff body (the run log does not carry old and new text); the
`-bg` diff tokens are reserved for one.

## Activity line and Worked-for line

While a turn runs, one line follows the transcript: a lamp (`--chat-accent` working, `--chat-warning`
waiting for you, `--chat-danger` blocked), a label ("Working · 1h 05m", "Waiting for you",
"Blocked"), a quiet "output 30s ago" note in `--chat-warning` when the agent has gone quiet, and a
"view run" link. The lamp switches in two steps; there is no spinner. When a turn has finished, the
transcript shows a faint "Worked for 1m 00s" button with a chevron and a 1px `--chat-border` hairline
that fills the rest of the row, above the answer; it folds that turn's tool work (closed once the
turn is done, open while it runs). Errors are one line in `--chat-danger`. Orca's reasoning
disclosure is not built: the harnesses do not record reasoning.

## Composer

One rounded card, `--chat-surface`, 1px `--chat-composer-border`, `--chat-radius-xl`, no shadow, in
the transcript's column width and sticky at the bottom. Inside, top to bottom: attachment chips
(rounded, on a hairline), a growing textarea at `--chat-text-md` (scrolls past 40% of the screen
height), and a footer row: a round 32px `+` button (and a photo button on a phone), the keyboard hint
(hidden on a phone), a faint `--chat-text-sm` label naming the role and model, and a round 32px Send
button filled with `--chat-fg-strong` (it becomes a Stop square while a turn runs; `aria-label`
carries the word). While the field has focus the card's 1px edge steps up to
`--chat-composer-border-focus` (muted ink); it draws no ring, because the owner types there for
minutes and a ring would frame the whole card the whole time. The small controls inside it (`+`,
Send, the permission and model menus) show the keyboard ring on `:focus-visible`. A dragged file turns
the border dashed. On the new-chat page the same card sits under the empty state at the same column
width, with the example prompts below as rounded chips (`--chat-radius-md`, faint 12px text, 44px
tall).

## Jump pill and message rail

**Jump to latest** is a small round pill centred just above the composer while the reader is away
from the end: 32px tall, `--chat-surface` with a `--chat-composer-border` edge, faint 12px text with
an arrow, brightening on hover, and an "n new" count in the strong tone. **The message rail** is a
column of short ticks fixed at the right edge of the screen, one per user message: 2px by 12px
`--chat-border` ticks inside 24px by 8px hit areas, the current one longer (16px) and in
`--chat-fg-faint`. Clicking a tick scrolls to that message. It shows from 768px up and is hidden on
a phone.

## Empty, error and loading states

A centred column under the top of the thread: a 22px line icon (`currentColor`, no circle), a title
at `--chat-text-md` weight 600 in the strong tone, one faint 12px line (at most 24rem wide), and for
an error a Retry button. Loading shows a skeleton, only after 150ms: a right-aligned user bubble
and full-width assistant lines, static under `prefers-reduced-motion`.

## Page chrome around the thread

The conversation list and thread header follow the same type. Section headings ("Orchestrator",
"Agents") and the thread title are sentence case at 14px weight 600 (the Agents heading is faint
12px); the open conversation sits on `--chat-surface` with a `--chat-radius-md` corner; buttons on
the page (New conversation, Close, Retry), attachment chips, the task-agent note and the closed note
use `--chat-radius-md`. The Conversation / Terminal switch is a round pill. On a phone the list
becomes a row of rounded chips above the open thread.

## Phone

The same design, `< 760px`: the column's side gutters are 12px, the user bubble may be up to 90%
wide, the composer clears the home indicator (`web/e2e/mobile.spec.ts` covers it), tool-row
metadata wraps, the message rail is hidden and tap targets are at least 44px.

## Tokens

All defined in `web/src/chat-tokens.css` on `:root`; dark values sit in `@media (prefers-color-scheme: dark)`.
Names marked * are the derived hairlines and fills that have no app-token equivalent.
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
| `--chat-focus-ring` | `#18201c` | `#e4ebe6` | keyboard focus outline on chat controls (= `var(--text)`: ink in both modes, never the app's dark call yellow) |
| `--chat-composer-border-focus` | `#4b5751` | `#a1aea7` | the composer card's edge while it has focus (= `var(--muted)`) |
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

## Where it lives

| Element | File |
|---|---|
| Tokens, fonts | `web/src/chat-tokens.css`, `web/src/assets/fonts` |
| User bubble, prose, Worked-for divider, timestamps | `web/src/components/Transcript.css` |
| Tool lines, runs, diff counts | `web/src/components/ToolRun.css` |
| Activity line, Worked-for toggle | `web/src/components/ActivityLine.css` |
| Composer card, jump pill | `web/src/components/Composer.css` (pill base in `web/src/pages/Chat.css`) |
| Code blocks | `web/src/components/CodeBlock.css` |
| Message rail | `web/src/components/MessageRail.css` |
| Empty, error and loading states | `web/src/components/ChatState.css` |
| Page layout, conversation list, thread header, chips | `web/src/pages/Chat.css` |

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
| `--chat-focus-ring` on `--chat-bg` | 13.59 | 15.08 |
| `--chat-focus-ring` on `--chat-surface` | 15.55 | 13.97 |
| `--chat-focus-ring` on `--chat-user-bg` | 12.62 | 12.83 |
| `--chat-composer-border-focus` on `--chat-bg` | 6.17 | 7.95 |
| `--chat-composer-border-focus` on `--chat-surface` | 7.06 | 7.36 |
| `--chat-composer-border-focus` against the resting `--chat-composer-border` (on the card) | 5.67 | 5.85 |
| `--chat-field-border` on `--chat-bg` | 3.52 | 3.66 |
| `--chat-field-border` on `--chat-surface` | 4.03 | 3.39 |

`--chat-border` is a decorative separator (1.44:1 on the light canvas, 1.88:1 on the dark, like Orca's hairlines).
Nothing may rely on it alone: cards are also set apart by their surface, and the one boundary the
user must find, the composer input, uses `--chat-field-border`. Other rules:

- Focus on a control (a button, a menu, a chip) uses `outline: 2px solid var(--chat-focus-ring)`
  on `:focus-visible` with a 2px offset; inside a scroll container use an inset outline so it is
  not clipped (Orca's rule). The ring is ink in both modes: the app's dark `--focus` is call yellow,
  which in onett means "needs you", so chat does not use it.
- Focus on a text-entry surface (the composer card, on the new-chat page, in a conversation and in
  a task thread) steps its 1px edge up to `--chat-composer-border-focus` with `:focus-within`: no
  ring, no 2px frame, no yellow. `src/lib/composerFocusCss.test.ts` guards it.
- State is never colour alone: lamps carry text, diffs carry `+`/`−`.
- Motion stops under `prefers-reduced-motion`.
- Re-measure when a token value changes; the numbers above are the check.
