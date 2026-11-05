// The Add project form's checks and request body. The server validates everything again; these
// checks only save a round trip for the obvious mistakes.
import type { NewProjectRequest } from '../types';

/** The same rule as config.ProjectNamePattern in the Go code. */
export const PROJECT_NAME = /^[A-Za-z0-9][A-Za-z0-9_.-]*$/;
const REPO = /^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/;

export interface ProjectFormValues {
  name: string;
  repo: string;
  path: string;
  branch: string;
  policy: string;
  instructions: string;
}

export const emptyProjectForm: ProjectFormValues = {
  name: '',
  repo: '',
  path: '',
  branch: '',
  policy: '',
  instructions: '',
};

/** The first problem with the values, or null when they can be sent. */
export function projectFormError(v: ProjectFormValues, existing: string[]): string | null {
  const name = v.name.trim();
  if (!name) return 'A name is required.';
  if (!PROJECT_NAME.test(name)) return 'The name may use letters, digits, ".", "_" and "-".';
  if (existing.includes(name)) return `A project named ${name} already exists.`;
  const repo = v.repo.trim();
  if (!repo) return 'A repository is required.';
  if (!REPO.test(repo) && !repo.startsWith('/')) return 'The repository must be owner/name.';
  return null;
}

/** The request body: trimmed, with empty optional fields left out so the server keeps its defaults. */
export function projectFormRequest(v: ProjectFormValues): NewProjectRequest {
  const body: NewProjectRequest = { name: v.name.trim(), repo: v.repo.trim() };
  if (v.path.trim()) body.path = v.path.trim();
  if (v.branch.trim()) body.branch = v.branch.trim();
  if (v.policy) body.policy = v.policy;
  if (v.instructions.trim()) body.instructions = v.instructions;
  return body;
}
