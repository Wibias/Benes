/**
 * Anchored floating layers.
 *
 * A Select menu and a Tooltip bubble are the same mechanism: an element rendered away from
 * its anchor (usually in `document.body`), positioned from the anchor's viewport box,
 * re-measured when the viewport moves under it, dismissed by a press outside the control,
 * and torn down when it closes. This module owns that lifecycle once. Where the layer
 * actually lands is the caller's placement function, because a menu and a tooltip place
 * themselves by different rules and those rules are already owned elsewhere
 * (`select-position.ts`, `tooltip-position.ts`).
 */
import { useCallback, useEffect, useLayoutEffect, useState } from "react";
import type { RefObject } from "react";

/** A viewport box, structurally compatible with `DOMRect`. */
export interface AnchorBox {
  readonly top: number;
  readonly bottom: number;
  readonly left: number;
  readonly right: number;
  readonly width: number;
  readonly height: number;
}

/** The layer's own measured size. */
export interface LayerSize {
  readonly width: number;
  readonly height: number;
}

/** The events that move an anchor under an open layer. */
export const VIEWPORT_RESIZE_EVENT = "resize";
export const VIEWPORT_SCROLL_EVENT = "scroll";

/**
 * True when two placements would paint the same thing.
 *
 * A shallow comparison is enough because every placement this module produces is a flat bag
 * of numbers and strings; keeping it here means an open menu stops re-rendering once its
 * geometry settles, without the call site hand-listing the fields to compare.
 */
export function samePlacement(left: unknown, right: unknown): boolean {
  if (left === right) return true;
  if (typeof left !== "object" || typeof right !== "object") return false;
  if (left === null || right === null) return false;
  const before = left as Record<string, unknown>;
  const after = right as Record<string, unknown>;
  const keys = new Set([...Object.keys(before), ...Object.keys(after)]);
  for (const key of keys) {
    if (before[key] !== after[key]) return false;
  }
  return true;
}

/**
 * True when a pointer target landed outside both the control and its floating layer.
 *
 * The layer is portaled, so it is not a descendant of the control: containment has to be
 * asked of each element, and a press inside either one is not a dismissal.
 */
export function pointerLeftControl(target: Node, control: Node | null, layer: Node | null): boolean {
  if (control?.contains(target) === true) return false;
  if (layer?.contains(target) === true) return false;
  return true;
}

export interface FloatingLayerOptions<TStyle> {
  /** Whether the layer is on screen. Nothing is measured or bound while it is closed. */
  readonly open: boolean;
  /** Whether this control uses a floating layer at all (an inline menu does not). */
  readonly enabled?: boolean;
  /** Element the layer is positioned against. */
  readonly anchor: RefObject<HTMLElement | null>;
  /**
   * An element whose box replaces the anchor's. The Select uses it to match a labelled filter
   * chip rather than its own trigger.
   */
  readonly substituteAnchor?: () => HTMLElement | null;
  /** The layer element, owned by the projection that renders it. */
  readonly layer: RefObject<HTMLElement | null>;
  /** The control's own wrapper. Only needed together with `onDismiss`. */
  readonly control?: RefObject<HTMLElement | null>;
  /** Turns the two boxes into the layer's style. `layerSize` is null before it is measured. */
  readonly place: (anchorBox: AnchorBox, layerSize: LayerSize | null) => TStyle;
  /** Called when a pointer press lands outside the control and its layer. */
  readonly onDismiss?: () => void;
}

/** What a caller does with its layer. */
export interface FloatingLayer<TStyle> {
  /** Style for the layer, or undefined until it has been placed. */
  readonly style: TStyle | undefined;
  /**
   * Read both boxes and place the layer now.
   *
   * The projection calls this from its own attach callback on the layer element: the first
   * placement has to happen while the element is in the DOM and before anything is painted,
   * which is exactly when React calls an attach callback — and unlike a layout effect it does
   * not write state from inside an effect.
   */
  readonly remeasure: () => void;
}

function layerSizeOf(element: HTMLElement | null): LayerSize | null {
  if (element === null) return null;
  const size = { width: element.offsetWidth, height: element.offsetHeight };
  return size.width === 0 && size.height === 0 ? null : size;
}

export function useFloatingLayer<TStyle>({
  open,
  enabled = true,
  anchor,
  substituteAnchor,
  layer,
  control,
  place,
  onDismiss,
}: FloatingLayerOptions<TStyle>): FloatingLayer<TStyle> {
  const active = open && enabled;
  const [style, setStyle] = useState<TStyle | undefined>(undefined);

  /**
   * `getBoundingClientRect` only means anything after layout, so this runs from the attach
   * callback and from the viewport handlers. The state write is skipped when the numbers did
   * not move, so a settled menu stops re-rendering.
   *
   * Closing deliberately keeps the last placement: a reopen re-attaches the element and
   * re-places from the live anchor before the next paint, so retained coordinates are never
   * painted stale, and clearing them on close would cost a cascading render for a layer nobody
   * is looking at.
   */
  const remeasure = useCallback(() => {
    const anchorElement = substituteAnchor?.() ?? anchor.current;
    const element = layer.current;
    if (anchorElement === null || element === null) return;
    const size = layerSizeOf(element);
    setStyle(previous => {
      const next = place(anchorElement.getBoundingClientRect(), size);
      return samePlacement(previous, next) ? previous : next;
    });
  }, [anchor, layer, place, substituteAnchor]);

  useLayoutEffect(() => {
    if (!active) return;
    const onViewportChange = () => remeasure();
    window.addEventListener(VIEWPORT_RESIZE_EVENT, onViewportChange);
    window.addEventListener(VIEWPORT_SCROLL_EVENT, onViewportChange, true);
    return () => {
      window.removeEventListener(VIEWPORT_RESIZE_EVENT, onViewportChange);
      window.removeEventListener(VIEWPORT_SCROLL_EVENT, onViewportChange, true);
    };
  }, [active, remeasure]);

  useEffect(() => {
    if (!active || onDismiss === undefined) return;
    const onPointerDown = (event: MouseEvent) => {
      const target = event.target;
      if (!(target instanceof Node)) return;
      if (!pointerLeftControl(target, control?.current ?? null, layer.current)) return;
      onDismiss();
    };
    document.addEventListener("mousedown", onPointerDown);
    return () => document.removeEventListener("mousedown", onPointerDown);
  }, [active, control, layer, onDismiss]);

  return { style, remeasure };
}
