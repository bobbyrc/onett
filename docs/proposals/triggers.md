# Triggers: start the orchestrator on a schedule or on a new GitHub issue

Status: proposed. The API names in "API (frozen for implementation)" are frozen once the
coordinator accepts this document.

## Why

The orchestrator agent is the agent the owner talks to in Chat. It holds the API token and hands
work to other agents. Today it acts only when a person sends it a message, or when the
`heartbeat` loop (a periodic check that wakes it when work looks stuck) fires.

The owner wants it to start by itself in two more cases. In the owner's words:

> Triggers for the orchestrator agent in onett. For example, I would like for new GitHub issues
> to trigger the agent to take a look. Also a feature for scheduling jobs, like addressing any
> open issues at 5am every morning, etc.

A **trigger** is a rule the owner defines. When it fires, it starts one orchestrator turn (one
round of the agent reading a message and acting on it) with a prompt the owner wrote. This change
adds two kinds: `schedule` and `github_issue`.

## Goals

- A `schedule` trigger fires at the times the owner sets: every day at a time, on chosen weekdays,
  or at a fixed interval, in a named time zone.
- A `github_issue` trigger fires once for each new issue opened in a project's repository, with an
  optional label filter.
- Every firing is recorded with its cause and its outcome, readable through the API, and announced
  as an event so the UI updates live.
- Only a signed-in owner can create, change, delete, enable, disable or manually fire a trigger.

## Non-goals (out of scope for this change)

- Webhooks. Onett is local by default and cannot receive them. `github_issue` polls through the
  existing SCM plugin (the piece of code that talks to the code host, here GitHub).
- Other events: a pull request opened, CI failing, a comment, an issue edited.
- Firing for issues that existed before the trigger was created or enabled.
- Triggers in `onett.yaml`. They are created in the UI and stored in the database.
- Creating or changing triggers from the CLI. The CLI is read-only for triggers
  (`onett trigger list`, `onett trigger history <id>`), because the CLI signs in with the API token.
- "Run now" for a `github_issue` trigger, since there is no issue to hand it. The owner tests one
  by opening an issue.
- Month and weekday names, `@daily`-style shortcuts, seconds, and the `?`, `L`, `W` and `#`
  extensions in cron expressions.
- More than one label per trigger; per-trigger model or agent settings (the trigger's conversation
  uses the existing per-conversation settings); pruning old firings (they are kept until the
  trigger is deleted); a fresh agent session for each firing.

## Anti-goals (a failure even if it works)

- Any path that lets an agent create, edit, enable, disable or fire a trigger, including through
  chat tools, or through the CLI with the API token.
- Issue text reaching the agent as if the owner wrote it.
- A trigger that commits, merges or deploys by itself. A trigger only starts an agent turn.
  Invariant 1 in `AGENTS.md` holds: agents judge, code acts.
- A feedback loop: onett files a friction issue (its own report of a problem it hit), the trigger
  fires on that issue, and the turn causes more friction issues.

## Data model

The types live in `internal/domain/trigger.go`.

`Trigger`:

| JSON field | Type | Meaning |
|---|---|---|
| `id` | string | UUIDv7 (a time-ordered unique id), assigned by the store. |
| `name` | string | The owner's name for it, 1 to 100 characters. |
| `kind` | string | `schedule` or `github_issue`. Fixed after creation. |
| `project` | string | A configured project. Required for both kinds. It is the default project of the trigger's conversation, the scope of the project pause, and for `github_issue` the repository that is watched. Fixed after creation. |
| `prompt` | string | What the owner wants the orchestrator to do, 1 to 20000 characters. |
| `enabled` | boolean | A disabled trigger never fires by itself. Defaults to false when left out on creation. |
| `cron` | string | `schedule` only: a five-field cron expression (see Schedules). |
| `time_zone` | string | `schedule` only: an IANA zone name such as `America/Chicago` (the standard names for zones, with their daylight-saving rules). Empty means the daemon's local zone. |
| `next_run_at` | RFC 3339 time, optional | `schedule` only: when it fires next. Absent while disabled. |
| `label` | string, optional | `github_issue` only: fire only for issues that carry this label. |
| `issues_after` | RFC 3339 time | `github_issue` only: only issues created after this moment can fire. Set when the trigger is created, and again each time it is enabled. |
| `conversation_id` | string, optional | The conversation its turns run in. Set at the first firing. |
| `created_at`, `updated_at` | RFC 3339 times | |

API responses add one more field to a trigger: `last_firing` (a `TriggerFiring`, optional), the
newest firing, so a list page needs one request.

`TriggerFiring`:

