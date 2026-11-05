// @vitest-environment jsdom
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { Markdown } from './Markdown';

const fence = (lang: string, body: string) => '```' + lang + '\n' + body + '\n```';

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe('code blocks', () => {
  it('labels the block with its language, or "text"', () => {
    const { container } = render(<Markdown text={fence('go', 'package main') + '\n\n' + fence('', 'plain')} />);
    const labels = Array.from(container.querySelectorAll('.code-block-lang')).map((e) => e.textContent);
    expect(labels).toEqual(['go', 'text']);
  });

  it('copies the exact raw code, not HTML', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true });
    const code = 'if a < b && c > d {\n\treturn "<b>x</b>"\n}';
    render(<Markdown text={fence('go', code)} />);
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /^Copy/ }));
    });
    expect(writeText).toHaveBeenCalledWith(code + '\n');
    expect(screen.getByRole('status').textContent).toBe('Copied');
  });

  it('clears the Copied message after about 1.5 seconds', async () => {
    vi.useFakeTimers();
    Object.defineProperty(navigator, 'clipboard', { value: { writeText: vi.fn().mockResolvedValue(undefined) }, configurable: true });
    render(<Markdown text={fence('sh', 'ls')} />);
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /^Copy/ }));
    });
    expect(screen.getByRole('status').textContent).toBe('Copied');
    await act(async () => {
      vi.advanceTimersByTime(1600);
    });
    expect(screen.getByRole('status').textContent).toBe('');
  });

  it('falls back to execCommand when the clipboard API is missing', async () => {
    Object.defineProperty(navigator, 'clipboard', { value: undefined, configurable: true });
    const exec = vi.fn().mockReturnValue(true);
    (document as unknown as { execCommand: typeof exec }).execCommand = exec;
    render(<Markdown text={fence('sh', 'ls')} />);
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /^Copy/ }));
    });
    expect(exec).toHaveBeenCalledWith('copy');
    expect(screen.getByRole('status').textContent).toBe('Copied');
  });

  it('toggles wrap with aria-pressed and a class', () => {
    const { container } = render(<Markdown text={fence('go', 'x := 1')} />);
    const btn = screen.getByRole('button', { name: /^Wrap/ });
    const block = container.querySelector('.code-block')!;
    expect(btn.getAttribute('aria-pressed')).toBe('false');
    expect(block.classList.contains('code-block-wrap')).toBe(false);
    fireEvent.click(btn);
    expect(btn.getAttribute('aria-pressed')).toBe('true');
    expect(block.classList.contains('code-block-wrap')).toBe(true);
    fireEvent.click(btn);
    expect(block.classList.contains('code-block-wrap')).toBe(false);
  });

  it('highlights known languages lazily with hljs spans', async () => {
    const { container } = render(<Markdown text={fence('go', 'func main() {}')} />);
    expect(container.querySelector('.hljs-keyword')).toBeNull();
    await waitFor(() => expect(container.querySelector('code .hljs-keyword')).not.toBeNull());
    expect(container.querySelector('code')!.textContent).toBe('func main() {}\n');
  });

  it('leaves unknown languages as plain text', async () => {
    const { container } = render(<Markdown text={fence('klingon', 'qapla')} />);
    await new Promise((r) => setTimeout(r, 50));
    expect(container.querySelector('code')!.innerHTML).toBe('qapla\n');
    expect(container.querySelector('.code-block-lang')!.textContent).toBe('klingon');
  });

  it.each([
    ['html', '<script>alert(1)</script><img src=x onerror=alert(1)>'],
    ['js', 'el.innerHTML = "<img src=x onerror=alert(1)>"; location = "javascript:alert(1)"'],
    ['xml', '<a href="javascript:alert(1)" onclick="x()">hi</a>'],
    ['go', '<script>alert(1)</script>'],
  ])('keeps markup inside a %s fence inert text', async (lang, body) => {
    const { container } = render(<Markdown text={fence(lang, body)} />);
    await waitFor(() => expect(container.querySelector('code')!.children.length).toBeGreaterThanOrEqual(0));
    await new Promise((r) => setTimeout(r, 100));
    const code = container.querySelector('code')!;
    expect(code.textContent).toBe(body + '\n');
    expect(container.querySelector('script, img, iframe')).toBeNull();
    expect(container.querySelector('a')).toBeNull();
    for (const el of Array.from(container.querySelectorAll('code *'))) {
      expect(el.tagName).toBe('SPAN');
      expect(Array.from(el.attributes).every((a) => a.name === 'class')).toBe(true);
      expect(el.getAttribute('class') ?? 'hljs-x').toMatch(/^(hljs-[\w-]+|[\w-]+_)( (hljs-[\w-]+|[\w-]+_))*$/);
    }
  });

  it('does not let a plain span with a class through outside code', () => {
    const { container } = render(<Markdown text={'<span class="hljs-keyword" onclick="x()">hi</span>'} />);
    expect(container.querySelector('span.hljs-keyword')).toBeNull();
  });
});
