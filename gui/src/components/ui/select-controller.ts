/**
 * The Select's interaction authority, without any rendering.
 *
 * Everything a Select decides lives here: whether the menu is showing, which option is
 * highlighted, which option is committed, which ids the combobox owes its listbox, and what a
 * trigger keypress means. The React projection in `select-view.tsx` only turns this model into
 * elements, so the state machine can be exercised directly and the keyboard policy stays where
 * it already was (`select-policy.ts`).
 */
import { useCallback, useId, useReducer } from "react";
import type { ReactNode, RefObject } from "react";
import {
  applySelectTriggerKey,
  selectActiveIndex,
  selectActiveOptionId,
  selectListboxControls,
  selectOptionClassName,
  selectSelectedIndex,
  selectShouldRenderMenu,
} from "../../select-policy.ts";

/** One selectable value. */
export interface SelectOption {
  value: string;
  label: ReactNode;
}

/** Select menu state. */
export interface SelectMenuState {
  readonly open: boolean;
  /** Null means "follow the selected option". */
  readonly highlight: number | null;
}

export const CLOSED_SELECT_MENU: SelectMenuState = { open: false, highlight: null };

export type SelectMenuEvent =
  | { readonly kind: "open-at"; readonly index: number }
  | { readonly kind: "highlight"; readonly index: number }
  | { readonly kind: "hide" }
  | { readonly kind: "close" };

/**
 * `hide` and `close` are not the same event.
 *
 * Escape and an outside press dismiss the control, so the highlight is forgotten with the
 * menu. A Tab commit does not dismiss anything — it is the control handing focus onward — so
 * the highlight is kept for the next open.
 */
export function selectMenuState(state: SelectMenuState, event: SelectMenuEvent): SelectMenuState {
  switch (event.kind) {
    case "open-at":
      return { open: true, highlight: event.index };
    case "highlight":
      return { open: state.open, highlight: event.index };
    case "hide":
      return { open: false, highlight: state.highlight };
    case "close":
      return CLOSED_SELECT_MENU;
  }
}

/** One option, projected into everything its listbox button needs. */
export interface SelectRow {
  readonly index: number;
  readonly value: string;
  readonly label: ReactNode;
  readonly id: string;
  readonly selected: boolean;
  readonly className: string;
  readonly disabled: boolean;
}

export function selectRows(
  options: readonly SelectOption[],
  value: string,
  activeIndex: number,
  disabled: boolean,
  optionId: (index: number) => string,
): SelectRow[] {
  return options.map((option, index) => {
    const selected = option.value === value;
    return {
      index,
      value: option.value,
      label: option.label,
      id: optionId(index),
      selected,
      className: selectOptionClassName(selected, index === activeIndex),
      disabled,
    };
  });
}

/**
 * The ARIA relationships a combobox owes its listbox.
 *
 * Kept as one bundle because they are one contract: a screen reader is told what is expanded,
 * what it controls, and which option is active, and those three answers have to come from the
 * same state. `undefined` values are omitted by React, which is exactly the closed-menu case.
 */
export interface SelectTriggerRelations {
  readonly role: "combobox";
  readonly "aria-haspopup": "listbox";
  readonly "aria-expanded": boolean;
  readonly "aria-controls": string | undefined;
  readonly "aria-activedescendant": string | undefined;
  readonly "aria-label": string | undefined;
  readonly "aria-describedby": string | undefined;
  readonly title: string | undefined;
}

export function selectTriggerRelations(input: {
  open: boolean;
  listboxId: string;
  activeOptionId: string | undefined;
  label?: string;
  describedBy?: string;
  title?: string;
}): SelectTriggerRelations {
  return {
    role: "combobox",
    "aria-haspopup": "listbox",
    "aria-expanded": input.open,
    "aria-controls": selectListboxControls(input.open, input.listboxId),
    "aria-activedescendant": input.activeOptionId,
    "aria-label": input.label,
    "aria-describedby": input.describedBy,
    title: input.title,
  };
}

/** What the projection gives the controller. */
export interface SelectControllerInput {
  readonly value: string;
  readonly options: readonly SelectOption[];
  readonly onChange: (value: string) => void;
  readonly disabled?: boolean;
  readonly matchParent: boolean;
  readonly label?: string;
  readonly describedBy?: string;
  readonly title?: string;
}