| JSON field | Type | Meaning |
|---|---|---|
| `id` | string | |
| `trigger_id` | string | |
| `at` | RFC 3339 time | When onett evaluated it. |
| `cause` | string | `schedule`, `github_issue` or `manual`. |
| `scheduled_for` | RFC 3339 time, optional | Cause `schedule`: the scheduled time this firing answers. |
| `issue_number` | integer, optional | Cause `github_issue`. |
| `issue_url` | string, optional | Cause `github_issue`. |
| `by` | string, optional | Cause `manual`: `owner:<username>`. |
| `outcome` | string | `started`, `skipped` or `failed`. |
| `reason` | string, optional | Why it was skipped or failed, in plain words. |
| `conversation_id` | string, optional | Outcome `started`: the conversation the prompt went to. |
| `message_id` | string, optional | Outcome `started`: the internal message that carried the prompt. That message's `run_id` names the run once the turn starts. |

`started` means the prompt was handed to the orchestrator: its turn started, or the prompt is
queued behind the turn already running in that conversation.

## How a firing reaches the orchestrator

- Each trigger has its own orchestrator conversation. It is created at the first firing, with the
  title `Trigger: <name>` and the trigger's project, and its ID is saved on the trigger as
  `conversation_id`.
- A firing posts one internal message to that conversation (role `system`, kind `internal`, the
  same kind the heartbeat uses) and starts a turn. If a turn is already running there, the message
  is queued and goes out when that turn ends: behind it, never beside it. The heartbeat already
  does this for the Operations conversation in `internal/engine/heartbeat_waker.go`. This change
  moves that mechanism into one shared function, `wakeConversation`, that both the heartbeat and
  triggers call, instead of copying it.
- The message is the owner's prompt first. For a `github_issue` firing, a separator follows, and
  then the issue as evidence (see Issue trust).
- The firing is written to the database before the message is posted, and a schedule's
  `next_run_at` is moved forward before that. If onett dies in between, the firing is lost rather
  than repeated. The rule is at most once.

## Schedules

- Stored as `cron` plus `time_zone`. A small parser in `internal/domain/trigger_cron.go` reads the
  expression. No new dependency.
- Five fields separated by spaces, in this order:

  | Field | Allowed values |
  |---|---|
  | minute | 0-59 |
  | hour | 0-23 |
  | day of month | 1-31 |
  | month | 1-12 |
  | day of week | 0-7 (0 and 7 are Sunday) |

- Each field is a comma-separated list of: `*`, a number, a range `a-b`, and a step on `*` or on a
  range (`*/15`, `9-17/2`).
- Day rule, as in standard cron: when both day of month and day of week are restricted (neither is
  `*`), a day matches when either one matches. Otherwise both must match.

Examples:

| Expression | Fires |
|---|---|
| `0 5 * * *` | every day at 05:00 |
| `30 9 * * 1-5` | weekdays at 09:30 |
| `0 8 * * 1,4` | Mondays and Thursdays at 08:00 |
| `*/15 * * * *` | every 15 minutes |
| `0 */6 * * *` | every six hours, at 00:00, 06:00, 12:00 and 18:00 |

A fixed interval is written as a step. A step that does not divide the hour or the day evenly
starts again at the top of the hour or the day: `*/45 * * * *` fires at :00 and :45, then at :00
of the next hour.

An expression that never fires in the next five years (for example 30 February) is rejected.

The trigger loop checks about every 30 seconds, so a schedule fires within about half a minute
after its time.

### Daylight saving

The schedule is read on the wall clock of its time zone, the clock on the wall that people read.
Clocks change twice a year, which gives three cases. Each example uses America/New_York in 2026.

1. A normal day. `0 5 * * *` fires at 05:00 local time before and after a clock change: at
   10:00 UTC on 7 March 2026 (05:00 EST), and at 09:00 UTC on 8 March 2026 (05:00 EDT).
2. Clocks go forward, so some wall-clock times do not exist that day. `30 2 * * *` on 8 March 2026
   (02:00 becomes 03:00) fires once, at the moment the clocks jump: 03:00 EDT, which is 07:00 UTC.
   If several scheduled times fall inside the missing hour, they fire once together, not once
   each.
3. Clocks go back, so some wall-clock times happen twice. `30 1 * * *` on 1 November 2026 fires
   at 01:30 EDT (05:30 UTC) and does not fire again at 01:30 EST.

One consequence of case 3: a schedule with a step shorter than an hour pauses for the repeated
hour once a year.

### Catch-up

- `next_run_at` is stored on the trigger. When the loop sees that it has passed, it fires once,
  records the passed time as `scheduled_for`, and sets `next_run_at` to the first scheduled time
  after now.
