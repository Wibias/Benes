import type { CSSProperties } from "react";

export type SelectKeyCommand =
  | { type: "move"; index: number }
  | { type: "open"; index: number }
  | { type: "commit"; index: number }
  | { type: "close" }
  | { type: "tab-commit"; index: number }
  | { type: "noop" };

export type SelectTriggerKeyInput = {
  key: string;
  open: boolean;
  disabled: boolean;
  activeIndex: number;
  selectedIndex: number;
  optionCount: number;
};

export type SelectKeyRuntime<TOption extends { value: string }> = {
  disabled: boolean;
  open: boolean;
  activeIndex: number;
  selectedIndex: number;
  optionCount: number;
  options: readonly TOption[];
  onChange: (value: string) => void;
  openAt: (index: number) => void;
  close: (restoreFocus?: boolean) => void;
  selectIndex: (index: number) => void;
  setOpen: (open: boolean) => void;
};

export function selectSelectedIndex(optionCount: number, foundIndex: number): number {
  return optionCount === 0 ? 0 : Math.max(0, foundIndex);
}

export function selectActiveIndex(
  open: boolean,
  optionCount: number,
  highlightIndex: number | null,
  selectedIndex: number,
): number {
  if (!open || optionCount === 0) return selectedIndex;
  return Math.min(highlightIndex ?? selectedIndex, optionCount - 1);
}

export function selectShouldRenderMenu(open: boolean, disabled?: boolean): boolean {
  return open && !disabled;
}

export function selectDropdownClassName(
  portal: boolean,
  align?: "left" | "right",
  placement?: "below" | "right",
): string {
  const classes = ["select-dropdown"];
  if (portal) classes.push("select-dropdown-portal");
  if (!portal && align === "right") classes.push("select-dropdown-right");
  if (!portal && placement === "right") classes.push("select-dropdown-beside");
  return classes.join(" ");
}

export function selectOptionClassName(selected: boolean, active: boolean): string {
  return `select-option${selected ? " active" : ""}${active ? " select-option-active" : ""}`;
}

export function selectDropdownStyle(
  portal: boolean,
  menuStyle: CSSProperties | undefined,
  dropdownStyle?: CSSProperties,
): CSSProperties | undefined {
  if (!portal) return dropdownStyle;
  return { ...menuStyle, zIndex: 60, ...dropdownStyle };
}

export type SelectChevronOrientation = "side" | "down";

/** Side: closed `>`, open `v`. Down: closed `v`, open `^` (right-pointing IconChevron). */
export function selectChevronTransform(
  open: boolean,
  orientation: SelectChevronOrientation = "side",
): string {
  if (orientation === "down") return open ? "rotate(-90deg)" : "rotate(90deg)";
  return open ? "rotate(90deg)" : "none";
}

export function selectListboxControls(open: boolean, listboxId: string): string | undefined {
  return open ? listboxId : undefined;
}

export function selectActiveOptionId(
  open: boolean,
  hasActiveOption: boolean,
  optionId: string,
): string | undefined {
  if (!open || !hasActiveOption) return undefined;
  return optionId;
}

function lastOptionIndex(input: SelectTriggerKeyInput): number {
  return input.optionCount - 1;
}

function commitOrOpen(input: SelectTriggerKeyInput): SelectKeyCommand {
  if (input.open) return { type: "commit", index: input.activeIndex };
  return { type: "open", index: input.selectedIndex };
}

const SELECT_KEY_COMMANDS: Record<string, (input: SelectTriggerKeyInput) => SelectKeyCommand> = {
  ArrowDown: (input) => ({
    type: "move",
    index: input.open ? Math.min(lastOptionIndex(input), input.activeIndex + 1) : input.selectedIndex,
  }),
  ArrowUp: (input) => ({
    type: "move",
    index: input.open ? Math.max(0, input.activeIndex - 1) : input.selectedIndex,
  }),
  Home: () => ({ type: "move", index: 0 }),
  End: (input) => ({ type: "move", index: lastOptionIndex(input) }),
  Enter: commitOrOpen,
  " ": commitOrOpen,
  Escape: (input) => (input.open ? { type: "close" } : { type: "noop" }),
  Tab: (input) => (input.open ? { type: "tab-commit", index: input.activeIndex } : { type: "noop" }),
};

export function selectTriggerKeyCommand(input: SelectTriggerKeyInput): SelectKeyCommand {
  if (input.disabled) return { type: "noop" };
  const handler = SELECT_KEY_COMMANDS[input.key];
  if (!handler) return { type: "noop" };
  return handler(input);
}

function applyIndexedSelectCommand<TOption extends { value: string }>(
  event: { preventDefault: () => void },
  runtime: SelectKeyRuntime<TOption>,
  command: Extract<SelectKeyCommand, { index: number }>,
): void {
  if (command.type === "commit") {
    event.preventDefault();
    runtime.selectIndex(command.index);
    return;
  }
  if (command.type === "tab-commit") {
    const option = runtime.options[command.index];
    if (option) runtime.onChange(option.value);
    runtime.setOpen(false);
    return;
  }
  event.preventDefault();
  runtime.openAt(command.index);
}

export function applySelectTriggerKey<TOption extends { value: string }>(
  event: { key: string; preventDefault: () => void },
  runtime: SelectKeyRuntime<TOption>,
): void {
  const command = selectTriggerKeyCommand({
    key: event.key,
    open: runtime.open,
    disabled: runtime.disabled,
    activeIndex: runtime.activeIndex,
    selectedIndex: runtime.selectedIndex,
    optionCount: runtime.optionCount,
  });
  if (command.type === "noop") return;
  if (command.type === "close") {
    event.preventDefault();
    runtime.close(true);
    return;
  }
  applyIndexedSelectCommand(event, runtime, command);
}
