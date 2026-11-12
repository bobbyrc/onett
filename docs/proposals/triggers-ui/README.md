# Triggers page: two directions (phase 1, design only)

Triggers are owner-defined rules that start the orchestrator agent on its own: on a schedule
("every day at 05:00"), or when a new issue is opened in a project's repository. The backend is
described in `docs/proposals/triggers.md`. This folder holds the design for the page that manages
them: the problems the page has to solve, two rendered directions, and a recommendation. Nothing
here is product code.

- `mockups/`: static HTML for every state. Each page links the real `web/src/styles.css`, so the
  tokens, fonts and existing components are the app's own. `shared.css`, `a.css` and `b.css` hold
  only what the Triggers page adds. `shell.js` draws the rail and the phone's top bar.
- `shots/`: every state as a PNG at desktop 1440x900 and phone 390x844, light scheme. A file
  ending in `-full` is the whole scrolling page; one ending in `-bottom` is a dialog scrolled to
  its end.
- `shoot.mjs`: takes the screenshots, and fails when a page scrolls sideways or the fonts or the
  stylesheet did not load.

```sh
cd web && npm ci && npx playwright install chromium-headless-shell && cd ..
node docs/proposals/triggers-ui/shoot.mjs
```

## What the page has to solve

1. **A schedule nobody has to decode.** A schedule is stored as a cron expression (five fields
   such as `30 9 * * 1-5`). The owner should pick "Monday to Friday at 09:30" and read it back in
   those words, with the time zone beside it.
2. **No cron reading in the browser.** The server checks the expression and says when the trigger
   fires next. The page only writes the few shapes its own form produces and describes those. Any
   other expression is shown as it is.
3. **Did it fire, and if not, why.** A 05:00 run that was skipped because the factory was paused
   must be visible the next morning without opening anything: a lamp, a word and the reason in
   words. Colour is never the only signal.
4. **Two kinds in one list.** A schedule has a next time; an issue trigger waits. The same row
   layout has to carry both without empty columns.
5. **Where the agent's work is reached from.** Each trigger has one conversation, titled
   "Trigger: <name>", and every firing goes into it. The page needs one clear link to it per
   trigger.
6. **What an issue trigger will not do.** It fires only for issues opened by an owner, member or
   collaborator, only for issues opened after it was last turned on, and never for issues that
   issue intake takes or that onett filed itself. Ignored issues leave no history row, so the form
   has to say these rules up front.
7. **What cannot be changed.** A trigger's kind and project are fixed once it is created. The edit
   form must say so in place of two controls that do nothing.
8. **Run now, only where it exists.** A schedule trigger can be fired by hand, also while it is
   off. An issue trigger cannot, so it gets no button.
9. **Read-only for everyone but the owner.** Operators and viewers see every trigger, its prompt
   and its history. Controls they may not use are left out, not greyed out, as on the other pages.
10. **A phone.** The list, the history and the form must work at 390px with no sideways scrolling,
    and the reason for a skip must not hide behind a hover.
11. **Room in the navigation.** The rail already fills a 900px-tall window when four project lamps
    show. A fifteenth item does not fit unless something gives.

## Decisions both directions share

**Status of a trigger** (one lamp and one word, the same model as the Loops page):

| Lamp | Word | Means |
|---|---|---|
| green | OK | on, and its last firing started the agent |
| yellow | Skipped | on, and its last firing was skipped; the reason is beside it |
| red | Failed | on, and its last firing failed; the reason is beside it |
| grey | Not fired yet | on, and nothing has fired it so far |
| grey | Off | switched off |

**The schedule form.** "Repeats" offers four shapes and one way out. The page describes a stored
expression in words only when it matches a shape below; otherwise it shows the expression itself.

| Repeats | Fields shown | Cron it writes | Words shown afterwards |
|---|---|---|---|
| Every day | At | `0 5 * * *` | Every day at 05:00 |
| On chosen days | Days (Mon to Sun), At | `30 9 * * 1-5` | Monday to Friday at 09:30 |
| Every few hours | Every 1, 2, 3, 4, 6, 8 or 12 hours | `0 */6 * * *` | Every 6 hours |
| Every few minutes | Every 5, 10, 15, 20 or 30 minutes | `*/15 * * * *` | Every 15 minutes |
| Custom | one text field for the expression | as typed | the expression, in monospace |

- Only steps that divide the hour or the day evenly are offered, because the server restarts an
  uneven step at the top of the hour or the day.
- The time is typed as 24-hour `HH:MM`, like every other time in the UI. The browser's own time
  picker would show AM and PM.
- The time zone list starts with "Daemon's time zone" (stored as empty) and preselects the
  browser's zone.
- Under the fields, one line says the schedule back in words and shows the cron it will save. The
  next firing time is not guessed in the browser: it appears once the trigger is saved, from the
  server.
