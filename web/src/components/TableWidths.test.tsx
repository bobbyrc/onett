// @vitest-environment jsdom
import { cleanup, render } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { Table, type Column } from './Table';

afterEach(cleanup);

type Row = { id: string; name: string };
const rows: Row[] = [{ id: 'a', name: 'Alpha' }];
const show = (columns: Column<Row>[]) => render(<Table columns={columns} rows={rows} rowKey={(r) => r.id} empty="none" />);

describe('Table column widths', () => {
  it('fixes the layout and sets each width when columns carry one', () => {
    show([
      { key: 'id', header: 'ID', width: 5, cell: (r) => r.id },
      { key: 'name', header: 'Name', wide: true, cell: (r) => r.name },
      { key: 'again', header: 'Again', width: 7.5, cell: (r) => r.id },
    ]);
    const table = document.querySelector('table');
    expect(table?.classList.contains('table-fixed')).toBe(true);
    const cols = [...document.querySelectorAll('colgroup col')] as HTMLElement[];
    expect(cols.map((c) => c.style.width)).toEqual(['5rem', '', '7.5rem']);
    // The set widths plus room for the free-text column: under this the table scrolls sideways.
    expect(table?.style.getPropertyValue('--table-min-width')).toBe('25.5rem');
  });

  it('leaves a table with no widths as it was', () => {
    show([
      { key: 'id', header: 'ID', cell: (r) => r.id },
      { key: 'name', header: 'Name', wide: true, cell: (r) => r.name },
    ]);
    const table = document.querySelector('table');
    expect(table?.classList.contains('table-fixed')).toBe(false);
    expect(document.querySelector('colgroup')).toBeNull();
    expect(table?.getAttribute('style')).toBeNull();
  });
});