- So after onett was down for any length of time, a trigger fires at most one catch-up run and
  never a backlog. A daily `0 5 * * *` trigger on a daemon that was off from Friday to Monday fires
  once on Monday when the daemon starts, and next on Tuesday at 05:00.
- Enabling a trigger, or changing its `cron` or `time_zone`, sets `next_run_at` to the first
  scheduled time after now. Time that passed while the trigger was disabled is not caught up.

## GitHub issues

- About every 30 seconds, the trigger loop lists the open issues of the project's repository
  through the SCM plugin. It does this only when the project has an enabled `github_issue`
  trigger.
- An issue can fire a trigger when all of these hold:
  - it was created after `issues_after`;
  - it carries the trigger's `label`, when the trigger has one (a label added later counts, the
    first time the loop sees it);
  - its author is trusted (see Issue trust);
  - it does not carry the friction label;
  - issue intake does not handle it (see below).
- Each issue fires each trigger at most once. The record is the firing row itself (`trigger_id`
  plus `issue_number`), so it survives a restart. An issue is settled for a trigger once that
  trigger has a firing for it with outcome `started` or `failed`.
- A new or newly enabled trigger does not fire for issues that already existed, because
  `issues_after` is set to that moment.
- Issues opened while onett was down are still open and still newer than `issues_after`, so they
  fire when it starts again. Each is a separate event, not a backlog that collapses into one run.
- An issue opened and closed between two polls is never seen. The listing reads the 100 newest open
  issues.
- An issue that onett filed itself never fires a trigger. An issue carrying the project's friction
  label (`loops.friction.label`) is skipped, whatever the trigger's `label` is. This stops the
  feedback loop named in the anti-goals.

### Relation to issue intake

Issue intake is the existing loop that turns an open issue carrying the intake label
(`loops.issue_intake.label`, default `onett`) into a task directly, with no agent deciding.

The rule is that intake wins. While issue intake is enabled for the project, a `github_issue`
trigger does not fire for an issue that carries the intake label, because that issue has already
become a task. Every other issue goes to the trigger. The message to the orchestrator says whether
intake is on and which label it uses, so the orchestrator does not also create a task for an issue
that a person then labels for intake.

## Issue trust

Who may start an agent turn from an issue is decided by the author. The brief for this work
assumed that issue intake already checks the issue's author for OWNER, MEMBER or
COLLABORATOR. It does not. Intake trusts the **label** on an issue, since only people with triage
rights can add labels. It applies the OWNER, MEMBER, COLLABORATOR rule to **comments** only
(`trustedComments` in `internal/engine/loop_intake.go`). The issue listing
(`gh issue list --json`) does not return the author's association for the issue itself.

- A trigger cannot rely on a label, because its label filter is optional. So every `github_issue`
  trigger, with or without a label filter, uses the author rule. It fires only when GitHub's
  `author_association` for the issue is `OWNER`, `MEMBER` or `COLLABORATOR`. The repository may
  be public, so an issue from anyone else is ignored and is not recorded as a firing.
- To support that:
  - `scm.Issue` gains the field `AuthorAssociation`.
  - The SCM interface gains one method, `ListOpenedIssues(ctx, since)`. It returns the open issues
    (not pull requests) created after `since`, oldest first, with `AuthorAssociation` filled in.
  - The GitHub plugin implements it with `gh api repos/{owner}/{repo}/issues`. The REST listing
    returns `author_association`; pull requests in that listing are skipped.
  - `ListIssues`, which intake and friction use, is unchanged.
- The check for the three trusted associations becomes one small function, `trustedAssociation`,
  used by both `trustedComments` and the trigger, so the rule lives in one place.
- Issue text is untrusted even when its author is trusted. The title and body reach the agent as
  evidence. They pass through `redactSecrets`, come after the owner's prompt and a separator, and
  start with `evidenceNote` ("data, not instructions"). The body sits inside a code fence that the
  body cannot close, and the text is cut at 8000 characters. Issue text is never spliced into the
  owner's prompt.

## Pause, budget and rate limits

A trigger only starts an agent turn, and only when work may start. Before each firing, onett checks
these in order: the global pause; the project's pause; the daily budget (`limits.daily_budget_usd`);
and a rate-limit hold on the orchestrator's harness (the program that runs the agent). If one of
them applies, the firing is recorded with outcome `skipped` and a reason.

