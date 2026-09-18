/**
 * Sub-agents roster rail: assigned spawn order, available catalog, row actions.
 */
import type { CSSProperties, PointerEvent } from "react";
import { IconGrip, IconInfo, IconPlus, IconSearch, IconX } from "../../icons";
import { useT, type TFn } from "../../i18n/shared";
import { Tooltip } from "../../ui";
import { formatNamespacedModelId } from "../../provider-icons";
import { ProviderMark } from "../ProviderMark";
import type { DelegationModelOption, MultiAgentMode } from "../../pages/use-subagent-delegation";
import {
  FEATURED_MAX,
  providerOf,
  roleOf,
  type SubagentRole,
} from "./roster";
import { usePointerReorder } from "./use-pointer-reorder";
import { SubagentsAgentInterface } from "./SubagentsAgentInterface";

function roleLabel(role: SubagentRole, t: TFn): string {
  if (role === "firstCall") return t("sub.badge.firstCall");
  if (role === "fallback") return t("sub.role.fallback");
  return "";
}

export function ModelMark({ id }: { id: string }) {
  return <ProviderMark name={providerOf(id)} className="subagents-mark" />;
}

export function SubagentsRail({
  query,
  onQueryChange,
  assignedRows,
  availableRows,
  chosenSet,
  firstCallModel,
  fallbackSet,
  active,
  busy,
  full,
  onSelect,
  onAdd,
  onRole,
  onDragReorder,
  agentMode,
  agentModeBusy,
  onAgentModeChange,
}: {
  query: string;
  onQueryChange: (value: string) => void;
  assignedRows: string[];
  availableRows: string[];
  chosenSet: Set<string>;
  firstCallModel: string;
  fallbackSet: Set<string>;
  active: string | null;
  busy: boolean;
  full: boolean;
  onSelect: (id: string) => void;
  onAdd: (id: string) => void;
  onRole: (id: string, role: SubagentRole) => void;
  onDragReorder: (sourceId: string, targetId: string) => void;
  agentMode: MultiAgentMode;
  agentModeBusy: boolean;
  onAgentModeChange: (mode: MultiAgentMode) => void;
}) {
  const t = useT();
  const { draggingId, begin, bindList, bindGhost } = usePointerReorder({
    boundSelector: ".subagents-assigned-list",
    itemAttr: "data-subagents-assigned-id",
  });
  const onGripPointerDown = (modelId: string, event: PointerEvent<HTMLButtonElement>) => {
    if (busy) return;
    const label = formatNamespacedModelId(modelId, t);
    const meta = roleLabel(roleOf(modelId, chosenSet, firstCallModel, fallbackSet), t);
    begin({
      id: modelId,
      event,
      ids: assignedRows,
      onSelect,
      onCommit: onDragReorder,
      fillGhost: (ghost) => {
        const model = ghost.querySelector(".subagents-assigned-ghost-model");
        const metaEl = ghost.querySelector(".subagents-assigned-ghost-meta");
        if (model) model.textContent = label;
        if (metaEl) metaEl.textContent = meta;
      },
    });
  };
  return (
    <section className="subagents-rail" aria-label={t("sub.roster.label")}>
      <SubagentsAgentInterface
        t={t}
        mode={agentMode}
        busy={agentModeBusy}
        onChange={onAgentModeChange}
      />
      <div className="subagents-rail-head">
        <h3>{t("sub.roster.label")}</h3>
        <Tooltip content={t("sub.roster.hint")} side="bottom">
          <span className="subagents-rail-info" aria-label={t("sub.roster.hint")}>
            <IconInfo />
          </span>
        </Tooltip>
      </div>
      <div className="subagents-search-row">
        <label className="subagents-search">
          <IconSearch />
          <span className="sr-only">{t("sub.roster.search")}</span>
          <input
            value={query}
            onChange={(event) => onQueryChange(event.target.value)}
            placeholder={t("sub.roster.search")}
          />
        </label>
      </div>
      <div className="subagents-groups">
        <div
          className={`subagents-group subagents-group--assigned${draggingId ? " is-reordering" : ""}`}
          style={{ "--sub-assigned-slots": String(FEATURED_MAX) } as CSSProperties}
        >
          <h3>{t("sub.roster.featured")}</h3>
          <ul
            className="subagents-assigned-list"
            ref={bindList}
          >
            {assignedRows.length === 0 ? (
              <li className="subagents-empty">{t("sub.noneSelected")}</li>
            ) : assignedRows.map((modelId) => (
              <AssignedRow
                key={modelId}
                modelId={modelId}
                role={roleOf(modelId, chosenSet, firstCallModel, fallbackSet)}
                active={active === modelId}
                dragging={draggingId === modelId}
                busy={busy}
                onSelect={onSelect}
                onRole={onRole}
                onGripPointerDown={onGripPointerDown}
              />
            ))}
          </ul>
          <div
            ref={bindGhost}
            className="subagents-assigned-ghost"
            hidden
            aria-hidden="true"
          >
            <span className="subagents-assigned-ghost-grip"><IconGrip /></span>
            <span className="subagents-assigned-ghost-copy">
              <span className="subagents-assigned-ghost-model" />
              <span className="subagents-assigned-ghost-meta" />
            </span>
          </div>
        </div>
        <div className="subagents-group subagents-group--available">
          <h3>{t("sub.roster.available")}</h3>
          <ul>
            {availableRows.length === 0 ? (
              <li className="subagents-empty">{t("sub.noModels")}</li>
            ) : availableRows.map((modelId) => (
              <AvailableRow
                key={modelId}
                modelId={modelId}
                active={active === modelId}
                busy={busy}
                full={full}
                onSelect={onSelect}
                onAdd={onAdd}
              />
            ))}
          </ul>
        </div>
      </div>
      <p className="subagents-rail-foot">{t("sub.roster.footer")}</p>
    </section>
  );
}

