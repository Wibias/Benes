/**
 * The Select's rendering projection.
 *
 * The controller in `select-controller.ts` owns the state and the ARIA relationships; the
 * floating layer in `floating-layer.ts` owns placement and dismissal. What is left here is
 * turning that model into the combobox and listbox elements — the part that has to stay in
 * step with the stylesheet and the rendered contract.
 */
import { useCallback, useLayoutEffect, useRef } from "react";
import type { CSSProperties } from "react";
import { createPortal } from "react-dom";
import { IconChevron } from "../../icons";
import { computeSelectMenuStyle } from "../../select-position";
import { selectChevronTransform, selectDropdownClassName, selectDropdownStyle } from "../../select-policy";
import { SELECT_CHEVRON_STYLE, SELECT_WRAPPER_STYLE } from "../../ui-presentation";
import { useFloatingLayer } from "./floating-layer";
import type { AnchorBox, LayerSize } from "./floating-layer";
import { useSelectController } from "./select-controller";
import type { SelectOption } from "./select-controller";

export interface SelectProps {
  readonly value: string;
  readonly options: SelectOption[];
  readonly onChange: (value: string) => void;
  readonly disabled?: boolean;
  /** Put on the trigger, so a sibling `<label htmlFor>` can name it — a button is labelable. */
  readonly id?: string;
  readonly label?: string;
  readonly describedBy?: string;
  readonly title?: string;
  readonly style?: CSSProperties;
  readonly align?: "left" | "right";
  readonly placement?: "below" | "right";
  readonly dropdownStyle?: CSSProperties;
  /** When true (default), the menu is portaled and flips above the trigger if it would leave the viewport. */
  readonly portal?: boolean;
  /** Size and align the menu to the parent control (e.g. a labeled filter chip). */
  readonly matchParent?: boolean;
  /** Closed chevron: side `>` (default), down `v`. */
  readonly chevron?: "side" | "down";
}

export function Select({
  value,
  options,
  onChange,
  disabled,
  id,
  label,
  describedBy,
  title,
  style,
  align,
  placement,
  dropdownStyle,
  portal = true,
  matchParent = false,
  chevron = "side",
}: SelectProps) {
  const wrapperRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const controller = useSelectController(
    { value, options, onChange, disabled, matchParent, label, describedBy, title },
    { wrapper: wrapperRef, trigger: triggerRef },
  );

  /**
   * A matched chip is the anchor itself: the layer hands the parent's box straight to the
   * placement, and passing it on as `match` is what tells the geometry to copy its width.
   */
  const placeMenu = useCallback(
    (anchor: AnchorBox, size: LayerSize | null) =>
      computeSelectMenuStyle(anchor, {
        align,
        placement,
        menuHeight: size?.height,
        match: matchParent ? anchor : undefined,
      }),
    [align, matchParent, placement],
  );

  const layer = useFloatingLayer<CSSProperties>({
    open: controller.open,
    enabled: portal,
    anchor: triggerRef,
    substituteAnchor: controller.substituteAnchor,
    layer: menuRef,
    control: wrapperRef,
    place: placeMenu,
    onDismiss: controller.closeMenu,
  });

  /**
   * The menu measures itself when it lands in the DOM. The element is keyed by the option count
   * so a list that changes shape while open is a new menu: it re-attaches, re-measures and
   * re-places, and an inline menu needs none of that.
   */
  const remeasure = layer.remeasure;
  const attachMenu = useCallback((element: HTMLDivElement | null) => {
    menuRef.current = element;
    if (element !== null) remeasure();
  }, [remeasure]);

  const activeOptionId = `${controller.listboxId}-${controller.activeIndex}`;
  useLayoutEffect(() => {
    if (!controller.open || menuRef.current === null) return;
    menuRef.current.querySelector<HTMLElement>(`[id="${activeOptionId}"]`)?.scrollIntoView({ block: "nearest" });
  }, [activeOptionId, controller.open]);

  const menu = controller.rendersMenu ? (
    <div
      key={options.length}
      ref={attachMenu}
      id={controller.listboxId}
      className={selectDropdownClassName(portal, align, placement)}
      role="listbox"
      aria-label={label}
      style={selectDropdownStyle(portal, layer.style, dropdownStyle)}
    >
      {controller.rows.map(row => (
        <button
          key={row.value}
          id={row.id}
          type="button"
          role="option"
          tabIndex={-1}
          disabled={row.disabled}
          aria-selected={row.selected}
          className={row.className}
          onMouseEnter={() => controller.highlight(row.index)}
          onClick={() => controller.commit(row.index)}
        >
          {row.label}
        </button>
      ))}
    </div>
  ) : null;

  /** A portaled menu leaves the control's stacking context; an inline one stays in it. */
  const mountedMenu = portal ? createPortal(menu, document.body) : menu;

  return (
    <div ref={wrapperRef} className="custom-select" style={{ ...SELECT_WRAPPER_STYLE, ...style }}>
      <button
        ref={triggerRef}
        id={id}
        type="button"
        {...controller.relations}
        className="select-trigger"
        onClick={controller.toggle}
        onKeyDown={controller.handleTriggerKey}
        disabled={disabled}
      >
        <span>{controller.selectedLabel}</span>
        <IconChevron style={{ ...SELECT_CHEVRON_STYLE, transform: selectChevronTransform(controller.open, chevron) }} />
      </button>
      {mountedMenu}
    </div>
  );
}
