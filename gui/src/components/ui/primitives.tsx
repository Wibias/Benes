/**
 * The dashboard's stateless presentation primitives.
 *
 * A notice is where the dashboard says something happened, and it says it in two shapes: inline
 * and transient. Both answer the same two questions — which class carries the tone, and which
 * glyph heads it — so the glyph table is one place here rather than a pair of ternaries that can
 * drift, and both shapes render through one frame instead of repeating the same three children
 * twice. The class vocabulary itself lives in `ui-presentation.ts`.
 */
import type { CSSProperties, ReactNode } from "react";
import { createPortal } from "react-dom";
import { IconAlert, IconCheck } from "../../icons";
import {
  EMPTY_TEXT_CLASS,
  EMPTY_TITLE_CLASS,
  SWITCH_KNOB_CLASS,
  TOAST_DISMISS_CLASS,
  TOAST_HOST_CLASS,
  emptyStateClassName,
  noticeBodyClassName,
  noticeContainerClassName,
  switchClassName,
} from "../../ui-presentation";
import type { NoticeTone, NoticeVariant } from "../../ui-presentation";

export type { NoticeTone } from "../../ui-presentation";

/**
 * The glyph each tone is headed with, built once at module load so a notice does not allocate
 * its icon on every render.
 */
const NOTICE_GLYPHS: Record<NoticeTone, ReactNode> = {
  ok: <IconCheck />,
  warn: <IconAlert />,
  err: <IconAlert />,
};

interface NoticeFrameProps {
  readonly tone: NoticeTone;
  readonly variant: NoticeVariant;
  readonly role: "status" | "alert";
  readonly body: ReactNode;
  readonly action?: ReactNode;
}

/** The frame both notice shapes share: tone class, glyph, body, optional action. */
function NoticeFrame({ tone, variant, role, body, action }: NoticeFrameProps) {
  return (
    <div
      className={noticeContainerClassName(variant, tone)}
      role={role}
      aria-live={variant === "toast" ? "polite" : undefined}
    >
      {NOTICE_GLYPHS[tone]}
      <span className={noticeBodyClassName(variant)}>{body}</span>
      {action}
    </div>
  );
}

export interface SwitchProps {
  readonly on: boolean;
  readonly onClick: () => void;
  readonly disabled?: boolean;
  /** Accessible name. Falls back to the switch's own state when the caller gave none. */
  readonly label?: string;
}

export function Switch({ on, onClick, disabled, label }: SwitchProps) {
  // The fallback name stays in this file on purpose: it is user-facing copy, and this tree is
  // the one the i18n linter watches.
  const accessibleName = label ?? (on ? "enabled" : "disabled");
  return (
    <button type="button" className={switchClassName(on)} onClick={onClick} disabled={disabled}
      aria-pressed={on} aria-label={accessibleName}>
      <span className={SWITCH_KNOB_CLASS} />
    </button>
  );
}

export interface NoticeProps {
  readonly tone: NoticeTone;
  readonly children: ReactNode;
  readonly role?: "status" | "alert";
}

export function Notice({ tone, children, role = "status" }: NoticeProps) {
  return <NoticeFrame tone={tone} variant="inline" role={role} body={children} />;
}

export interface ToastNoticeProps {
  readonly tone: NoticeTone;
  readonly children: ReactNode;
  readonly onDismiss?: () => void;
  /** Required whenever onDismiss is provided — pass t("common.close"). */
  readonly dismissLabel: string;
}

/**
 * Fixed-position status toast. Portaled so it never consumes page flow / shifts layout.
 * Parent owns auto-dismiss timing (success banners are typically transient).
 */
export function ToastNotice({ tone, children, onDismiss, dismissLabel }: ToastNoticeProps) {
  const dismissAction = onDismiss && (
    <button type="button" className={TOAST_DISMISS_CLASS} onClick={onDismiss} aria-label={dismissLabel}>
      ×
    </button>
  );
  return createPortal(
    <div className={TOAST_HOST_CLASS} role="presentation">
      <NoticeFrame tone={tone} variant="toast" role="status" body={children} action={dismissAction} />
    </div>,
    document.body,
  );
}

export interface EmptyStateProps {
  readonly icon?: ReactNode;
  readonly title: ReactNode;
  readonly children?: ReactNode;
  readonly className?: string;
  readonly style?: CSSProperties;
}

export function EmptyState({ icon, title, children, className, style }: EmptyStateProps) {
  const body = children && <div className={EMPTY_TEXT_CLASS}>{children}</div>;
  return (
    <div className={emptyStateClassName(className)} style={style}>
      {icon}
      <div className={EMPTY_TITLE_CLASS}>{title}</div>
      {body}
    </div>
  );
}
