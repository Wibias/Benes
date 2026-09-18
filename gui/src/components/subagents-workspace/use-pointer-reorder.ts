import { useCallback, useEffect, useRef, useState, type PointerEvent as ReactPointerEvent } from "react";
import { cssPx, cssTransformTransition, cssTranslate3d, cssTranslateY } from "../../css-transform";
import {
  clampGhostY,
  findVerticalScrollParent,
  insertIndexFromY,
  moveToIndex,
  reorderShiftY,
  scrollEdgeDelta,
  shiftSlotsY,
} from "./roster";

type DragState = {
  id: string;
  pointerId: number;
  originY: number;
  offsetY: number;
  left: number;
  height: number;
  fromIndex: number;
  slots: { top: number; bottom: number }[];
  ids: string[];
  commit: (sourceId: string, targetId: string) => void;
  fillGhost: (ghost: HTMLElement, hoverIndex: number) => void;
  paintItem?: (el: HTMLElement, id: string, order: string[]) => void;
  lifted: boolean;
  lastClientY: number;
};

export function usePointerReorder({
  boundSelector,
  itemAttr,
}: {
  boundSelector: string;
  itemAttr: string;
}) {
  const listRef = useRef<HTMLElement | null>(null);
  const ghostRef = useRef<HTMLElement | null>(null);
  const drag = useRef<DragState | null>(null);
  const scrollRaf = useRef(0);
  const [draggingId, setDraggingId] = useState<string | null>(null);

  useEffect(() => {
    const items = () => {
      const root = listRef.current;
      if (!root) return [] as HTMLElement[];
      return [...root.querySelectorAll<HTMLElement>(`[${itemAttr}]`)];
    };

    const stopAutoScroll = () => {
      if (!scrollRaf.current) return;
      cancelAnimationFrame(scrollRaf.current);
      scrollRaf.current = 0;
    };

    const placeGhost = (clientY: number) => {
      const state = drag.current;
      const ghost = ghostRef.current;
      if (!state || !ghost) return;
      const bound = listRef.current?.closest(boundSelector)?.getBoundingClientRect();
      const y = bound
        ? clampGhostY(clientY - state.offsetY, bound.top, bound.bottom, state.height)
        : clientY - state.offsetY;
      ghost.style.transform = cssTranslate3d(state.left, y);
    };

    const freezeListHits = (frozen: boolean) => {
      const root = listRef.current;
      if (root) root.style.pointerEvents = frozen ? "none" : "";
    };

    const clearRowMotion = (ids?: string[], paintItem?: DragState["paintItem"]) => {
      freezeListHits(false);
      for (const row of items()) {
        row.style.transition = "none";
        row.style.transform = "";
        row.style.opacity = "";
        row.style.pointerEvents = "";
        const id = row.getAttribute(itemAttr);
        if (ids && id) paintItem?.(row, id, ids);
      }
    };

    const applyGap = (hoverIndex: number) => {
      const state = drag.current;
      if (!state) return;
      const reduce = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
      const preview = moveToIndex(state.ids, state.id, hoverIndex);
      for (const row of items()) {
        const id = row.getAttribute(itemAttr);
        if (!id) continue;
        state.paintItem?.(row, id, preview);
        if (id === state.id) {
          row.style.transition = "none";
          row.style.transform = "";
          row.style.opacity = "0";
          row.style.pointerEvents = "none";
          continue;
        }
        const index = state.ids.indexOf(id);
        const dy = reorderShiftY(state.fromIndex, hoverIndex, index, state.height);
        row.style.transition = reduce ? "none" : cssTransformTransition();
        row.style.transform = cssTranslateY(dy);
      }
    };

    const paintAt = (clientY: number) => {
      const state = drag.current;
      if (!state?.lifted) return;
      placeGhost(clientY);
      const hover = insertIndexFromY(clientY, state.slots, state.fromIndex);
      const ghost = ghostRef.current;
      if (ghost) state.fillGhost(ghost, hover);
      applyGap(hover);
    };

    const runAutoScroll = () => {
      scrollRaf.current = 0;
      const state = drag.current;
      if (!state?.lifted) return;
      const scroller = findVerticalScrollParent(listRef.current);
      if (!scroller) return;
      const box = scroller.getBoundingClientRect();
      const delta = scrollEdgeDelta(state.lastClientY, box.top, box.bottom);
      if (delta === 0) return;
      const before = scroller.scrollTop;
      scroller.scrollTop = before + delta;
      const scrolled = scroller.scrollTop - before;
      if (scrolled !== 0) {
        state.slots = shiftSlotsY(state.slots, scrolled);
        paintAt(state.lastClientY);
      }
      const atTop = scroller.scrollTop <= 0;
      const atBottom = scroller.scrollTop + scroller.clientHeight >= scroller.scrollHeight - 1;
      if ((delta < 0 && atTop) || (delta > 0 && atBottom)) return;
      if (scrollEdgeDelta(state.lastClientY, box.top, box.bottom) === 0) return;
      scrollRaf.current = requestAnimationFrame(runAutoScroll);
    };

    const ensureAutoScroll = () => {
      const state = drag.current;
      if (!state?.lifted || scrollRaf.current) return;
      const scroller = findVerticalScrollParent(listRef.current);
      if (!scroller) return;
      const box = scroller.getBoundingClientRect();
      if (scrollEdgeDelta(state.lastClientY, box.top, box.bottom) === 0) return;
      scrollRaf.current = requestAnimationFrame(runAutoScroll);
    };

    const lift = (clientY: number) => {
      const state = drag.current;
      const ghost = ghostRef.current;
      const row = state
        ? listRef.current?.querySelector(`[${itemAttr}="${CSS.escape(state.id)}"]`)
        : null;
      if (!state || !ghost || !(row instanceof HTMLElement)) return;
      const box = row.getBoundingClientRect();
      state.offsetY = clientY - box.top;
      state.left = box.left;
      state.height = box.height;
      state.lifted = true;
      state.lastClientY = clientY;
      ghost.style.width = cssPx(box.width);
      ghost.style.height = cssPx(box.height);
      state.fillGhost(ghost, state.fromIndex);
      ghost.hidden = false;
      placeGhost(clientY);
      freezeListHits(true);
      setDraggingId(state.id);
      ensureAutoScroll();
    };

    const finish = (clientY: number) => {
      stopAutoScroll();
      const state = drag.current;
      drag.current = null;
      const ghost = ghostRef.current;
      if (ghost) ghost.hidden = true;
      clearRowMotion(state?.ids, state?.paintItem);
      if (!state?.lifted) {
        setDraggingId(null);
        return;
      }
      const to = insertIndexFromY(clientY, state.slots, state.fromIndex);
      const targetId = state.ids[to];
      if (targetId && targetId !== state.id) state.commit(state.id, targetId);
      setDraggingId(null);
    };

    const onMove = (event: PointerEvent) => {
      const current = drag.current;
      if (!current || event.pointerId !== current.pointerId) return;
      current.lastClientY = event.clientY;
      if (!current.lifted) {
        if (Math.abs(event.clientY - current.originY) < 5) return;
        lift(event.clientY);
      }
      if (!drag.current?.lifted) return;
      paintAt(event.clientY);
      const scroller = findVerticalScrollParent(listRef.current);
      if (!scroller) {
        stopAutoScroll();
        return;
      }
      const box = scroller.getBoundingClientRect();
      if (scrollEdgeDelta(event.clientY, box.top, box.bottom) === 0) stopAutoScroll();
      else ensureAutoScroll();
    };

    const onUp = (event: PointerEvent) => {
      const current = drag.current;
      if (!current || event.pointerId !== current.pointerId) return;
      finish(event.clientY);
    };

    const ghostNode = ghostRef.current;
    document.addEventListener("pointermove", onMove);
    document.addEventListener("pointerup", onUp);
    document.addEventListener("pointercancel", onUp);
    return () => {
      document.removeEventListener("pointermove", onMove);
      document.removeEventListener("pointerup", onUp);
      document.removeEventListener("pointercancel", onUp);
      stopAutoScroll();
      drag.current = null;
      freezeListHits(false);
      if (ghostNode) ghostNode.hidden = true;
    };
  }, [boundSelector, itemAttr]);

  const begin = ({
    id,
    event,
    ids,
    onSelect,
    onCommit,
    fillGhost,
    paintItem,
  }: {
    id: string;
    event: ReactPointerEvent<HTMLElement>;
    ids: string[];
    onSelect: (id: string) => void;
    onCommit: (sourceId: string, targetId: string) => void;
    fillGhost: DragState["fillGhost"];
    paintItem?: DragState["paintItem"];
  }) => {
    if (event.button !== 0) return;
    event.preventDefault();
    event.stopPropagation();
    onSelect(id);
    const root = listRef.current;
    if (!root) return;
    const slots = [...root.querySelectorAll(`[${itemAttr}]`)].map((row) => {
      const box = row.getBoundingClientRect();
      return { top: box.top, bottom: box.bottom };
    });
    drag.current = {
      id,
      pointerId: event.pointerId,
      originY: event.clientY,
      offsetY: 0,
      left: 0,
      height: 0,
      fromIndex: ids.indexOf(id),
      slots,
      ids: ids.slice(),
      commit: onCommit,
      fillGhost,
      paintItem,
      lifted: false,
      lastClientY: event.clientY,
    };
  };

  const bindList = useCallback((node: HTMLElement | null) => {
    listRef.current = node;
  }, []);

  const bindGhost = useCallback((node: HTMLElement | null) => {
    ghostRef.current = node;
  }, []);

  return { draggingId, begin, bindList, bindGhost };
}
