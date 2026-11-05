// How the Config editor's left nav groups the settings: the one table that says which schema
// paths each group shows. A path is the YAML keys joined by dots. A group lists a whole top-level
// key ("projects") or a part of one ("loops.goals"); a key that other groups take parts of is
// listed whole by exactly one group, which then shows the rest of it.
import type { SchemaNode } from '../types';
import { isUnder, type Path } from './configDoc';

export interface ConfigGroup {
  /** The ?section= value. */
  id: string;
  label: string;
  /** Schema paths, dotted, shown in this order as sub-sections. */
  paths: string[];
  /** Instead of settings, the nav entry links to this page. */
  link?: string;
}

export const CONFIG_GROUPS: ConfigGroup[] = [
  { id: 'projects', label: 'Projects', paths: ['projects'] },
  { id: 'agents', label: 'Agents & routing', paths: ['agents', 'routing', 'harnesses'] },
  { id: 'policies', label: 'Policies', paths: ['policies'] },
  { id: 'limits', label: 'Limits & budget', paths: ['limits'] },
  { id: 'loops', label: 'Loops', paths: ['loops'] },
  { id: 'goals', label: 'Goals', paths: ['loops.goals', 'limits.max_goal_turns'] },
  { id: 'self-update', label: 'Self-update', paths: ['loops.self_update', 'self_project'] },
  { id: 'channels', label: 'Channels & notifiers', paths: ['channels', 'notify'] },
  { id: 'chat', label: 'Chat', paths: ['chat'] },
  { id: 'server', label: 'Server', paths: ['server'] },
  // Users are not in onett.yaml; they have their own page.
  { id: 'users', label: 'Users', paths: [], link: '/users' },
];

const split = (path: string): Path => path.split('.');

/** The group whose listed path is the longest one containing `path`, or undefined. */
export function groupOfPath(path: Path, groups: ConfigGroup[] = CONFIG_GROUPS): ConfigGroup | undefined {
  let best: ConfigGroup | undefined;
  let bestLen = -1;
  for (const group of groups) {
    for (const listed of group.paths) {
      const prefix = split(listed);
      if (prefix.length > bestLen && isUnder(path, prefix)) {
        best = group;
        bestLen = prefix.length;
      }
    }
  }
  return best;
}

/**
 * The group a ?section= value names: a group id, or an older top-level key such as "routing" or
 * "notify" (links from before the nav was grouped). Falls back to the first group.
 */
export function resolveGroup(section: string, groups: ConfigGroup[] = CONFIG_GROUPS): ConfigGroup {
  const byId = groups.find((g) => g.id === section && !g.link);
  return byId ?? groupOfPath([section], groups) ?? groups[0];
}

/** A schema node together with the path to it, ready for NodeEditor. */
export interface GroupSection {
  path: Path;
  node: SchemaNode;
}

/**
 * The sections a group shows. A listed path that other groups take parts of ("loops") loses those
 * parts. A path missing from the schema is skipped.
 */
export function groupSections(
  group: ConfigGroup,
  schema: SchemaNode,
  groups: ConfigGroup[] = CONFIG_GROUPS,
): GroupSection[] {
  const sections: GroupSection[] = [];
  for (const listed of group.paths) {
    const path = split(listed);
    const node = withoutOthers(nodeAt(schema, path), path, group, groups);
    if (node) sections.push({ path, node });
  }
  return sections;
}

function nodeAt(schema: SchemaNode, path: Path): SchemaNode | undefined {
  let node: SchemaNode | undefined = schema;
  for (const key of path) node = node?.fields?.find((f) => f.key === key);
  return node;
}

/** `node` (at `path`) without the descendants that another group lists. */
function withoutOthers(
  node: SchemaNode | undefined,
  path: Path,
  group: ConfigGroup,
  groups: ConfigGroup[],
): SchemaNode | undefined {
  if (!node?.fields) return node;
  const fields = node.fields.filter((field) => {
    const owner = groupOfPath([...path, field.key], groups);
    return owner === undefined || owner === group;
  });
  return { ...node, fields };
}
