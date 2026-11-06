// Turns the owner's agent actions into config-file changes. The Agents page sends these lists to
// the config document endpoint; nothing here touches the network or the DOM. A file entry is
// the raw `agents.<name>` mapping from the file (not the computed AgentView), or undefined when
// the file does not name the agent.
import type { AgentView, ConfigChange } from '../types';

export type AgentEntry = Record<string, unknown>;

/** The fields of the agent editor. `promptMode` says which of the two prompt fields is in use. */
export interface AgentForm {
  name: string;
  title: string;
  description: string;
  /** One path per entry. */
  context: string[];
  /** One tool rule per entry. */
  tools: string[];
  harness: string;
  model: string;
  effort: string;
  permissionMode: string;
  args: string[];
  promptMode: 'ref' | 'text';
  promptRef: string;
  promptText: string;
}

/** The editor's starting values for an existing agent. */
export function formFromAgent(a: AgentView): AgentForm {
  const inline = a.prompt === '';
  return {
    name: a.name,
    title: a.title ?? '',
    description: a.description,
    context: [...(a.context ?? [])],
    tools: [...(a.tools ?? [])],
    harness: a.harness,
    model: a.model,
    effort: a.effort ?? '',
    permissionMode: a.permission_mode ?? '',
    args: [...a.args],
    promptMode: inline ? 'text' : 'ref',
    promptRef: inline ? '' : a.prompt,
    promptText: a.prompt_text,
  };
}

/**
 * The editor's starting values from the file's own entry for `name`. The entry holds the settings
 * as written: a relative prompt path, and no masks. `view` only supplies the current text, which
 * "Write the prompt here" starts from when the entry names a file.
 */
export function formFromEntry(name: string, entry: AgentEntry, view: AgentView | undefined): AgentForm {
  const text = (key: string): string => (typeof entry[key] === 'string' ? (entry[key] as string) : '');
  const strings = (key: string): string[] => (Array.isArray(entry[key]) ? (entry[key] as unknown[]).map(String) : []);
  const inline = typeof entry.prompt_text === 'string';
  return {
    name,
    title: text('title'),
    description: text('description'),
    context: strings('context'),
    tools: strings('tools'),
    harness: text('harness'),
    model: text('model'),
    effort: text('effort'),
    permissionMode: text('permission_mode'),
    args: Array.isArray(entry.args) ? entry.args.map(String) : [],
    promptMode: inline ? 'text' : 'ref',
    promptRef: text('prompt'),
    promptText: inline ? text('prompt_text') : (view?.prompt_text ?? ''),
  };
}

/** An empty form for a new agent. */
export function blankForm(): AgentForm {
  return {
    name: '',
    title: '',
    description: '',
    context: [],
    tools: [],
    harness: '',
    model: '',
    effort: '',
    permissionMode: '',
    args: [],
    promptMode: 'text',
    promptRef: '',
    promptText: '',
  };
}

/** One `set` of the whole agent entry, from what the editor shows. Empty optional fields are left out. */
export function saveChanges(form: AgentForm): ConfigChange[] {
  const value: Record<string, unknown> = { harness: form.harness.trim() };
  if (form.model.trim()) value.model = form.model.trim();
  if (form.title.trim()) value.title = form.title.trim();
  if (form.effort.trim()) value.effort = form.effort.trim();
  if (form.permissionMode.trim()) value.permission_mode = form.permissionMode.trim();
  if (form.description.trim()) value.description = form.description.trim();
  const context = form.context.map((line) => line.trim()).filter(Boolean);
  if (context.length > 0) value.context = context;
  const tools = form.tools.map((line) => line.trim()).filter(Boolean);
  if (tools.length > 0) value.tools = tools;
  if (form.args.length > 0) value.args = form.args;
  if (form.promptMode === 'text') {
    value.prompt_text = form.promptText;
  } else {
    value.prompt = form.promptRef.trim();
  }
  return [{ op: 'set', path: ['agents', form.name], value }];
}

/** Whether the file names `name` under `agents`. */
export function hasEntry(fileAgents: Record<string, AgentEntry>, name: string): boolean {
  return Object.prototype.hasOwnProperty.call(fileAgents, name);
}

/**
 * Deletes the file entry for `name`. When it is the only entry under `agents`, the whole `agents`
 * mapping goes, so the file never keeps an empty `agents: {}`.
 */
function deleteEntry(name: string, fileAgents: Record<string, AgentEntry>): ConfigChange {
  const onlyEntry = Object.keys(fileAgents).length === 1 && hasEntry(fileAgents, name);
  return onlyEntry ? { op: 'delete', path: ['agents'] } : { op: 'delete', path: ['agents', name] };
}

/** The routing the file writes: the global `routing:` and each project's own `routing:`. */
export interface FileRouting {
  routing: Record<string, string>;
  projects: Record<string, Record<string, string>>;
  /** The built-in agent for each kind, from the server, which a kind falls back to. */
  defaults: Record<string, string>;
}

