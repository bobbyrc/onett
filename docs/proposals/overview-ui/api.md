# Overview, Direction B: what the page needs from the daemon

The design gate for building Direction B ("Read the board", see `README.md`). It names every
piece of data the page shows, where it comes from in the code on this branch's base (the
integration branch at `f598e04`), and the little that has to be added. Names here are proposals
until the coordinator freezes them.

In short: **one new read route, three new fields on a task, one new field in the heartbeat's
memory.** No new table, no migration, no new action, no new status, no new write route.

## What already exists and is enough

| The page shows | Comes from (today) |
|---|---|
| A project's board window: lamp, counts, last deploy | `GET /projects`, `GET /deployments` (as now, `AndonBoard`) |
| The line window: running or paused, active, queued, cost today | `GET /control`, `GET /stats` |
| "Nobody will be told" (no channel) and rate-limit holds | `GET /stats`: `owner_unreachable`, `holds` |
| In flight, per lane | `GET /runs?status=starting,running` (each run has `project`); queued count from `GET /projects` `counts` |
| Approvals waiting, per lane | `GET /approvals?status=pending`, and each approval's task (`GET /tasks/{id}`) for risk, reasons and PR number, as `ApprovalCard` reads them now |
| Orchestrator card: last and next check, told you and acted in 24h, Operations link | `GET /loops`, the `heartbeat` rows: `last_run_at`, `next_run_at`, `escalations_24h`, `actions_24h`, `operations_conversation_id`, `quiet_now` |
| Setup steps | `GET /projects`, `/agents`, `/stats`, `/secrets` (as now, `lib/setupChecklist.ts`) |
| Activity | `GET /events/history` and the live stream. Each event already carries its subject as `data` (a Task, Run, Approval or Goal), so the UI writes the sentence. One gap: a `run.updated` event has no task title. The feed does not need it: "started on <title>" is written from the `task.updated` event, and the run rows that are plumbing are hidden. |

**Approvals are reviewed on the task page, and the task page can approve today:**
`web/src/pages/TaskDetail.tsx:98` renders `ApprovalCard` for each pending approval, with Approve
and Reject. "Review" on an Overview row is a link to `/tasks/<task_id>`. Nothing changes there.

## New: `GET /api/v1/problems` (action: read)

What is wrong, per project, with one entry per cause. The page cannot build this itself: the
cause of a block, a loop's failure streak and whether the owner was told are not in any response
today. Approvals are not in it: they are decisions, not problems, and `GET /approvals` has them.

Response: a JSON array of `Problem`, sorted by project, then kind in the order below, then
`count` descending, then `since` ascending. `[]` when nothing is wrong.

| Field | Type | Meaning |
|---|---|---|
| `kind` | string | `blocked_tasks`, `failing_loops` or `blocked_goal` |
| `project` | string | the project's name |
| `key` | string | unique in the response and stable while the problem lasts: `blocked_tasks:<project>:<cause or reason>`, `failing_loops:<project>`, `blocked_goal:<goal id>` |
| `title` | string | one line, at most 200 characters. `blocked_tasks`: the cause (`blocked_cause`), or the task's whole `blocked_reason` when the block recorded no cause. `blocked_goal`: the goal's title. `failing_loops`: empty (the page names the loops). |
| `detail` | string | one line, at most 300 characters, keeping the end of the text (a wrapped error says what failed first and why last). `blocked_tasks`: `blocked_detail` of the task blocked most recently. `failing_loops`: `last_error` of the loop that failed most recently. `blocked_goal`: the goal's reason. May be empty. |
| `count` | integer | `blocked_tasks`: how many tasks. `failing_loops`: the longest run of failed passes among the loops. `blocked_goal`: 1. |
| `since` | time | `blocked_tasks`: the earliest `blocked_at`. `failing_loops`: the earliest streak start. `blocked_goal`: the goal's `updated_at`. |
| `last_at` | time, optional | `blocked_tasks`: the latest `blocked_at`. `failing_loops`: the latest `last_run_at`. |
| `told_at` | time, optional | when onett last sent the owner a notification about this problem after it stayed open: the heartbeat's escalation of one of its items, or the loop alert. Absent when neither has happened. |
| `tasks` | array, optional | `blocked_tasks` only: `{ "id", "title", "attempts", "max_attempts" }`, most recently blocked first, at most 50 (`count` is the real number). |
| `loops` | array, optional | `failing_loops` only: `{ "name", "fails", "since" }`, one per failing loop of the project. |
| `goal_id` | string, optional | `blocked_goal` only. |

Where each piece comes from:

- **`blocked_tasks`**: every task of the project with status `blocked`, grouped by
  `blocked_cause`, or by the whole `blocked_reason` when the cause is empty. So seven tasks that
  share a cause are one entry, and a task blocked for its own reason is an entry with
  `count` 1. Tasks of a goal are included: they are blocked whoever handles them.
- **`failing_loops`**: one entry per project that has a loop whose `LoopState.LastError` is not
  empty. `fails` and `since` come from the row `loop_alert:<loop>` that the loop-alerts change
  keeps (`internal/engine/loop_alert.go`, `loopStreak`); a loop with an error and no streak row
  reads as `fails` 1 since its `last_run_at`. No second counter.
- **`blocked_goal`**: goals with status `blocked` (the same list `/stats` sends as
  `blocked_goals`).
