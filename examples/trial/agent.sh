#!/bin/sh
# A scripted stand-in for a coding agent, for trying onett without a paid agent CLI.
#
# Onett's exec harness runs this in the task's worktree with the prompt on stdin (protocol:
# docs/plugins.md). It uses no network and no credential. As the code reviewer it approves; as
# any other role it adds one line to TRIAL.md. Like every agent it reports through
# $ONETT_OUTBOX/result.json and never commits or pushes: onett does that.
set -eu

cat >/dev/null # the prompt; this script does the same thing whatever the task says
mkdir -p "$ONETT_OUTBOX"

if [ "${ONETT_ROLE:-}" = "code-reviewer" ]; then
	echo "trial agent: approving the pull request"
	printf '%s\n' '{"status":"completed","summary":"Scripted review: approved.","risk":"low","review":{"approve":true}}' >"$ONETT_OUTBOX/result.json"
	exit 0
fi

echo "trial agent: adding a line to TRIAL.md"
echo "The onett trial agent ran task ${ONETT_TASK_ID:-unknown}." >>TRIAL.md
printf '%s\n' '{"status":"completed","summary":"Added a line to TRIAL.md.","risk":"low","pr_title":"Add a line to TRIAL.md","pr_body":"Written by the scripted trial agent in examples/trial."}' >"$ONETT_OUTBOX/result.json"
