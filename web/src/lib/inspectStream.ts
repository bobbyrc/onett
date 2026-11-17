// Reads the answer of POST /projects/inspect: one JSON object per line (NDJSON), each a step,
// the final result, or an error. The network can cut a line anywhere, so lines are joined here.
import type { InspectLine } from '../types';

/** Splits streamed text into whole lines. Text after the last newline waits for the next push. */
export class LineSplitter {
  private rest = '';

  /** The complete lines in this chunk, without their newline. Blank lines are dropped. */
  push(chunk: string): string[] {
    const parts = (this.rest + chunk).split('\n');
    this.rest = parts.pop() ?? '';
    return parts.filter((line) => line.trim() !== '');
  }

  /** The text left when the stream ends, as a line if it is not blank. */
  end(): string[] {
    const last = this.rest;
    this.rest = '';
    return last.trim() === '' ? [] : [last];
  }
}

/** Parses one line. A line must hold exactly one of step, result or error. */
export function parseInspectLine(line: string): InspectLine {
  let value: unknown;
  try {
    value = JSON.parse(line);
  } catch {
    throw new Error('unexpected line from the server');
  }
  if (value === null || typeof value !== 'object' || Array.isArray(value)) {
    throw new Error('unexpected line from the server');
  }
  const keys = Object.keys(value);
  if (keys.length !== 1 || !['step', 'result', 'error'].includes(keys[0])) {
    throw new Error('unexpected line from the server');
  }
  return value as InspectLine;
}