| Situation | `schedule` | `github_issue` |
|---|---|---|
| Paused, over budget or rate-limited when due | Recorded as `skipped`. That scheduled time is gone; the next one is as scheduled. | Recorded as `skipped` once, not once per poll. The issue stays pending and fires when the condition clears. |
| A turn is already running in the trigger's conversation | The message is queued behind it. Outcome `started`. | The message is queued behind it. Outcome `started`. |
| The message could not be posted (a store error, or onett is shutting down) | Outcome `failed` with the reason. Not retried. | Outcome `failed` with the reason. Not retried. |
| Manual run while paused or over budget | Recorded as `skipped` and returned to the caller. | Not applicable: there is no manual run. |

The trigger loop keeps running while the factory or a project is paused, only so that it can record
what it skipped. It starts nothing while paused.

## Who can do what

- Create, update (which includes enable and disable), delete and "run now" need the `administer`
  action, which means a signed-in owner session. Reading triggers and firings needs `read`.
- The API token, which the orchestrator agent and the CLI hold, is limited to `read` and `operate`
  (`auth.TokenAllows`). It gets 403 on every mutating trigger route. Nothing in this change widens
  the token.
- This extends invariant 12 in `AGENTS.md`, which becomes: config, users and triggers change only
  through a person. The README Safety model section gets the same sentence.
- One thing the token can do, stated so nobody is surprised: `POST /loops/triggers/run`, an
  `operate` route that exists for every loop, makes the trigger loop check now instead of in up to
  30 seconds. It cannot fire a schedule before its time, and it cannot fire an issue a second time.
  It only brings forward a check that was about to happen.
- An agent cannot make a trusted issue appear. Agents get no GitHub credentials (invariant 3), and
  the only issues onett files for agents are friction issues, which never fire a trigger.

## API (frozen for implementation)

All routes are under `/api/v1`. JSON, with unknown fields rejected, as everywhere.

| Method and path | Action | Request body | Response |
|---|---|---|---|
| `GET /triggers` | read | none | 200, an array of triggers (each with `last_firing`), oldest first. |
| `POST /triggers` | administer | create body | 201, the trigger. |
| `GET /triggers/{id}` | read | none | 200, the trigger with `last_firing`. |
| `PATCH /triggers/{id}` | administer | patch body | 200, the trigger. |
| `DELETE /triggers/{id}` | administer | none | 204. Deletes the trigger and its firings. Its conversation stays. |
| `POST /triggers/{id}/run` | administer | none | 200, the `TriggerFiring` it recorded. `schedule` triggers only. Works on a disabled trigger and does not change `next_run_at`. |
| `GET /triggers/{id}/firings?limit=N` | read | none | 200, an array of `TriggerFiring`, newest first. `limit` defaults to 200. |

Create body, one example for each kind:

```json
{"name": "Morning issues", "kind": "schedule", "project": "onett", "prompt": "Look at the open issues and queue work for the ones that are ready.", "enabled": true, "cron": "0 5 * * *", "time_zone": "America/Chicago"}
```

```json
{"name": "New issue triage", "kind": "github_issue", "project": "onett", "prompt": "A new issue was opened. Read it and decide what to do.", "enabled": true, "label": "bug"}
```

Fields of the create body:

| Field | Type | Rule |
|---|---|---|
| `name` | string | Required. |
| `kind` | string | Required. `schedule` or `github_issue`. |
| `project` | string | Required. |
| `prompt` | string | Required. |
| `enabled` | boolean | Default false. |
| `cron` | string | Required for `schedule`; not allowed for `github_issue`. |
| `time_zone` | string | Optional; `schedule` only. |
| `label` | string | Optional; `github_issue` only. |

Patch body: any subset of `name`, `prompt`, `enabled`, `cron`, `time_zone` and `label`. A field left
out is unchanged. `kind` and `project` cannot be changed, so they are unknown fields here. Example:
`{"enabled": false}`.

A full trigger response for a `schedule` trigger, with its `last_firing`:

```json
{"id": "0190e5a2-7c41-7b3e-9d52-4f1a8c6e2b10", "name": "Morning issues", "kind": "schedule", "project": "onett", "prompt": "Look at the open issues and queue work for the ones that are ready.", "enabled": true, "cron": "0 5 * * *", "time_zone": "America/Chicago", "next_run_at": "2026-11-13T11:00:00Z", "conversation_id": "0190e5a2-9b10-7d22-8f3c-5e7a1b4c9d01", "created_at": "2026-11-10T16:20:00Z", "updated_at": "2026-11-12T11:00:02Z", "last_firing": {"id": "0190e5b8-3d6a-7e90-a1f4-2c8b9d0e7f35", "trigger_id": "0190e5a2-7c41-7b3e-9d52-4f1a8c6e2b10", "at": "2026-11-12T11:00:02Z", "cause": "schedule", "scheduled_for": "2026-11-12T11:00:00Z", "outcome": "started", "conversation_id": "0190e5a2-9b10-7d22-8f3c-5e7a1b4c9d01", "message_id": "0190e5b8-3c11-7a04-b6e2-9f1d7a3c5e88"}}
```

