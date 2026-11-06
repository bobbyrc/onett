// @vitest-environment node
import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';

const css = readFileSync(new URL('../styles.css', import.meta.url), 'utf8');

describe('styles.css selectors', () => {
  it('styles .cell-note at top level, since Deploys renders it outside .pause-switch', () => {
    expect(css).toMatch(/^\.cell-note \{/m);
    expect(css).not.toMatch(/\.pause-switch \.cell-note/);
  });

  it('keeps the pause switch error colour scoped to .pause-switch', () => {
    expect(css).toMatch(/^\.pause-switch \.error-line \{[^}]*flex-basis: 100%/m);
  });
});
