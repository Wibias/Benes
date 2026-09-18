/**
 * Presentation vocabulary for the shared UI primitives.
 *
 * The class names and the few style objects the primitives reuse live here rather than inline
 * in the components, for the same reason `select-policy.ts`, `select-position.ts` and
 * `app-shell.ts` exist: what the stylesheet keys off is a contract of its own, and assembling it
 * in the middle of a component's markup buries that contract among the elements.
 */
import type { CSSProperties } from "react";

/** Shared presentation tone for success, degraded success, and failure notices. */
export type NoticeTone = "ok" | "warn" | "err";

/** Which notice shape is being rendered. */
export type NoticeVariant = "inline" | "toast";

/**
 * `warn` is degraded-but-not-failed: the action happened, something adjacent did not. It must
 * not read as the clean success the user did not get, so it keeps its own class.
 */
const NOTICE_TONE_CLASSES: Record<NoticeTone, string> = {
  ok: "notice-ok",
  warn: "notice-warn",
  err: "notice-err",
};

/** The container class both notice shapes start from. */
const NOTICE_BASE_CLASS = "notice";

/** The toast's own container class, added by the transient shape. */
const TOAST_NOTICE_CLASS = "toast-notice";

/** The toast's copy span; an inline notice's body needs no class of its own. */
const TOAST_NOTICE_COPY_CLASS = "toast-notice-copy";

export function noticeContainerClassName(variant: NoticeVariant, tone: NoticeTone): string {
  const toneClass = NOTICE_TONE_CLASSES[tone];
  if (variant === "toast") return `${TOAST_NOTICE_CLASS} ${NOTICE_BASE_CLASS} ${toneClass}`;
  return `${NOTICE_BASE_CLASS} ${toneClass}`;
}

export function noticeBodyClassName(variant: NoticeVariant): string | undefined {
  return variant === "toast" ? TOAST_NOTICE_COPY_CLASS : undefined;
}

/** The empty-state container, with the caller's extra class appended when it gave one. */
export function emptyStateClassName(extra?: string): string {
  return extra ? `empty ${extra}` : "empty";
}

/**
 * Element class names for the standalone primitives.
 *
 * Gathered here rather than spelled inside each component for the same reason the notice
 * classes above are: these strings are the contract with `styles.css`, and a component that
 * scatters them through its markup makes that contract hard to review in one place.
 */
export const SWITCH_CLASS = "switch";
export const SWITCH_ON_CLASS = "on";
export const SWITCH_KNOB_CLASS = "knob";
export const TOAST_HOST_CLASS = "toast-notice-host";
export const TOAST_DISMISS_CLASS = "toast-notice-dismiss";
export const EMPTY_TITLE_CLASS = "title";
export const EMPTY_TEXT_CLASS = "text-control";

/** The switch's own class list, on or off. */
export function switchClassName(on: boolean): string {
  return on ? `${SWITCH_CLASS} ${SWITCH_ON_CLASS}` : SWITCH_CLASS;
}

/** An inline tooltip trigger inherits its surroundings; it must not look like a button. */
export const TOOLTIP_TRIGGER_STYLE: CSSProperties = {
  display: "inline",
  border: 0,
  background: "transparent",
  padding: 0,
  margin: 0,
  color: "inherit",
  font: "inherit",
  cursor: "inherit",
};

/** The Select's wrapper is the positioning context for an inline menu. */
export const SELECT_WRAPPER_STYLE: CSSProperties = { position: "relative", display: "inline-block" };

/** The Select's chevron box and colour; only its rotation follows the menu. */
export const SELECT_CHEVRON_STYLE: CSSProperties = {
  width: 12,
  height: 12,
  color: "var(--muted)",
  transition: "transform .12s",
};