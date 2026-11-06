import { useState } from 'react';
import { MenuPopover } from './MenuPopover';
import { useConversationSettings, useUpdateConversationSettings } from '../queries';
import { modelEffortLabel, needsDangerConfirm, permissionLabel } from '../lib/composerSettings';
import type { ConversationSettings, SettingsChoice } from '../types';

function Chevron() {
  return (
    <svg width="10" height="10" viewBox="0 0 10 10" aria-hidden="true" focusable="false">
      <path d="m2 3.5 3 3 3-3" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

function ShieldIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true" focusable="false">
      <path d="M8 2 3 4v4c0 3 2.2 5 5 6 2.8-1 5-3 5-6V4L8 2Z" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinejoin="round" />
    </svg>
  );
}

function Group({
  title,
  list,
  value,
  onPick,
}: {
  title: string;
  list: SettingsChoice[];
  value: string | undefined;
  onPick: (choice: SettingsChoice) => void;
}) {
  if (list.length === 0) return null;
  return (
    <div className="menu-pop-group">
      <div className="menu-pop-title">{title}</div>
      {list.map((c) => (
        <button
          key={c.value}
          type="button"
          role="menuitemradio"
          aria-checked={value === c.value}
          className="menu-pop-item"
          onClick={() => onPick(c)}
        >
          <span className="menu-pop-check" aria-hidden="true">{value === c.value ? '✓' : ''}</span>
          <span>
            {c.label}
            {c.hint ? <span className="menu-pop-hint muted small"> {c.hint}</span> : null}
          </span>
        </button>
      ))}
    </div>
  );
}

function ResetItem({ onClick }: { onClick: () => void }) {
  return (
    <button type="button" role="menuitem" className="menu-pop-item menu-pop-reset" onClick={onClick}>
      <span className="menu-pop-check" aria-hidden="true" />
      <span>Reset to role default</span>
    </button>
  );
}

/**
 * Both pickers share one saved copy of the settings. `side` picks which control this renders so the
 * composer can place the permission picker on the left of its footer and the model picker on the right.
 */
export function ComposerSettings({ conversationId, side }: { conversationId: string; side: 'permission' | 'model' }) {
  const query = useConversationSettings(conversationId);
  const update = useUpdateConversationSettings(conversationId);
  // The dangerous mode waiting for the owner's confirmation, if any.
  const [pending, setPending] = useState<SettingsChoice | null>(null);
  const view = query.data;
  if (!view) return null;

  const save = (settings: ConversationSettings, close: () => void, confirmDangerous = false) =>
    update.mutate({ settings, confirmDangerous }, { onSuccess: close });
  const failure = update.error ? <p className="menu-pop-error small" role="alert">{update.error.message}</p> : null;

  if (side === 'permission') {
    const label = permissionLabel(view);
    return (
      <MenuPopover
        label={`Permissions: ${label}`}
        className="composer-permission"
        onClosed={() => setPending(null)}
        trigger={
          <>
            <ShieldIcon />
            <span className="menu-pop-label">{label}</span>
          </>
        }
      >
        {(close) =>
          pending ? (
            <div className="menu-pop-confirm" role="group" aria-label="Confirm">
              <p>
                <strong>{pending.label}</strong> skips safety checks: the agent can change files and run commands
                without any permission check. Use it only in a project you trust.
              </p>
              <div className="menu-pop-actions">
                <button type="button" className="btn" onClick={() => setPending(null)}>
                  Cancel
                </button>
                <button
                  type="button"
                  className="btn danger"
                  onClick={() =>
                    save({ ...view.settings, permission_mode: pending.value }, () => {
                      setPending(null);
                      close();
                    }, true)
                  }
                >
                  Allow {pending.label}
                </button>
              </div>
              {failure}
            </div>
          ) : (
            <>
              <Group
                title="Permissions"
                list={view.choices.permission_modes}
                value={view.settings.permission_mode || view.defaults.permission_mode}
                onPick={(c) =>
                  needsDangerConfirm(view, c.value)
                    ? setPending(c)
                    : save({ ...view.settings, permission_mode: c.value }, close)
                }
              />
              <ResetItem onClick={() => save({ ...view.settings, permission_mode: '' }, close)} />
              {failure}
            </>
          )
        }
      </MenuPopover>
    );
  }

  const label = modelEffortLabel(view);
  return (
    <MenuPopover
      label={`Model and effort: ${label}`}
      className="composer-model-picker"
      trigger={
        <>
          <span className="menu-pop-label">{label}</span>
          <Chevron />
        </>
      }
    >
      {(close) => (
        <>
          <Group
            title="Model"
            list={view.choices.models}
            value={view.settings.model || view.defaults.model}
            onPick={(c) => save({ ...view.settings, model: c.value }, close)}
          />
          <Group
            title="Effort"
            list={view.choices.efforts}
            value={view.settings.effort || view.defaults.effort}
            onPick={(c) => save({ ...view.settings, effort: c.value }, close)}
          />
          <ResetItem onClick={() => save({ ...view.settings, model: '', effort: '' }, close)} />
          {failure}
        </>
      )}
    </MenuPopover>
  );
}
