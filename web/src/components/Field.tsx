import { cloneElement, isValidElement, useId, type ReactElement, type ReactNode } from 'react';
import { cx } from '../lib/format';

/**
 * A labelled control. Wrapping the input in the label gives it an accessible name. The hint sits
 * outside the label and is linked with aria-describedby, so a screen reader reads it as the
 * input's description, not as part of its name.
 */
export function Field({ label, children, hint, className }: { label: string; children: ReactNode; hint?: ReactNode; className?: string }) {
  const hintId = useId();
  // Only a single element can carry the link; any other child keeps its hint as plain text below.
  const control =
    hint && isValidElement(children)
      ? cloneElement(children as ReactElement<{ 'aria-describedby'?: string }>, { 'aria-describedby': hintId })
      : children;
  return (
    <div className={cx('field', className)}>
      <label className="field-control">
        <span className="field-label">{label}</span>
        {control}
      </label>
      {hint ? (
        <span id={hintId} className="field-hint">
          {hint}
        </span>
      ) : null}
    </div>
  );
}
