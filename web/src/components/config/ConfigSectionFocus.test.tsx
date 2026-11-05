// @vitest-environment jsdom
// A top-level map takes the link's ?entry= name: that card is outlined and scrolled into view.
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import type { SchemaNode } from '../../types';
import { NodeEditor, type EditContext } from './ConfigSection';

const projects: SchemaNode = {
  key: 'projects',
  type: 'map',
  item: { key: 'project', type: 'section', fields: [{ key: 'repo', type: 'string' }] },
};

let root: Root | undefined;
let host: HTMLElement;

beforeAll(() => {
  (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
});

afterEach(() => {
  act(() => root?.unmount());
  host?.remove();
});

function render(focusEntry?: string) {
  const ctx: EditContext = {
    draft: { projects: { onett: { repo: 'a' }, other: { repo: 'b' } } },
    problems: new Map(),
    setValue: () => {},
    clearValue: () => {},
    focusEntry,
  };
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
  act(() => root?.render(<NodeEditor node={projects} path={['projects']} ctx={ctx} nested={false} />));
}

describe('focusEntry', () => {
  it('outlines and scrolls to the named entry only', () => {
    const scroll = vi.fn();
    Element.prototype.scrollIntoView = scroll;
    render('onett');
    const focused = host.querySelectorAll('.config-entry-focus');
    expect(focused).toHaveLength(1);
    expect(focused[0].id).toBe('config-entry-onett');
    expect(scroll).toHaveBeenCalledTimes(1);
  });

  it('ignores a name the map does not have', () => {
    render('missing');
    expect(host.querySelector('.config-entry-focus')).toBeNull();
  });
});
