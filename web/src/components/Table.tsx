import type { ReactNode } from 'react';
import { Empty } from './Empty';

export interface Column<T> {
  key: string;
  header: string;
  cell: (row: T) => ReactNode;
  /** A free-text column (a title, notes, a URL). It takes the width the other cells leave and may
   * wrap; every other cell stays on one line. */
  wide?: boolean;
}

/**
 * A table that the stylesheet turns into stacked cards under 760px. Each cell carries its header
 * as data-label, and the first column becomes the card's title row.
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
  return (
    <div className="table-wrap">
      <table className="table">
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
