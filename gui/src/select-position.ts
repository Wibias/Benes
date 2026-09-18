/**
 * Viewport geometry for the dashboard's fixed-position menus.
 *
 * A menu is measured against one viewport-relative anchor box and rendered with
 * `position: fixed`, so every number below is an absolute viewport coordinate. The two
 * placement families stay separate on purpose: a stacked menu shares its anchor's column
 * and hangs off an anchor edge, while a beside menu occupies the column to the right of
 * the anchor and may start at the anchor's top edge instead of below it.
 */
import type { CSSProperties } from "react";

/** A viewport-relative box. Matched "inactive control" boxes use the same shape. */
export interface SelectMenuTriggerRect {
  top: number;
  bottom: number;
  left: number;
  right: number;
  width: number;
  height: number;
}

export interface SelectMenuStyleOptions {
  align?: "left" | "right";
  placement?: "below" | "right";
  menuHeight?: number;
  /** Horizontal (and below-placement vertical) box to match, e.g. a labeled filter chip. */
  match?: SelectMenuTriggerRect;
}

/** The viewport measurements placement is resolved against. */
interface Screen {
  width: number;
  height: number;
}

/** Which way a stacked menu grows away from its anchor. */
type Growth = "down" | "up";

/** Which anchor vertical edge a stacked menu is pinned to. */
type Pin = "start" | "end";

const MENU = Object.freeze({
  /** Clearance between the anchor and the edge of the menu it hangs off. */
  drop: 4,
  /** Clearance used when the menu has to point back across its anchor instead. */
  reversal: 8,
  /** Margin kept between the menu and every viewport edge. */
  margin: 8,
  /** The height band the menu stylesheet can actually render. */
  band: Object.freeze({ min: 120, max: 280 }),
  /** Beside menus keep a column of their own with a fixed floor width. */
  beside: Object.freeze({ minWidth: 160, offset: 12 }),
});

/** The 1024x800 fallback keeps the geometry testable outside a browser. */
function screenSize(): Screen {
  if (typeof window === "undefined") return { width: 1024, height: 800 };
  return { width: window.innerWidth, height: window.innerHeight };
}

/** The height a menu asks for, brought inside the band the stylesheet can render. */
function bandedHeight(requested: number): number {
  return Math.min(Math.max(requested, MENU.band.min), MENU.band.max);
}

/**
 * Left inset for a fixed box of `width`.
 *
 * Not a symmetric clamp: when the viewport is narrower than the box, the left margin has
 * to win, otherwise `Math.min` would drag the box off the left edge entirely.
 */
function leftInset(desired: number, width: number, viewportWidth: number): number {
  return Math.max(MENU.margin, Math.min(desired, viewportWidth - MENU.margin - width));
}

/** Right inset for a fixed box pinned to an anchor's right edge. */
function rightInset(anchorRight: number, viewportWidth: number): number {
  return Math.max(MENU.margin, viewportWidth - anchorRight);
}

/** How a stacked menu grows away from its anchor, and how far it can. */
interface StackedSlot {
  growth: Growth;
  /** Distance from the viewport edge the menu grows from. */
  offset: number;
  /** Space between the menu and the viewport edge it grows toward. */
  room: number;
}


/**
 * Resolve the vertical half of a stacked menu.
 *
 * The menu only reverses when it genuinely does not fit *and* the other side is roomier,
 * so a short viewport never makes a short menu jump for no reason.
 */
function stackedSlot(anchor: SelectMenuTriggerRect, height: number, viewportHeight: number): StackedSlot {
  const roomBelow = viewportHeight - anchor.bottom - MENU.margin;
  const roomAbove = anchor.top - MENU.margin;
  if (height + MENU.drop > roomBelow && roomAbove > roomBelow) {
    return { growth: "up", offset: viewportHeight - anchor.top + MENU.reversal, room: roomAbove };
  }
  return { growth: "down", offset: anchor.bottom + MENU.drop, room: roomBelow };
}

/** A stacked menu: pinned to one edge of its anchor's column, reversed when needed. */
function stackedStyle(
  anchor: SelectMenuTriggerRect,
  pin: Pin,
  height: number,
  screen: Screen,
  matched: boolean,
): CSSProperties {
  const width = Math.max(anchor.width, 0);
  const slot = stackedSlot(anchor, height, screen.height);
  const style: CSSProperties = {
    position: "fixed",
    minWidth: width,
    width: matched ? width : undefined,
    maxHeight: Math.max(0, Math.min(MENU.band.max, slot.room - MENU.drop)),
  };
  if (slot.growth === "up") style.bottom = slot.offset;
  else style.top = slot.offset;
  if (pin === "end") style.right = rightInset(anchor.right, screen.width);
  else style.left = leftInset(anchor.left, width, screen.width);
  return style;
}

/** A beside menu: its own column to the right of the anchor, top-aligned with it. */
function besideStyle(anchor: SelectMenuTriggerRect, height: number, screen: Screen): CSSProperties {
  const roomBelow = screen.height - anchor.top - MENU.margin;
  const roomAbove = anchor.top - MENU.margin;
  const openUp = height + MENU.reversal > roomBelow && roomAbove > roomBelow;
  const room = openUp ? anchor.top - MENU.margin - MENU.drop : roomBelow;
  const style: CSSProperties = {
    position: "fixed",
    left: leftInset(anchor.right + MENU.beside.offset, MENU.beside.minWidth, screen.width),
    minWidth: MENU.beside.minWidth,
    maxHeight: Math.max(MENU.band.min, Math.min(MENU.band.max, room)),
  };
  if (openUp) style.bottom = screen.height - anchor.top + MENU.reversal;
  else style.top = anchor.top;
  return style;
}

export function computeSelectMenuStyle(
  trigger: SelectMenuTriggerRect,
  { align, placement = "below", menuHeight = MENU.band.max, match }: SelectMenuStyleOptions = {},
): CSSProperties {
  const screen = screenSize();
  const height = bandedHeight(menuHeight);
  if (placement === "right") return besideStyle(trigger, height, screen);

  // A matched box substitutes for the trigger everywhere except the alignment default,
  // which stays a decision about the trigger's own column.
  const anchor = match ?? trigger;
  const pin: Pin = align
    ? align === "right" ? "end" : "start"
    : anchor.right > screen.width / 2 ? "end" : "start";
  return stackedStyle(anchor, pin, height, screen, match !== undefined);
}
