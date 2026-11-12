// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { Field } from './Field';

afterEach(cleanup);

describe('Field', () => {
  it("names the input by its label alone; the hint is its description", () => {
    render(
      <Field label="Name" hint="Letters, digits, '.', '_' and '-'.">
        <input type="text" />
      </Field>,
    );
    const input = screen.getByRole('textbox', { name: 'Name' });
    expect(input).toHaveAccessibleName('Name');
    expect(input).toHaveAccessibleDescription("Letters, digits, '.', '_' and '-'.");
  });

  it('describes a select and a textarea the same way', () => {
    render(
      <>
        <Field label="Role" hint="Leave on routing default.">
          <select>
            <option>Routing default</option>
          </select>
        </Field>
        <Field label="Body" hint="Markdown is supported.">
          <textarea />
        </Field>
      </>,
    );
    expect(screen.getByRole('combobox', { name: 'Role' })).toHaveAccessibleDescription('Leave on routing default.');
    expect(screen.getByRole('textbox', { name: 'Body' })).toHaveAccessibleDescription('Markdown is supported.');
  });

  it('adds no description when there is no hint', () => {
    render(
      <Field label="Title">
        <input type="text" />
      </Field>,
    );
    const input = screen.getByRole('textbox', { name: 'Title' });
    expect(input).not.toHaveAttribute('aria-describedby');
    expect(document.querySelector('.field-hint')).toBeNull();
  });

  it('keeps the class name on the outer element and the input inside the label', () => {
    render(
      <Field label="Name" hint="A hint." className="grow">
        <input type="text" />
      </Field>,
    );
    const input = screen.getByRole('textbox', { name: 'Name' });
    expect(input.closest('label')?.querySelector('.field-label')?.textContent).toBe('Name');
    expect(input.closest('.field')).toHaveClass('field', 'grow');
    // The hint is not inside the label.
    expect(input.closest('label')?.querySelector('.field-hint')).toBeNull();
  });
});
