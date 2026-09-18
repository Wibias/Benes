/**
 * The dashboard's shared UI primitives — the public entry point.
 *
 * The implementation lives under `components/ui/`, separated by responsibility rather than by
 * the historical single file: the stateless presentation primitives, the anchored-layer runtime
 * that both popups need, and the Select and Tooltip controllers with their projections. This
 * module re-exports that surface so every consumer keeps importing from one place and none of
 * them has to know which layer an export came from. Its import path is part of the dashboard's
 * own API and is not going away.
 */
export { EmptyState, Notice, Switch, ToastNotice } from "./components/ui/primitives";
export type {
  EmptyStateProps,
  NoticeProps,
  NoticeTone,
  SwitchProps,
  ToastNoticeProps,
} from "./components/ui/primitives";
export { Select } from "./components/ui/select-view";
export type { SelectProps } from "./components/ui/select-view";
export type { SelectOption } from "./components/ui/select-controller";
export { Tooltip } from "./components/ui/tooltip-view";
export type { TooltipProps } from "./components/ui/tooltip-view";
