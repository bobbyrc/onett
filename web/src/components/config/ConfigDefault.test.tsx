// @vitest-environment jsdom
// A bool setting with a schema default shows it, so an unset toggle is not read as "off".
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { beforeAll, expect, it } from 'vitest';
import type { SchemaNode } from '../../types';
import { NodeEditor, type EditContext } from './ConfigSection';

beforeAll(() => {
  (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
});

function text(node: SchemaNode): string {
  const ctx: EditContext = {
    draft: {},
    problems: new Map(),
    setValue: () => {},
    clearValue: () => {},
  };
  const host = document.createElement('div');
  document.body.append(host);
  const root = createRoot(host);
  act(() =>
    root.render(
      <QueryClientProvider client={new QueryClient()}>
        <NodeEditor node={node} path={[node.key]} ctx={ctx} nested={true} />
      </QueryClientProvider>,
    ),
  );
  const out = host.textContent ?? '';
  act(() => root.unmount());
  host.remove();
  return out;
}

it('shows the default of a bool setting', () => {
  expect(text({ key: 'enabled', type: 'bool', default: true })).toContain('Default: on');
  expect(text({ key: 'enabled', type: 'bool', default: false })).toContain('Default: off');
});

it('shows nothing when the schema has no default', () => {
  expect(text({ key: 'enabled', type: 'bool' })).not.toContain('Default');
});
