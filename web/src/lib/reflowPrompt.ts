// Role prompts are markdown hard-wrapped at ~100 columns. Shown in a box of another width, the
// browser wraps them a second time and the lines alternate long and short. This joins each
// soft-wrapped paragraph or list item into one line and leaves everything else as written.

const FENCE = /^\s*(```|~~~)/;
const LIST_ITEM = /^\s*(?:[-*+]|\d+[.)])\s/;
// Lines that always start their own line: headings, quotes, table rows, rules, indented code.
const STANDALONE = /^(?:#{1,6}\s|>|\||(?:-{3,}|\*{3,}|_{3,})\s*$|(?: {4}|\t))/;

/** Joins soft-wrapped lines within paragraphs and list items; blank lines, headings, fenced code
 * blocks, tables and quotes keep their line breaks. */
export function reflowPrompt(text: string): string {
  const out: string[] = [];
  let inFence = false;
  // True when the last output line is prose that the next plain line continues.
  let open = false;
  for (const line of text.split('\n')) {
    if (FENCE.test(line)) {
      inFence = !inFence;
      out.push(line);
      open = false;
      continue;
    }
    if (inFence) {
      out.push(line);
      continue;
    }
    if (line.trim() === '') {
      out.push('');
      open = false;
      continue;
    }
    const startsNew = LIST_ITEM.test(line) || STANDALONE.test(line);
    if (open && !startsNew) {
      out[out.length - 1] += ' ' + line.trim();
      continue;
    }
    out.push(line);
    // Headings, quotes, tables and rules never take a continuation; prose and list items do.
    open = LIST_ITEM.test(line) || !STANDALONE.test(line);
  }
  return out.join('\n');
}
