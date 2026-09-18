/** Roster membership and exclusive roles for the Sub-agents board. */

export const FEATURED_MAX = 5;

export type SubagentRole = "featured" | "firstCall" | "fallback" | "unassigned";

/** Role radios: Primary model is Assignment, not a Role option. */
export type SubagentRoleChoice = "assigned" | "fallback" | "unassigned";

export const ROLE_CHOICES: SubagentRoleChoice[] = ["assigned", "fallback", "unassigned"];

export function roleChoiceOf(role: SubagentRole): SubagentRoleChoice {
  if (role === "fallback") return "fallback";
  if (role === "unassigned") return "unassigned";
  return "assigned";
}

export function roleFromChoice(choice: SubagentRoleChoice, current: SubagentRole): SubagentRole {
  if (choice === "assigned") return current === "firstCall" ? "firstCall" : "featured";
  return choice;
}

export function providerOf(id: string): string {
  const slash = id.indexOf("/");
  return slash > 0 ? id.slice(0, slash) : id;
}

export function roleOf(
  id: string,
  chosenSet: Set<string>,
  firstCall: string,
  fallbackSet: Set<string>,
): SubagentRole {
  if (firstCall === id) return "firstCall";
  if (chosenSet.has(id)) return "featured";
  if (fallbackSet.has(id)) return "fallback";
  return "unassigned";
}

/** Featured spawn order first, then fallbacks that are not already featured. */
export function rosterIds(chosen: string[], fallbacks: string[]): string[] {
  const seen = new Set(chosen);
  const extra = fallbacks.filter((id) => !seen.has(id));
  return [...chosen, ...extra];
}

export function reconcileOrder(prev: string[], chosen: string[], fallbacks: string[]): string[] {
  const members = rosterIds(chosen, fallbacks);
  const memberSet = new Set(members);
  const kept = prev.filter((id) => memberSet.has(id));
  const seen = new Set(kept);
  for (const id of members) {
    if (!seen.has(id)) kept.push(id);
  }
  return kept;
}

export function availableIds(catalog: string[], roster: string[]): string[] {
  const taken = new Set(roster);
  return catalog.filter((id) => !taken.has(id));
}

export function moveToIndex(ids: string[], id: string, index: number): string[] {
  const from = ids.indexOf(id);
  if (from < 0) return ids;
  const next = ids.slice();
  next.splice(from, 1);
  const to = Math.max(0, Math.min(next.length, index));
  next.splice(to, 0, id);
  return next;
}

/** Slot under the pointer. Stays on `fromIndex` until the pointer enters another row. */
export function insertIndexFromY(
  clientY: number,
  rows: { top: number; bottom: number }[],
  fromIndex = 0,
): number {
  if (rows.length === 0) return 0;
  let index = Math.max(0, Math.min(fromIndex, rows.length - 1));
  for (let i = 0; i < rows.length; i++) {
    if (i === fromIndex) continue;
    if (i > fromIndex && clientY >= rows[i].top) index = i;
    if (i < fromIndex && clientY < rows[i].bottom) {
      index = i;
      break;
    }
  }
  return index;
}

export function reorderShiftY(
  fromIndex: number,
  hoverIndex: number,
  index: number,
  height: number,
): number {
  if (fromIndex < hoverIndex && index > fromIndex && index <= hoverIndex) return -height;
  if (fromIndex > hoverIndex && index >= hoverIndex && index < fromIndex) return height;
  return 0;
}

/** Keep a lifted order card inside the Model order block (top divider to section bottom). */
export function clampGhostY(y: number, boundTop: number, boundBottom: number, height: number): number {
  const maxY = Math.max(boundTop, boundBottom - height);
  return Math.max(boundTop, Math.min(maxY, y));
}

/** Pixels to scroll per frame when the pointer sits in a scroll edge. Negative = up. */
export function scrollEdgeDelta(
  clientY: number,
  boundTop: number,
  boundBottom: number,
  edgePx = 36,
  maxStep = 18,
): number {
  if (boundBottom - boundTop <= edgePx * 2) return 0;
  if (clientY <= boundTop + edgePx) {
    const t = Math.min(1, Math.max(0, (boundTop + edgePx - clientY) / edgePx));
    return -Math.max(1, Math.ceil(maxStep * t));
  }
  if (clientY >= boundBottom - edgePx) {
    const t = Math.min(1, Math.max(0, (clientY - (boundBottom - edgePx)) / edgePx));
    return Math.max(1, Math.ceil(maxStep * t));
  }
  return 0;
}

/** Nearest ancestor (inclusive) that can scroll vertically. */
export function findVerticalScrollParent(start: Element | null): HTMLElement | null {
  let el: Element | null = start;
  while (el) {
    if (el instanceof HTMLElement) {
      const { overflowY } = getComputedStyle(el);
      if (
        (overflowY === "auto" || overflowY === "scroll" || overflowY === "overlay") &&
        el.scrollHeight > el.clientHeight + 1
      ) {
        return el;
      }
    }
    el = el.parentElement;
  }
  return null;
}

/** Keep slot geometry in viewport coords after the list scrolls. */
export function shiftSlotsY(
  slots: { top: number; bottom: number }[],
  scrollDelta: number,
): { top: number; bottom: number }[] {
  if (scrollDelta === 0) return slots;
  return slots.map((slot) => ({
    top: slot.top - scrollDelta,
    bottom: slot.bottom - scrollDelta,
  }));
}

export function splitRosterOrder(
  order: string[],
  chosenSet: Set<string>,
  fallbackSet: Set<string>,
): { chosen: string[]; fallbacks: string[] } {
  return {
    chosen: order.filter((id) => chosenSet.has(id)),
    fallbacks: order.filter((id) => fallbackSet.has(id) && !chosenSet.has(id)),
  };
}

export function applyRole(
  id: string,
  role: SubagentRole,
  chosen: string[],
  fallbacks: string[],
  firstCall: string,
): { chosen: string[]; fallbacks: string[]; firstCall: string } {
  const inChosen = chosen.includes(id);
  if ((role === "featured" || role === "firstCall") && !inChosen && chosen.length >= FEATURED_MAX) {
    if (role === "firstCall") {
      return {
        chosen,
        fallbacks: fallbacks.filter((item) => item !== id),
        firstCall: id,
      };
    }
    return { chosen, fallbacks, firstCall };
  }

  const nextChosen = chosen.filter((item) => item !== id);
  const nextFallbacks = fallbacks.filter((item) => item !== id);
  const nextFirst = firstCall === id ? "" : firstCall;

  if (role === "featured" || role === "firstCall") {
    return {
      chosen: inChosen ? chosen : [...nextChosen, id],
      fallbacks: nextFallbacks,
      firstCall: role === "firstCall" ? id : nextFirst,
    };
  }
  if (role === "fallback") {
    return { chosen: nextChosen, fallbacks: [...nextFallbacks, id], firstCall: nextFirst };
  }
  return { chosen: nextChosen, fallbacks: nextFallbacks, firstCall: nextFirst };
}

export function effortLabel(value: string): string {
  if (!value) return value;
  return value.charAt(0).toUpperCase() + value.slice(1);
}
