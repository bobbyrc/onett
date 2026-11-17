import { describe, expect, it } from 'vitest';
import { LineSplitter, parseInspectLine } from './inspectStream';

describe('LineSplitter', () => {
  it('joins a line that arrives in two chunks', () => {
    const s = new LineSplitter();
    expect(s.push('{"step":{"name":"copy"')).toEqual([]);
    expect(s.push(',"state":"running"}}\n')).toEqual(['{"step":{"name":"copy","state":"running"}}']);
  });

  it('returns every complete line in one chunk and keeps the partial tail', () => {
    const s = new LineSplitter();
    expect(s.push('{"error":"a"}\n{"error":"b"}\n{"err')).toEqual(['{"error":"a"}', '{"error":"b"}']);
    expect(s.push('or":"c"}\n')).toEqual(['{"error":"c"}']);
  });

  it('drops blank lines', () => {
    const s = new LineSplitter();
    expect(s.push('\n\n{"error":"a"}\n\n')).toEqual(['{"error":"a"}']);
  });

  it('returns a last line that has no newline from end()', () => {
    const s = new LineSplitter();
    expect(s.push('{"error":"a"}\n{"result":')).toEqual(['{"error":"a"}']);
    expect(s.end()).toEqual(['{"result":']);
    expect(s.end()).toEqual([]);
  });

  it('keeps multi-byte text whole when it is split across chunks as strings', () => {
    const s = new LineSplitter();
    expect(s.push('{"error":"caf')).toEqual([]);
    expect(s.push('é ✓"}\n')).toEqual(['{"error":"café ✓"}']);
  });
});

describe('parseInspectLine', () => {
  it('parses a step', () => {
    expect(parseInspectLine('{"step":{"name":"copy","state":"failed","detail":"no"}}')).toEqual({
      step: { name: 'copy', state: 'failed', detail: 'no' },
    });
  });

  it('parses a result and an error', () => {
    expect(parseInspectLine('{"result":{"name":"web"}}')).toEqual({ result: { name: 'web' } });
    expect(parseInspectLine('{"error":"gh went away"}')).toEqual({ error: 'gh went away' });
  });

  it('refuses a line that is not exactly one of the three kinds', () => {
    expect(() => parseInspectLine('not json')).toThrow('unexpected line from the server');
    expect(() => parseInspectLine('3')).toThrow('unexpected line from the server');
    expect(() => parseInspectLine('{}')).toThrow('unexpected line from the server');
    expect(() => parseInspectLine('{"error":"a","result":{}}')).toThrow('unexpected line from the server');
  });
});