- A cron or time zone the server refuses shows the server's message under the form.

**Other behaviour** (not visible in a still picture):

- **Run now** reads "Running…" until the server answers, then a line above the list says what
  happened: "Fired Morning issue sweep." or "Skipped: the factory is paused."
- **Turn off / Turn on** saves at once. Turning an issue trigger on again starts it from that
  moment: older issues do not fire.
- **Delete** asks with the existing inline "Confirm?" button, and while it asks, a line says
  "Deletes the trigger and its firing history. Its conversation stays in Chat."
- **New trigger** has "On" ticked. The API creates a trigger switched off unless told otherwise;
  the form tells it, because a new trigger that silently never fires is the worse surprise.
- The list is sorted by name, with switched-off triggers last.
- The firing history shows the newest 200 firings, which is what the API returns by default.
- The prompt is shown as plain text, never as HTML or rendered Markdown.
- The page refreshes on the `trigger.updated`, `trigger.deleted` and `trigger.fired` events.

**Navigation.** Both mockups tighten the rail's rows by 2px (`.nav-item` padding from 7px to 6px,
desktop only), so fifteen items and four project lamps still fit a 900px-tall window. The two
directions show the two places the item could go:

- after **Goals** (direction A, recommended): Chat, Goals and Triggers are the three ways to put
  the orchestrator to work: now, until something is done, and whenever something happens.
- after **Loops** (direction B): next to onett's built-in periodic jobs, which are set in
  `onett.yaml`.

## Direction A: a table, and a page for each trigger

The pattern Tasks, Runs and Goals already use. `/triggers` is a table with one row per trigger:
status, name (with kind and project under it), when it fires, and the last firing with its reason.
The name opens the trigger's own page, `/triggers/:id`, with its schedule, its prompt, the link to
its conversation and every firing. Creating and editing happen in a dialog, like New task. Under
880px of column width the table becomes one sheet per trigger, like Loops.

| State | Desktop | Phone |
|---|---|---|
| List | ![](shots/a-list-desktop.png) | <img src="shots/a-list-phone-full.png" width="260"> |
| Empty | ![](shots/a-empty-desktop.png) | <img src="shots/a-empty-phone.png" width="260"> |
| New trigger (schedule) | ![](shots/a-form-schedule-desktop.png) | <img src="shots/a-form-schedule-phone.png" width="260"> |
| Edit trigger (GitHub issue) | ![](shots/a-form-issue-desktop.png) | <img src="shots/a-form-issue-phone.png" width="260"> |
| One trigger and its firings | ![](shots/a-history-desktop.png) | <img src="shots/a-history-phone-full.png" width="260"> |
| Read-only | ![](shots/a-readonly-desktop.png) | <img src="shots/a-readonly-phone-full.png" width="260"> |

Also in `shots/`: `a-list-phone.png`, `a-history-phone.png` and `a-readonly-phone.png` (the first
screen only), and `a-form-schedule-phone-bottom.png` and `a-form-issue-phone-bottom.png` (the
dialog scrolled to its end).

- Good: six triggers fit one desktop screen and between three and four a phone screen. Every
  trigger has an address, so its conversation in Chat can link back to it. It reuses the table,
  dialog, page header and status pill that exist, and it needs exactly the two requests the API
  offers: the list, and one trigger's firings.
- Weak: the history is one click away from the list, which shows only the last firing. On a phone
  the form is a dialog that scrolls inside itself, two screens long, as New task is today.

## Direction B: rule sheets beside a firing log

One page and no detail page. Each trigger is a sheet that reads as a rule: **when** it fires, what
it **asks**, and its **last** firing, with a lamp bar down its left edge. Beside the sheets runs a
log of recent firings across all triggers; "All firings" on a sheet narrows the log to that
trigger. A trigger is created and edited in place: the sheet turns into the form, and the schedule
is filled in as a sentence ("Every week, on Mon to Fri, at 09:30 in America/New_York"). On a phone
the two columns become two tabs, Triggers and Firings. An empty page offers the owner's two
examples as starting points.

| State | Desktop | Phone |
|---|---|---|
| List | ![](shots/b-list-desktop.png) | <img src="shots/b-list-phone-full.png" width="260"> |
| List, Firings tab | (the right-hand column above) | <img src="shots/b-list-firings-phone-full.png" width="260"> |
| Empty | ![](shots/b-empty-desktop.png) | <img src="shots/b-empty-phone.png" width="260"> |
| New trigger (schedule) | ![](shots/b-form-schedule-desktop.png) | <img src="shots/b-form-schedule-phone-full.png" width="260"> |
| Edit trigger (GitHub issue) | ![](shots/b-form-issue-desktop.png) | <img src="shots/b-form-issue-phone-full.png" width="260"> |
| Firings of one trigger | ![](shots/b-history-desktop.png) | <img src="shots/b-history-phone.png" width="260"> |
| Read-only | ![](shots/b-readonly-desktop.png) | <img src="shots/b-readonly-phone-full.png" width="260"> |