- **`told_at`**: for tasks and goals, the heartbeat's memory of the item
  (`internal/engine/heartbeat.go`, `hbItemMemory`, found by kind and entity ID in the heartbeat
  row's cursor); for loops, the later of `loopStreak.Notified` and the heartbeat's escalation of
  its `loop_error` item. For a group of tasks it is the earliest time any of them was escalated.

Everything quoted (`title`, `detail`, task titles) goes through `redactSecrets` with the
project's environment, is collapsed to one line and cut to its limit (invariant 8). The route is
read-only and returns nothing the reader could not already see task by task.

Errors: none of its own. A project removed from the config is skipped.

Code: handler and types in a new file `internal/api/problems.go`; the route line in
`internal/api/routes.go` (`s.read(r, "/problems", s.listProblems)`); a new method
`Problems(ctx context.Context) ([]Problem, error)` on `api.Controller` (one line in
`internal/api/controller.go`), implemented in a new file `internal/engine/problems.go`. The test
fake's method goes in the new test file.

## New stored fields

All three task fields live in the JSON document the task row already stores, so no migration.

| Field | Where | Type | Written |
|---|---|---|---|
| `blocked_cause` | `domain.Task` | string, optional | In `blockTaskFrom` (`internal/engine/helpers.go`). When the block comes from `requeueOrBlockFrom` giving up, it is the `why` argument without the "(gave up after N attempts)" ending, for example `workspace setup failed`. Any other block leaves it empty. |
| `blocked_detail` | `domain.Task` | string, optional | Same place: the `context` argument of `requeueOrBlockFrom`, which today is dropped on the last attempt (for workspace setup it is the `git fetch` error). Redacted with the project's environment and cut to its last 2,000 characters before it is stored. Empty for any other block. |
| `blocked_at` | `domain.Task` | time, optional | Same place: the time of the block. |
| `escalated_at` | `hbItemMemory` (heartbeat cursor JSON) | time, optional | Where the heartbeat sets `Escalated` (`internal/engine/heartbeat.go`). An item escalated before this change has no time, so its problem shows no "told you" until it clears. |

`blockTaskFrom` sets all three task fields on every block (empty when there is none), so an old
value never survives into a new block. `blocked_reason` and the notification text are unchanged.

## What the buttons call

No new write route. Both use the existing `POST /api/v1/tasks/{id}/retry` (action: operate).

| Button | Shown | Calls |
|---|---|---|
| **Retry** | a problem with one task | `POST /tasks/{id}/retry` |
| **Retry all N** | a problem with two or more tasks | `POST /tasks/{id}/retry` once per task in `tasks`, one after the other; the row reports how many failed and the first error |
| **Check again** | a problem with two or more tasks | `POST /tasks/{id}/retry` for one task, the one blocked most recently. If the cause is gone that task runs; if not it blocks again and `last_at` moves. |
| **Open**, **Open Loops**, **Open goal**, **Review** | | links: `/tasks/<id>` (or `/tasks?project=<p>&status=blocked` for a group), `/loops`, `/goals/<id>`, `/tasks/<task_id>` |

Person-only decisions: none is added. Retrying a task that was blocked by a denied tool call
already refuses the agent token (`auth.RetryDenied`, checked in `RetryTask`), and approving stays
`auth.ApproveMerge` on the task page.

## Events that refresh the page

A new query key `problems`, refetched when one of these arrives, and every 30 seconds like the
other lists:

| Event | Why |
|---|---|
| `task.updated` | a task was blocked, retried or cancelled |
| `goal.updated` | a goal was blocked or resumed |
| `loop.error` | a loop started failing or its error changed |
| `notification` | the owner was told (the heartbeat's escalation, the loop alert) |

A loop that recovers sends no event; the 30-second refetch picks it up. The other queries keep
their keys and events.

## Where this differs from the mockup, and why

1. **"Check again" retries one task.** The mockup promised a check that spends no attempt. That
   needs a way to re-run only the failed step, which exists for no cause today. Retrying one task
   of the group is the lean equivalent: for causes that fail before the agent starts (workspace
   setup, merging the base, copying attachments) it costs no model call.
2. **"checked 2m ago, still failing" becomes "last blocked 2m ago"** (`last_at`). No check state
   is stored.
3. **Row titles are the daemon's words.** The mockup wrote "Workspace setup fails: git fetch
   cannot reach the repository"; the page will say "Workspace setup failed" and show the error
   under it.
4. **The error line is the end of the error, not a tidy first line.** A loop's `last_error` is
   one string (name, command, output). Cutting it into parts would mean changing how every loop
   wraps its errors; keeping the last 300 characters shows the tool's own words.
5. **"told you" appears only when set; "not told yet" only on failing loops.** A blocked task and
   a new approval notify the owner at once, so "not told yet" would be untrue there. `told_at`
   records the later telling (the heartbeat's escalation), as in the mockup's times.

## Open points for the coordinator

1. Names: route `/problems`; fields `blocked_cause`, `blocked_detail`, `blocked_at`,
   `escalated_at`; kinds `blocked_tasks`, `failing_loops`, `blocked_goal`.
2. `RetryTask` (`internal/engine/controller.go`, not in my file list) clears `blocked_reason`
   only. I would clear the three new fields in the same statement so a re-queued task does not
   carry them: one line. Without it they are harmless (only blocked tasks are read) but untidy.
3. `api.Controller` gains one method, a one-line change in `internal/api/controller.go`.
