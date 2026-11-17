// The Add project dialog's values, its blocking checks and the request body. The server checks
// everything again; these checks only spare a round trip and say why Add is off.
import type { InspectResult, NewProjectRequest } from '../types';

/** The same rule as config.ProjectNamePattern in the Go code. */
export const PROJECT_NAME = /^[A-Za-z0-9][A-Za-z0-9_.-]*$/;

export type Policy = 'supervised' | 'default';

export interface AddProjectValues {
  name: string;
  branch: string;
  policy: Policy;
  instructions: string;
  setup: string[];
  checks: string[];
  /** "No setup or checks" is chosen: the project gets neither, and no command is required. */
  none: boolean;
}

/** What the person starts from: the check's name and branch, and the commands it guessed. */
export function initialValues(r: InspectResult): AddProjectValues {
  return {
    name: r.name,
    branch: r.default_branch,
    policy: 'supervised',
    instructions: '',
    setup: r.guess.setup.map((c) => c.run),
    checks: r.guess.checks.map((c) => c.run),
    none: false,
  };
}

/** The files the guessed commands came from, setup first, each once. Shown as "Guessed from …". */
export function guessedFrom(r: InspectResult): string[] {
  const files: string[] = [];
  for (const c of [...r.guess.setup, ...r.guess.checks]) {
    if (c.from && !files.includes(c.from)) files.push(c.from);
  }
  return files;
}

/** The reason Add is off, or null when the project can be added. The first reason that applies is shown. */
export function addBlocker(r: InspectResult, v: AddProjectValues, existing: string[]): string | null {
  if (!r.copy.ok) return 'onett adds a project only once it can copy it.';
  if (!r.default_branch && !v.branch.trim()) return 'onett could not read the default branch; set one under Advanced.';
  if (r.github && !r.pull_requests.ok) return 'onett cannot open pull requests until the GitHub CLI is signed in.';
  const name = v.name.trim();
  if (!name || !PROJECT_NAME.test(name)) return 'The name may use letters, digits, ".", "_" and "-".';
  if (existing.includes(name)) return `A project named ${name} already exists.`;
  const commands = [...v.setup, ...v.checks].some((c) => c.trim() !== '');
  if (!v.none && !commands) return 'Fill in a command, or choose no setup or checks.';
  return null;
}

/** The commands as the request sends them: trimmed, blanks and repeats dropped. */
function commands(list: string[]): { name: string; run: string }[] {
  const runs = [...new Set(list.map((c) => c.trim()).filter(Boolean))];
  return runs.map((run) => ({ name: run, run }));
}

/** The POST /projects body. Fields the person did not change are left out so the server keeps its own. */
export function addProjectRequest(r: InspectResult, v: AddProjectValues): NewProjectRequest {
  const body: NewProjectRequest = { repo: r.repo, policy: v.policy };
  const name = v.name.trim();
  if (name !== r.name) body.name = name;
  const branch = v.branch.trim();
  if (branch) body.branch = branch;
  if (v.instructions.trim()) body.instructions = v.instructions;
  if (!v.none) {
    body.setup = commands(v.setup);
    body.checks = commands(v.checks);
  }
  return body;
}

/** The first task offered after a project is added: a question, so no pull request is opened. */
export const firstTaskTitle = (name: string): string => `Explain how ${name} is built and tested`;

export const FIRST_TASK_BODY =
  'Read the repository and explain how it is built, tested and laid out. List anything an agent would need to know that is not written down. Do not change any files.';

export const POLICY_SENTENCE: Record<Policy, string> = {
  supervised:
    'Small, low-risk changes merge on their own once the reviewing agent approves and CI passes. Bigger ones, and any change to dependencies, CI, migrations or auth, wait for you.',
  default: 'Every change merges on its own once the reviewing agent approves and CI passes, whatever its risk.',
};