Also in `shots/`: the whole-page desktop shots `b-list-desktop-full.png`,
`b-form-schedule-desktop-full.png`, `b-form-issue-desktop-full.png`, `b-history-desktop-full.png`
and `b-readonly-desktop-full.png` (the rail ends at the first screen in these, as it does in the
app's own whole-page shots), and the first-screen phone shots `b-list-phone.png`,
`b-list-firings-phone.png`, `b-form-schedule-phone.png`, `b-form-issue-phone.png` and
`b-readonly-phone.png`.

- Good: the prompt is on the page, so a trigger reads as a whole rule. A skipped run shows at the
  top of the log whichever trigger it belongs to. The form is part of the page, so on a phone it
  scrolls like any page.
- Weak: a sheet is about 210px tall, so a desktop screen shows three and a half triggers and a
  phone two and a half. The API has no route for firings across triggers, so the log needs one
  request per trigger, merged in the browser, or a new route. A trigger has no address of its own.
  The sheet, the log, the in-place form and the tabs are all new components.

## Recommendation: direction A

1. **It fits the API as frozen.** The list is one request (`GET /triggers`, each trigger with its
   last firing) and a trigger's page is one more. Direction B's log is the one part of either
   design the API does not serve directly.
2. **It is how the rest of the app works.** A table that opens a page is what Tasks, Runs and Goals
   do, and the phone sheets are the Loops page's. An owner who knows those pages already knows this
   one, and the build is mostly existing components.
3. **It answers the morning question on the first screen.** Status, last firing and the reason for
   a skip or a failure are in the row. All six sample triggers fit one desktop screen.
4. **Each trigger gets an address.** The trigger's conversation in Chat can link back to
   `/triggers/:id`, and so can a notification later.

What direction A gives up is the log across triggers. If that turns out to be missed, it can be
added under the table once the API has a route for it; nothing in A has to change for that.

## Questions for the owner before the build

1. **Direction A or B?** A is recommended.
2. **Where in the rail?** After Goals (recommended) or after Loops.
3. **May the rail's rows lose 2px each?** Without it, a fifteenth item pushes the "live" light out
   of a 900px-tall window when four project lamps show. The alternative is to let the rail scroll.
4. **Should a new trigger start switched on?** The mockups tick "On" by default; the API's own
   default is off.

Not blocking, for later: the "Trigger: <name>" conversation in Chat should link back to its
trigger; and an issue that was ignored (wrong author, intake label) leaves no row, so the page
cannot say why a particular issue did not fire. The backend proposal lists that as open.

## The build: how the chosen parts join

The owner chose state by state: direction B's list, empty state and read-only view; direction
A's dialog for a new and an edited trigger; direction A's page for one trigger and its firings.
The joins are these, and nothing else changes.

- **Triggers** sits after Loops in the rail. The rail's rows keep their size; when the rail is
  taller than the window, the rail scrolls.
- **New trigger** in the list's header, **Edit** on a card and **Use this** on an empty page's
  example all open A's dialog. Nothing is edited in place: B's in-place form is not built.
- A card's name and its **All firings** link both go to A's page for that trigger. B's own
  "firings of one trigger" view, the narrowed log and the outlined card, is not built.
- A's page keeps its own **Run now**, **Edit**, **Turn off** and **Delete**. Delete is only
  there, since A's dialog has no Delete and B's cards never had one outside the in-place form.
- The log beside the cards lists every trigger's firings from one new route,
  `GET /api/v1/trigger-firings`, and takes each trigger's name from the list of triggers.
- A new trigger's dialog has "On" ticked, and always sends it: the API's own default is off.
- Only an owner sees New trigger, Edit, Turn on, Turn off, Run now and Delete. Everyone else
  gets B's read-only list and A's page without its buttons.
- In Chat, a trigger's conversation shows its model and permissions without the pickers, and
  one line says why: every turn there runs with the orchestrator's defaults.

The pictures above are still the reference for each state. Where the built page differs from
them, `docs/ui.md` describes what was built.

## Not done in this phase

- Dark scheme screenshots. The mockups use only the app's tokens, so they follow the system
  scheme; `SHOTS_SCHEME=dark SHOTS_OUT=<dir> node docs/proposals/triggers-ui/shoot.mjs` shoots it.
  Two dark shots were looked at and read well; none are in `shots/`.
- The sample reasons ("the factory was paused", "onett was shutting down") are examples. The page
  shows whatever reason the server sends.
- No product code, route or component. Phase 2 builds the chosen direction and compares its
  screenshots with these side by side.
