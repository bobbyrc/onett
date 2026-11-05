// The nav groups must place every setting in exactly one group, so a new top-level key in the
// schema cannot vanish from the Config page. The schema is a fixture written by a Go test from
// the real config.Schema() (internal/config/schema_defaults_test.go).
import { describe, expect, it } from 'vitest';
import type { SchemaNode } from '../types';
import schemaJson from './configSchema.fixture.json';
import { CONFIG_GROUPS, groupOfPath, groupSections, resolveGroup } from './configGroups';

const schema = schemaJson as unknown as SchemaNode;
const topKeys = (schema.fields ?? []).map((f) => f.key);

function nodeAt(path: string[]): SchemaNode | undefined {
  let node: SchemaNode | undefined = schema;
  for (const key of path) node = node?.fields?.find((f) => f.key === key);
  return node;
}

describe('config nav groups', () => {
  it('lists only paths that exist in the schema', () => {
    for (const group of CONFIG_GROUPS) {
      for (const path of group.paths) expect(nodeAt(path.split('.')), `${group.id}: ${path}`).toBeDefined();
    }
  });

  it('lists no path twice', () => {
    const all = CONFIG_GROUPS.flatMap((g) => g.paths);
    expect(all.filter((p, i) => all.indexOf(p) !== i)).toEqual([]);
  });

  it('places every top-level key whole in exactly one group', () => {
    for (const key of topKeys) {
      const owners = CONFIG_GROUPS.filter((g) => g.paths.includes(key)).map((g) => g.id);
      expect(owners, `top-level key ${key} must be listed whole by exactly one group`).toHaveLength(1);
    }
  });

  it('shows every leaf setting in exactly one group', () => {
    const leaves: string[] = [];
    const walk = (node: SchemaNode, path: string[]) => {
      if (node.type === 'section' && node.fields) node.fields.forEach((f) => walk(f, [...path, f.key]));
      else leaves.push(path.join('.'));
    };
    walk(schema, []);
    for (const leaf of leaves) {
      const shown = CONFIG_GROUPS.filter((g) =>
        groupSections(g, schema).some((s) => {
          let node: SchemaNode | undefined = s.node;
          for (const key of leaf.split('.').slice(s.path.length)) node = node?.fields?.find((f) => f.key === key);
          return leaf.startsWith(s.path.join('.')) && node !== undefined;
        }),
      );
      expect(shown.map((g) => g.id), leaf).toHaveLength(1);
    }
  });

  it('splits loops and limits the way the owner thinks', () => {
    const shown = (id: string) =>
      groupSections(CONFIG_GROUPS.find((g) => g.id === id)!, schema).flatMap((s) => [
        s.path.join('.'),
        ...(s.node.fields ?? []).map((f) => `${s.path.join('.')}.${f.key}`),
      ]);
    expect(shown('loops')).not.toContain('loops.goals');
    expect(shown('loops')).not.toContain('loops.self_update');
    expect(shown('loops')).toContain('loops.friction');
    expect(shown('limits')).not.toContain('limits.max_goal_turns');
    expect(shown('limits')).toContain('limits.stall_timeout');
    expect(shown('goals')).toEqual(expect.arrayContaining(['loops.goals', 'limits.max_goal_turns']));
    expect(shown('self-update')).toEqual(expect.arrayContaining(['loops.self_update', 'self_project']));
  });

  it('finds the group of a path, longest listed prefix first', () => {
    expect(groupOfPath(['loops', 'goals', 'every'])?.id).toBe('goals');
    expect(groupOfPath(['loops', 'retro'])?.id).toBe('loops');
    expect(groupOfPath(['limits', 'max_goal_turns'])?.id).toBe('goals');
    expect(groupOfPath(['notify', '0', 'type'])?.id).toBe('channels');
  });

  it('maps old ?section= keys to their group', () => {
    expect(resolveGroup('routing').id).toBe('agents');
    expect(resolveGroup('notify').id).toBe('channels');
    expect(resolveGroup('self_project').id).toBe('self-update');
    expect(resolveGroup('projects').id).toBe('projects');
    expect(resolveGroup('').id).toBe('projects');
    expect(resolveGroup('users').link).toBeUndefined();
  });
});
