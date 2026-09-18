/** Viewport-clamped fixed tooltip coords. Escapes overflow:hidden ancestors. */

export type TooltipSide = "top" | "bottom" | "left" | "right";

export interface TooltipTriggerRect {
  top: number;
  bottom: number;
  left: number;
  right: number;
  width: number;
  height: number;
}

const GAP_PX = 8;
const VIEWPORT_PAD_PX = 8;

function viewportWidth() {
  return typeof window !== "undefined" ? window.innerWidth : 1024;
}

function viewportHeight() {
  return typeof window !== "undefined" ? window.innerHeight : 800;
}

function clamp(value: number, min: number, max: number) {
  return Math.min(max, Math.max(min, value));
}

/** Prefer `after` (below/right of the trigger). Flip to `before` if it would leave the viewport. */
function preferAfter(after: number, before: number, size: number, view: number) {
  const pad = VIEWPORT_PAD_PX;
  const overflowsEnd = after + size > view - pad && before >= pad;
  const overflowsStart = after < pad && before + size <= view - pad;
  if (overflowsEnd || overflowsStart) return before;
  return after;
}

export function computeTooltipStyle(
  trigger: TooltipTriggerRect,
  side: TooltipSide,
  size: { width: number; height: number },
  viewport?: { width: number; height: number },
): { top: number; left: number } {
  const vw = viewport?.width ?? viewportWidth();
  const vh = viewport?.height ?? viewportHeight();
  const width = Math.max(0, size.width);
  const height = Math.max(0, size.height);
  const pad = VIEWPORT_PAD_PX;
  const below = trigger.bottom + GAP_PX;
  const above = trigger.top - GAP_PX - height;
  const after = trigger.right + GAP_PX;
  const before = trigger.left - GAP_PX - width;

  const top = side === "left" || side === "right"
    ? trigger.top + trigger.height / 2 - height / 2
    : preferAfter(side === "bottom" ? below : above, side === "bottom" ? above : below, height, vh);
  const left = side === "top" || side === "bottom"
    ? trigger.left
    : preferAfter(side === "right" ? after : before, side === "right" ? before : after, width, vw);

  return {
    top: clamp(top, pad, Math.max(pad, vh - pad - height)),
    left: clamp(left, pad, Math.max(pad, vw - pad - width)),
  };
}
