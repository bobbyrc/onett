package main

import (
	"fmt"
	"os"
)

// insideAgentRun reports whether this process was started inside an agent session. Onett sets
// ONETT_RUN_ID and ONETT_ROLE for every agent it starts, workers and the orchestrator alike.
func insideAgentRun() bool {
	return os.Getenv("ONETT_RUN_ID") != "" || os.Getenv("ONETT_ROLE") != ""
}

// agentRunCommands are the commands that may run inside an agent run: the ones that call the
// API, where the server decides what the caller may do, plus doctor, version and help. The rest
// act with the owner's authority on this machine (print the owner token, reset a password, edit
// onett.yaml, change the service, run a command through run-shim), so an agent must not reach
// them through a pre-approved `onett ...` call. It is an allowlist: a new command is refused
// inside an agent run until it is added here.
var agentRunCommands = map[string]bool{
	"status": true, "projects": true, "agents": true, "config": true, "task": true,
	"goal": true, "run": true, "deploys": true, "approvals": true, "approve": true,
	"events": true, "loops": true, "loop": true, "project": true, "pause": true,
	"resume": true, "chat": true, "doctor": true, "version": true,
	"help": true, "-h": true, "-help": true, "--help": true,
}

// refuseInAgentRun returns an error when args names a command that must not run inside an
// agent run, and nil otherwise or outside one. `project add` is refused although `project
// pause` and `project resume` are not: it edits onett.yaml directly instead of calling the API.
// `serve` is refused for the orchestrator only: a worker that develops onett starts throwaway
// daemons to test its change, while for the orchestrator every `onett ...` call is pre-approved
// and a second daemon on the owner's data directory would run the scheduler twice.
func refuseInAgentRun(args []string) error {
	if !insideAgentRun() || len(args) == 0 {
		return nil
	}
	name := args[0]
	switch {
	case name == "project" && len(args) > 1 && args[1] == "add":
		name = "project add"
	case name == "serve" && os.Getenv("ONETT_ROLE") != "orchestrator":
		return nil
	case agentRunCommands[name]:
		return nil
	}
	return fmt.Errorf("`onett %s` does not run inside an agent run: it is for a person at a terminal", name)
}
