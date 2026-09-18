/**
 * Benes dashboard client for the Go proxy (`internal/server`).
 * Shared loading indicators (WP2 / 010_loading_contract.md).
 *
 * One live region per loading transition. A skeleton owns the announcement while a surface is
 * cold; a status line owns it during revalidation, and steps down to visual-only when an error
 * notice is already announcing. Rendering two live regions for one transition makes screen
 * readers repeat themselves, which is what the per-page ad-hoc loaders used to do.
 */

import type { ReactNode } from "react";

/** Attributes that make one element the transition's single announced live region. */
const ANNOUNCED_LIVE_REGION = {
  role: "status",
  "aria-live": "polite",
  "aria-atomic": "true",
} as const;

/** The announcement attributes, or nothing at all when something else already announces. */
function announced(announcing: boolean) {
  return announcing ? ANNOUNCED_LIVE_REGION : {};
}

/** A surface's own class next to the caller's, if the caller passed one. */
function surfaceName(base: string, extra?: string): string {
  return extra ? `${base} ${extra}` : base;
}

/**
 * Lets a page mirror its ready geometry without exposing placeholder values to assistive
 * technology. The surrounding skeleton owns the single announced sentence.
 */
function SkeletonRow() {
  return (
    <div className="data-surface-skeleton__row" aria-hidden="true">
      <span className="data-surface-skeleton__block" aria-hidden="true" />
    </div>
  );
}

/** The one sentence a transition announces; visual-only, because the region around it speaks. */
function AnnouncedSentence({ children }: { children: string }) {
  return <span className="sr-only">{children}</span>;
}

/** Every data surface takes the caller's own class next to its own. */
interface CallerClassName {
  className?: string;
}

export interface DataSurfaceSkeletonProps extends CallerClassName {
  /** The sentence read out while the surface is cold. */
  label: string;
  /** Placeholder rows to reserve; at least one, so a cold surface is never fully blank. */
  rows?: number;
}

/**
 * Keeps a cold surface non-empty from its first commit. This is the only live region for a cold
 * transition, so callers must not render a live status line beside it.
 */
export function DataSurfaceSkeleton({ rows, label, className }: DataSurfaceSkeletonProps) {
  const rowCount = Math.max(1, Math.trunc(rows ?? 3));
  const placeholders = Array.from({ length: rowCount }, (_, row) => <SkeletonRow key={row} />);
  return (
    <div
      className={surfaceName("data-surface-skeleton", className)}
      {...ANNOUNCED_LIVE_REGION}
      aria-busy="true"
    >
      <AnnouncedSentence>{label}</AnnouncedSentence>
      {placeholders}
    </div>
  );
}

export interface DataSurfaceStatusProps extends CallerClassName {
  children: ReactNode;
  /** Spinner while the surface revalidates; defaults to on. */
  busy?: boolean;
  /** `false` where an error notice already owns this transition's announcement. */
  live?: boolean;
}

/**
 * Announces a revalidation without replacing visible stale content. Pass `live={false}` where an
 * error notice already owns the announcement for this transition.
 */
export function DataSurfaceStatus({ children, live, busy, className }: DataSurfaceStatusProps) {
  const spinning = busy ?? true;
  return (
    <div
      className={surfaceName("data-surface-status", className)}
      {...announced(live ?? true)}
      aria-busy={spinning || undefined}
    >
      {spinning ? <span className="spin" aria-hidden="true" /> : null}
      <span>{children}</span>
    </div>
  );
}