/** One route in the file that names an agent, and what the kind uses once it is dropped. */
export interface RouteRef {
  kind: string;
  /** The project whose routing holds it; undefined for the global routing. */
  project?: string;
  /** The agent the kind falls back to: the built-in default, or the global route for a project. */
  fallback: string;
}

/** The routing in a config document (`document.routing` and `document.projects.<p>.routing`). */
export function fileRouting(document: unknown, defaults: Record<string, string> = {}): FileRouting {
  const doc = (document ?? {}) as { routing?: Record<string, string>; projects?: Record<string, { routing?: Record<string, string> }> };
  const projects: Record<string, Record<string, string>> = {};
  for (const [name, p] of Object.entries(doc.projects ?? {})) {
    if (p?.routing && Object.keys(p.routing).length > 0) projects[name] = p.routing;
  }
  return { routing: doc.routing ?? {}, projects, defaults };
}

/** Every route in the file that names `agent`. Removing the agent has to drop these too. */
export function routesNaming(agent: string, file: FileRouting): RouteRef[] {
  const refs: RouteRef[] = [];
  for (const [kind, target] of Object.entries(file.routing)) {
    if (target === agent) refs.push({ kind, fallback: file.defaults[kind] ?? '' });
  }
  for (const [project, routes] of Object.entries(file.projects)) {
    for (const [kind, target] of Object.entries(routes)) {
      if (target !== agent) continue;
      const global = file.routing[kind];
      refs.push({ kind, project, fallback: global && global !== agent ? global : (file.defaults[kind] ?? '') });
    }
  }
  return refs;
}

/** The sentence the card shows before Remove/Delete when routes name the agent. */
export function routeDropNote(refs: RouteRef[]): string {
  const parts = refs.map((r) => `${r.kind}${r.project ? ` in ${r.project}` : ''}${r.fallback ? ` → ${r.fallback}` : ''}`);
  return `Routing names this agent for ${parts.join(', ')}. Removing it also drops these routes; each kind then uses the agent after the arrow.`;
}

/** Deletes the routes in `refs`; a mapping left with no entries is deleted whole, as for `agents`. */
function dropRouteChanges(refs: RouteRef[], file: FileRouting): ConfigChange[] {
  const changes: ConfigChange[] = [];
  const groups = new Map<string | undefined, string[]>();
  for (const r of refs) groups.set(r.project, [...(groups.get(r.project) ?? []), r.kind]);
  for (const [project, kinds] of groups) {
    const routes = project === undefined ? file.routing : file.projects[project];
    const base = project === undefined ? [] : ['projects', project];
    if (kinds.length >= Object.keys(routes).length) {
      changes.push({ op: 'delete', path: [...base, 'routing'] });
    } else {
      for (const kind of kinds) changes.push({ op: 'delete', path: [...base, 'routing', kind] });
    }
  }
  return changes;
}

/**
 * Takes an agent out of use. A custom agent is deleted, since nothing would bring it back. A
 * built-in with no file entry gets an entry that only disables it; an edited one keeps its
 * entry and gets `disabled` set, so its changes survive a restore. Routes in the file that name
 * the agent are dropped in the same change: the server refuses a config whose routing names an
 * unknown or disabled agent, and those kinds then fall back to the default routing.
 */
export function removeChanges(
  a: AgentView,
  fileAgents: Record<string, AgentEntry>,
  routing: FileRouting = { routing: {}, projects: {}, defaults: {} },
): ConfigChange[] {
  const drops = dropRouteChanges(routesNaming(a.name, routing), routing);
  if (a.origin === 'custom') return [...drops, deleteEntry(a.name, fileAgents)];
  if (!hasEntry(fileAgents, a.name)) {
    return [...drops, { op: 'set', path: ['agents', a.name], value: { disabled: true } }];
  }
  return [...drops, { op: 'set', path: ['agents', a.name, 'disabled'], value: true }];
}

/**
 * Puts a removed agent back. An entry that only disabled a built-in is deleted, so the built-in
 * returns as it was; any other entry keeps its settings and loses only the `disabled` flag.
 */
export function restoreChanges(name: string, fileAgents: Record<string, AgentEntry>): ConfigChange[] {
  if (!hasEntry(fileAgents, name)) return [];
  const otherKeys = Object.keys(fileAgents[name]).filter((key) => key !== 'disabled');
  if (otherKeys.length === 0) return [deleteEntry(name, fileAgents)];
  return [{ op: 'delete', path: ['agents', name, 'disabled'] }];
}

/** Drops the file entry for a built-in, so it returns to the built-in definition. */
export function resetChanges(name: string, fileAgents: Record<string, AgentEntry>): ConfigChange[] {
  return [deleteEntry(name, fileAgents)];
}