export interface SelectController {
  readonly open: boolean;
  readonly selectedIndex: number;
  readonly activeIndex: number;
  readonly selectedLabel: ReactNode;
  readonly rendersMenu: boolean;
  readonly rows: readonly SelectRow[];
  readonly relations: SelectTriggerRelations;
  readonly listboxId: string;
  /** Anchor substitute for `matchParent`; null keeps the trigger's own box. */
  substituteAnchor(): HTMLElement | null;
  optionId(index: number): string;
  openMenu(index: number): void;
  closeMenu(restoreFocus?: boolean): void;
  toggle(): void;
  highlight(index: number): void;
  commit(index: number): void;
  handleTriggerKey(event: { key: string; preventDefault: () => void }): void;
}

/**
 * The nodes the controller drives.
 *
 * The projection owns them because it owns the elements; the controller only ever asks a ref
 * for its element, so nothing here reads a ref during render.
 */
export interface SelectControllerRefs {
  readonly wrapper: RefObject<HTMLDivElement | null>;
  readonly trigger: RefObject<HTMLButtonElement | null>;
}

export function useSelectController(
  { value, options, onChange, disabled, matchParent, label, describedBy, title }: SelectControllerInput,
  { wrapper, trigger }: SelectControllerRefs,
): SelectController {
  const [menuState, dispatch] = useReducer(selectMenuState, CLOSED_SELECT_MENU);
  const listboxId = useId();

  const optionId = (index: number) => `${listboxId}-${index}`;
  const off = disabled === true;
  const open = menuState.open;
  const selected = options.find(option => option.value === value);
  const selectedIndex = selectSelectedIndex(options.length, selected === undefined ? -1 : options.indexOf(selected));
  const activeIndex = selectActiveIndex(open, options.length, menuState.highlight, selectedIndex);
  const activeDescendant = selectActiveOptionId(open, options[activeIndex] !== undefined, optionId(activeIndex));
  const rows = selectRows(options, value, activeIndex, off, optionId);

  /**
   * The chip this Select should match, when it was asked to match its parent. Null means the
   * trigger's own box is the anchor.
   */
  const substituteAnchor = useCallback(
    () => (matchParent ? wrapper.current?.parentElement ?? null : null),
    [matchParent, wrapper],
  );

  const closeMenu = useCallback((restoreFocus = false) => {
    dispatch({ kind: "close" });
    if (restoreFocus) trigger.current?.focus();
  }, [trigger]);

  const openMenu = (index: number) => {
    if (off || options.length === 0) return;
    dispatch({ kind: "open-at", index: Math.max(0, Math.min(options.length - 1, index)) });
  };

  const highlight = (index: number) => dispatch({ kind: "highlight", index });
  /** Tab committing stops the menu being open without dismissing the control. */
  const hideMenu = () => dispatch({ kind: "hide" });

  const commit = (index: number) => {
    if (off) return;
    const option = options[index];
    if (option === undefined) return;
    onChange(option.value);
    closeMenu(true);
  };

  const toggle = () => {
    if (off) return;
    if (open) closeMenu();
    else openMenu(selectedIndex);
  };

  const handleTriggerKey = (event: { key: string; preventDefault: () => void }) => {
    applySelectTriggerKey(event, {
      disabled: off,
      open,
      activeIndex,
      selectedIndex,
      optionCount: options.length,
      options,
      onChange,
      openAt: openMenu,
      close: closeMenu,
      selectIndex: commit,
      setOpen: next => (next ? openMenu(selectedIndex) : hideMenu()),
    });
  };

  return {
    open,
    selectedIndex,
    activeIndex,
    selectedLabel: selected?.label ?? value,
    rendersMenu: selectShouldRenderMenu(open, disabled),
    rows,
    relations: selectTriggerRelations({
      open,
      listboxId,
      activeOptionId: activeDescendant,
      label,
      describedBy,
      title,
    }),
    listboxId,
    substituteAnchor,
    optionId,
    openMenu,
    closeMenu,
    toggle,
    highlight,
    commit,
    handleTriggerKey,
  };
}
