import { describe, expect, it } from 'vitest';
import { reflowPrompt } from './reflowPrompt';

describe('reflowPrompt', () => {
  it('joins soft-wrapped lines inside a paragraph and keeps paragraph breaks', () => {
    expect(reflowPrompt('one two\nthree four\n\nfive\nsix')).toBe('one two three four\n\nfive six');
  });

  it('keeps list items apart and folds their continuation lines in', () => {
    const src = '1. first item\n   continues here\n2. second\n- bullet\n  more';
    expect(reflowPrompt(src)).toBe('1. first item continues here\n2. second\n- bullet more');
  });

  it('leaves fenced code blocks untouched', () => {
    const src = 'Run:\n```sh\nnpm test\n  --watch\n```\nafter\ntext';
    expect(reflowPrompt(src)).toBe('Run:\n```sh\nnpm test\n  --watch\n```\nafter text');
  });

  it('does not merge headings, quotes or tables into prose', () => {
    const src = '# Title\nbody\nmore\n> quote\n> again\n| a | b |\n| - | - |\nend';
    expect(reflowPrompt(src)).toBe('# Title\nbody more\n> quote\n> again\n| a | b |\n| - | - |\nend');
  });

  it('returns text without wrapping unchanged', () => {
    expect(reflowPrompt('')).toBe('');
    expect(reflowPrompt('a\n\nb')).toBe('a\n\nb');
  });
});
