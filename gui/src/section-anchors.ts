/** Benes dashboard client for the Go proxy (`internal/server`). */
/**
 * Scroll-target ids for the sticky section strips (`SectionTabs`).
 *
 * They live outside the strip component so that file only exports components, which the
 * fast-refresh rule requires, and so a page can render its anchors without the strip.
 * Every id of a scope starts with the stem `anchorStem` builds; the strip drops that stem
 * to read a section id back off a node.
 */
const SECTION_SEGMENT = "section";
const ANCHOR_SEPARATOR = "-";

/** `<pageScope>-section-` — the stem every anchor id of a page scope shares. */
function anchorStem(pageScope: string): string {
  return [pageScope, SECTION_SEGMENT, ""].join(ANCHOR_SEPARATOR);
}

/** Scroll-target id for one section inside a page scope. */
export function sectionAnchorId(pageScope: string, sectionId: string): string {
  return anchorStem(pageScope) + sectionId;
}

/** The prefix `sectionAnchorId` gives a scope. */
export function sectionAnchorPrefix(pageScope: string): string {
  return anchorStem(pageScope);
}

/** Ignore scroll-spy updates briefly after a tab click while smooth scroll is in flight. */
export const SECTION_TAB_SCROLL_LOCK_MS = 1200;
