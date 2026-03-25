/**
 * @license
 * Copyright 2025 NGOClaw Team
 * SPDX-License-Identifier: BSL-1.1
 *
 * Display-only checkbox component for plan entries
 */

import type { FC } from 'react';

export interface CheckboxDisplayProps {
  checked?: boolean;
  indeterminate?: boolean;
  disabled?: boolean;
  className?: string;
  style?: React.CSSProperties;
  title?: string;
}

/**
 * Display-only checkbox styled via Tailwind classes.
 * - Renders a custom-looking checkbox using appearance-none and pseudo-elements.
 * - Supports indeterminate (middle) state using a data- attribute.
 * - Intended for read-only display (disabled by default).
 */
export const CheckboxDisplay: FC<CheckboxDisplayProps> = ({
  checked = false,
  indeterminate = false,
  disabled = true,
  className = '',
  style,
  title,
}) => {
  const showCheck = !!checked && !indeterminate;
  const showInProgress = !!indeterminate;

  return (
    <span
      role="checkbox"
      aria-checked={indeterminate ? 'mixed' : !!checked}
      aria-disabled={disabled || undefined}
      title={title}
      style={style}
      className={[
        'q m-[2px] shrink-0 w-4 h-4 relative rounded-[2px] box-border',
        showCheck 
          ? 'bg-emerald-500/20 border-emerald-500/40' 
          : 'border border-[var(--app-input-border)] bg-[var(--app-input-background)]',
        'inline-flex items-center justify-center',
        className,
      ].join(' ')}
    >
      {showCheck ? (
        <span
          aria-hidden
          className={[
            'absolute block',
            'left-[3px] top-[2px]',
            'w-[9px] h-[5px]',
            'border-l-[2px] border-b-[2px]',
            'border-emerald-400',
            '-rotate-45',
          ].join(' ')}
        />
      ) : null}
      {showInProgress ? (
        <span
          aria-hidden
          className={[
            'absolute inline-block',
            'left-1/2 top-[10px] -translate-x-1/2 -translate-y-1/2',
            'text-[16px] leading-none text-[#e1c08d] select-none',
          ].join(' ')}
        >
          *
        </span>
      ) : null}
    </span>
  );
};