One `TriggerFiring` for cause `github_issue` with outcome `started`:

```json
{"id": "0190e6c1-5e20-7f14-93b8-4d6a2c1f0e77", "trigger_id": "0190e6b0-1a4d-7c3e-8e52-7b9f0d2a6c41", "at": "2026-11-12T14:03:31Z", "cause": "github_issue", "issue_number": 351, "issue_url": "https://github.com/example-owner/onett/issues/351", "outcome": "started", "conversation_id": "0190e6b0-2f8e-7d10-a5c3-1e6b4d9f7a20", "message_id": "0190e6c1-4a07-7b92-9e1d-3c5f8a2b6d14"}
```

Errors:

| Status | When |
|---|---|
| 400 | An invalid body: an unknown project; a `github_issue` trigger on a project whose repository is not on GitHub; a bad cron expression or time zone; a field that belongs to the other kind; an empty name or prompt. |
| 403 | The API token, or a non-owner, on a mutating route. |
| 404 | An unknown id. |
| 409 | `run` on a `github_issue` trigger. |

## Events

| Event type | Data | When |
|---|---|---|
| `trigger.updated` | the `Trigger` | After create and patch, and after a firing changed `next_run_at` or `conversation_id`. |
| `trigger.deleted` | `{"id": "<trigger id>"}` | After delete. |
| `trigger.fired` | the `TriggerFiring` | Once per firing, with its final outcome. |

Each event carries the trigger's project. The constants are `EvTriggerUpdated`, `EvTriggerDeleted`
and `EvTriggerFired`, in `internal/domain/trigger.go`.

## Store

One new migration is appended to `internal/store/sqlite/migrations.go`:

```sql
CREATE TABLE triggers (
	id TEXT PRIMARY KEY,
	project TEXT NOT NULL,
	created_at INTEGER NOT NULL,
	data TEXT NOT NULL
);
CREATE TABLE trigger_firings (
	id TEXT PRIMARY KEY,
	trigger_id TEXT NOT NULL,
	issue_number INTEGER NOT NULL DEFAULT 0,
	at INTEGER NOT NULL,
	data TEXT NOT NULL
);
CREATE INDEX trigger_firings_trigger ON trigger_firings (trigger_id, at);
```

As in the other tables, `data` holds the JSON document, and the other columns exist for lookups.

New `Store` methods: `CreateTrigger`, `GetTrigger`, `ListTriggers`, `UpdateTrigger`,
`DeleteTrigger`, `CreateTriggerFiring`, `UpdateTriggerFiring`, `ListTriggerFirings` and
`LastIssueFiring`.

The migration-undo test (`TestGoalMigrationKeepsExistingTasks`, issue #189) lists every later
migration's tables by hand. This change fixes that test so that a new migration no longer has to
edit it.

## Engine

- A new loop named `triggers` lives in `internal/engine/loop_triggers.go`. The existing loop
  framework runs it per project, about every 30 seconds. The interval is a constant, not a new
  config key. It appears in `GET /loops` like every other loop. Its errors, for example GitHub being
  unreachable, show there and in the heartbeat digest.
- Four new methods on the API's controller interface, all called only from `administer` routes:
  `CreateTrigger`, `UpdateTrigger`, `DeleteTrigger` and `RunTrigger`.
- CLI: `onett trigger list` and `onett trigger history <id>`, both read-only.

## What changes in the rules

- `AGENTS.md` invariant 12 and the README Safety model: triggers join config and users as things
  only a person changes.
- `docs/design.md`, `docs/api.md`, `docs/openapi.yaml` and `web/src/types.ts` describe the new
  routes and types. The page that manages triggers is a separate change, and `docs/ui.md` goes with
  it.

## What stays open (for the owner)

- A trigger's conversation lives as long as the trigger, as the Operations conversation does, so its
  agent session grows over time. A fresh session per firing would cost less for a daily job. It is
  left out until there is evidence that it matters.
- Firings are never pruned. A trigger that fires every minute writes about half a million rows a
  year.
- Ignored issues (untrusted author, intake label, friction label) leave no firing row. If the owner
  wants to see why an issue did not fire, a "skipped: not a collaborator" row is a small follow-up.
  The cost is noise on a public repository.