function AssignedRow({
  modelId,
  role,
  active,
  dragging,
  busy,
  onSelect,
  onRole,
  onGripPointerDown,
}: {
  modelId: string;
  role: SubagentRole;
  active: boolean;
  dragging: boolean;
  busy: boolean;
  onSelect: (id: string) => void;
  onRole: (id: string, role: SubagentRole) => void;
  onGripPointerDown: (modelId: string, event: PointerEvent<HTMLButtonElement>) => void;
}) {
  const t = useT();
  return (
    <li
      data-subagents-assigned-id={modelId}
      className={`subagents-assigned${active ? " is-active" : ""}${dragging ? " is-dragging" : ""}`}
    >
      <button
        type="button"
        className="subagents-grip"
        disabled={busy}
        aria-label={t("sub.order.hint")}
        onPointerDown={(event) => onGripPointerDown(modelId, event)}
        onDragStart={(event) => event.preventDefault()}
      >
        <IconGrip />
      </button>
      <button
        type="button"
        className="subagents-assigned-main"
        onClick={() => onSelect(modelId)}
      >
        <span className="subagents-row-name">{formatNamespacedModelId(modelId, t)}</span>
        <span className="subagents-row-meta">{roleLabel(role, t)}</span>
      </button>
      <SubagentsRemove
        modelId={modelId}
        busy={busy}
        onRemove={() => onRole(modelId, "unassigned")}
      />
    </li>
  );
}

function AvailableRow({
  modelId,
  active,
  busy,
  full,
  onSelect,
  onAdd,
}: {
  modelId: string;
  active: boolean;
  busy: boolean;
  full: boolean;
  onSelect: (id: string) => void;
  onAdd: (id: string) => void;
}) {
  const t = useT();
  return (
    <li className={`subagents-available${active ? " is-active" : ""}`}>
      <button
        type="button"
        className="subagents-available-main"
        onClick={() => onSelect(modelId)}
      >
        <ModelMark id={modelId} />
        <span className="subagents-row-name">{formatNamespacedModelId(modelId, t)}</span>
      </button>
      <button
        type="button"
        className="btn-icon subagents-add"
        disabled={busy || full}
        onClick={() => onAdd(modelId)}
        aria-label={t("sub.roster.add", { m: formatNamespacedModelId(modelId, t) })}
      >
        <IconPlus />
      </button>
    </li>
  );
}

export function SubagentsRemove({
  modelId,
  busy,
  onRemove,
}: {
  modelId: string;
  busy: boolean;
  onRemove: () => void;
}) {
  const t = useT();
  return (
    <button
      type="button"
      className="btn-icon subagents-remove"
      disabled={busy}
      aria-label={`${formatNamespacedModelId(modelId, t)} — ${t("sub.actions.removeRoster")}`}
      onPointerDown={(event) => event.stopPropagation()}
      onClick={(event) => {
        event.stopPropagation();
        onRemove();
      }}
    >
      <IconX />
    </button>
  );
}

export function SubagentsKpi({ value, label }: { value: number; label: string }) {
  return (
    <div className="subagents-kpi">
      <div className="subagents-kpi-value">{value}</div>
      <div className="subagents-kpi-label">{label}</div>
    </div>
  );
}

export type SubagentsDelegationProps = {
  model: string;
  effort: string;
  efforts: string[];
  available: DelegationModelOption[];
  guidanceEnabled: boolean;
  syncCodexDefaults: boolean;
  saving: boolean;
  onSave: (patch: import("../../pages/use-subagent-delegation").DelegationPatch) => void;
  ultraMode: import("../../pages/use-subagent-delegation").UltraModeState;
  ultraSaving: boolean;
  onUltraModeSave: (patch: import("../../pages/use-subagent-delegation").UltraModePatch) => void;
  ultraLoadFailed: boolean;
  onUltraModeRetry: () => void;
};
