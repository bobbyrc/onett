import type { CSSProperties, ReactNode } from 'react';
import { cx } from '../lib/format';
import { Empty } from './Empty';
import './Table.css';

export interface Column<T> {
  key: string;
  header: string;
  cell: (row: T) => ReactNode;
  /** A free-text column (a title, notes, a URL). It takes the width the other cells leave and may
   * wrap; every other cell stays on one line. */
  wide?: boolean;
  /** A fixed width in rem at table width (760px and up). When any column has one, the table's
   * layout is fixed: tables with the same columns then line up, and the `wide` column takes
   * what is left. Give every column but the `wide` one a width, or none. */
  width?: number;
}

/** Room kept for the free-text column of a fixed-width table before the table scrolls sideways. */
const WIDE_MIN_REM = 13;

/**
 * A table that the stylesheet turns into stacked cards under 760px. Each cell carries its header
 * as data-label, and the first column becomes the card's title row. A column `width` fixes the
 * layout so that tables with the same columns line up.
 */
export function Table<T>({
  columns,
  rows,
  rowKey,
  empty,
}: {
  columns: Column<T>[];
  rows: T[];
  rowKey: (row: T) => string;
  empty: ReactNode;
}) {
  if (rows.length === 0) return <Empty>{empty}</Empty>;
  const fixed = columns.some((column) => column.width !== undefined);
  const setWidth = columns.reduce((sum, column) => sum + (column.width ?? 0), 0);
  const style = fixed ? ({ '--table-min-width': `${setWidth + WIDE_MIN_REM}rem` } as CSSProperties) : undefined;
  return (
    <div className="table-wrap">
      <table className={cx('table', fixed && 'table-fixed')} style={style}>
        {fixed ? (
          <colgroup>
            {columns.map((column) => (
              <col key={column.key} style={column.width !== undefined ? { width: `${column.width}rem` } : undefined} />
            ))}
          </colgroup>
        ) : null}
        <thead>
          <tr>
            {columns.map((column) => (
              <th key={column.key} scope="col" className={column.wide ? 'cell-wide' : undefined}>
                {column.header}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr key={rowKey(row)}>
              {columns.map((column) => {
                const content = column.cell(row);
                // Plain text (a role, a project) is truncated with an ellipsis when too long, and
                // the title shows it whole.
                const text = typeof content === 'string' && !column.wide;
                return (
                  <td
                    key={column.key}
                    data-label={column.header}
                    className={column.wide ? 'cell-wide' : text ? 'cell-text' : undefined}
                    title={text ? content : undefined}
                  >
                    {content}
                  </td>
                );
              })}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
