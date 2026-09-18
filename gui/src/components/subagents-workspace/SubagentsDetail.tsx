/**
 * Sub-agents detail pane: assignment, exclusive role, capabilities, model order.
 */
import { type KeyboardEvent, type PointerEvent } from "react";
import { IconChevron, IconGrip, IconHelp, IconInfo } from "../../icons";
import { useT, type TKey } from "../../i18n/shared";
import { navigateHash } from "../../hash-routing";
import { Select, Switch, Tooltip } from "../../ui";
import { formatNamespacedModelId } from "../../provider-icons";
import SubagentDelegationSection, { ULTRA_MODE_PRESET } from "./SubagentDelegationSection";
import type { SubagentsDelegationProps } from "./SubagentsRail";
import {
  ModelMark,
} from "./SubagentsRail";
import {
  effortLabel,
  ROLE_CHOICES,
  roleChoiceOf,
  roleFromChoice,
  roleOf,
  type SubagentRole,
  type SubagentRoleChoice,
} from "./roster";
import { usePointerReorder } from "./use-pointer-reorder";

const ROLE_CHOICE_KEYS: Record<SubagentRoleChoice, TKey> = {
  assigned: "sub.badge.assigned",
  fallback: "sub.role.fallback",
  unassigned: "sub.role.unassigned",
};

const ROLE_CHOICE_HINT_KEYS: Record<SubagentRoleChoice, TKey> = {
  assigned: "sub.role.featuredHint",
  fallback: "sub.role.fallbackHint",
  unassigned: "sub.role.unassignedHint",
};

const ORDER_ROLE_KEYS: Record<SubagentRole, TKey> = {
  featured: "sub.badge.assigned",
  firstCall: "sub.role.firstCall",
  fallback: "sub.role.fallback",
  unassigned: "sub.role.unassigned",
};

export function SubagentsDetail({
  active,
  assignedRows,
  chosenSet,
  fallbackSet,
  busy,
  fallbackBusy,
  onSelect,
  onRole,
  onDragReorder,
  onPosition,
  delegation,
  ultraOn,
}: {
  active: string | null;
  assignedRows: string[];
  chosenSet: Set<string>;
  fallbackSet: Set<string>;
  busy: boolean;
  fallbackBusy: boolean;
  onSelect: (id: string) => void;
  onRole: (id: string, role: SubagentRole) => void;
  onDragReorder: (sourceId: string, targetId: string) => void;
  onPosition: (id: string, position: number) => void;
  delegation: SubagentsDelegationProps;
  ultraOn: boolean;
}) {
  const t = useT();
  if (!active) {
    return (
      <article className="subagents-detail">
        <p className="subagents-empty">{t("sub.detail.empty")}</p>
      </article>
    );
  }
  const role = roleOf(active, chosenSet, delegation.model, fallbackSet);
  const featuredIndex = assignedRows.indexOf(active);
  const rosterLocked = busy || fallbackBusy;
  return (
    <article className="subagents-detail">
      <div className="subagents-detail-head">
        <div className="subagents-detail-title">
          <ModelMark id={active} />
          <h3>{formatNamespacedModelId(active, t)}</h3>
        </div>
        <DetailActions />
      </div>
      <section className="subagents-assign">
        <AssignmentColumn
          active={active}
          role={role}
          featuredIndex={featuredIndex}
          rosterLength={assignedRows.length}
          locked={rosterLocked}
          onRole={onRole}
          onPosition={onPosition}
          delegation={delegation}
        />
        <RoleColumn role={role} locked={rosterLocked} onPick={(next) => onRole(active, next)} />
        <CapabilitiesColumn delegation={delegation} />
      </section>
      {ultraOn && (
        <section className="subagents-block subagents-block--ultra">
          <h4>{t("sub.ultraMode")}</h4>
          <SubagentDelegationSection {...delegation} compact slices={["editor"]} />
        </section>
      )}
      <OrderTable
        assignedRows={assignedRows}
        active={active}
        chosenSet={chosenSet}
        firstCallModel={delegation.model}
        fallbackSet={fallbackSet}
        busy={rosterLocked}
        onSelect={onSelect}
        onDragReorder={onDragReorder}
      />
    </article>
  );
}

function AssignmentColumn({
  active,
  role,
  featuredIndex,
  rosterLength,
  locked,
  onRole,
  onPosition,
  delegation,
}: {
  active: string;
  role: SubagentRole;
  featuredIndex: number;
  rosterLength: number;
  locked: boolean;
  onRole: (id: string, role: SubagentRole) => void;
  onPosition: (id: string, position: number) => void;
  delegation: SubagentsDelegationProps;
}) {
  const t = useT();
  const isPrimary = role === "firstCall";
  const effortDisabled = locked || delegation.saving || !isPrimary || delegation.efforts.length === 0;
  return (
    <div className="subagents-col">
      <h4><span>1</span>{t("sub.assignment.title")}</h4>
      <div className="subagents-fact-row subagents-fact-row--switch">
        <span>{t("sub.assignment.primary")}</span>
          <Switch
            on={isPrimary}
            disabled={locked || delegation.saving}
            label={t("sub.assignment.primary")}
            onClick={() => onRole(active, isPrimary ? "featured" : "firstCall")}
          />
      </div>
      <div className="subagents-fact-row">
        <span>{t("sub.assignment.effort")}</span>
        <Select
          value={isPrimary ? delegation.effort : ""}
          options={[
            { value: "", label: t("dash.injectionEffortNone") },
            ...delegation.efforts.map((item) => ({ value: item, label: effortLabel(item) })),
          ]}
          onChange={(value) => delegation.onSave({ model: active, effort: value || null })}
          disabled={effortDisabled}
          label={t("sub.assignment.effort")}
          align="left"
          dropdownStyle={{ background: "var(--surface)", color: "var(--text)" }}
        />
      </div>
      <div className="subagents-fact-row">
        <span>{t("sub.assignment.position")}</span>
        {featuredIndex >= 0 ? (
          <input
            className="input subagents-position"
            type="number"
            min={1}
            max={rosterLength}
            value={featuredIndex + 1}
            disabled={locked}
            aria-label={t("sub.assignment.position")}
            onChange={(event) => {
              const raw = event.target.value;
              if (!raw) return;
              const next = Number(raw);
              if (!Number.isInteger(next) || next < 1) return;
              onPosition(active, next - 1);
            }}
          />
        ) : (
          <span className="muted">{t("sub.assignment.none")}</span>
        )}
      </div>
    </div>
  );
}

function RoleColumn({
  role,
  locked,
  onPick,
}: {
  role: SubagentRole;
  locked: boolean;
  onPick: (role: SubagentRole) => void;
}) {
  const t = useT();
  return (
    <div className="subagents-col">
      <div className="subagents-col-head">
        <h4><span>2</span>{t("sub.role.title")}</h4>
        <Tooltip
          content={
            <div className="subagents-role-hint">
              {ROLE_CHOICES.map((option) => (
                <p key={option}>
                  <strong>{t(ROLE_CHOICE_KEYS[option])}</strong>
                  {" "}
                  {t(ROLE_CHOICE_HINT_KEYS[option])}
                </p>
              ))}
            </div>
          }
          side="bottom"
          maxWidth={320}
        >
          <span className="subagents-rail-info">
            <span className="sr-only">{t("sub.role.help")}</span>
            <IconHelp />
          </span>
        </Tooltip>
      </div>
      <div className="subagents-roles" role="radiogroup" aria-label={t("sub.role.title")}>
        {ROLE_CHOICES.map((option) => (
          <label key={option} className="subagents-role">
            <input
              type="radio"
              name="subagent-role"
              checked={roleChoiceOf(role) === option}
              disabled={locked}
              onChange={() => onPick(roleFromChoice(option, role))}
            />
            {t(ROLE_CHOICE_KEYS[option])}
          </label>
        ))}
      </div>
    </div>
  );
}

function CapabilitiesColumn({ delegation }: { delegation: SubagentsDelegationProps }) {
  const t = useT();
  if (delegation.ultraLoadFailed) {
    return (
      <div className="subagents-col">
        <h4><span>3</span>{t("sub.capabilities.title")}</h4>
        <SubagentDelegationSection {...delegation} compact slices={["policies"]} />
      </div>
    );
  }
  const ultraOn = (delegation.ultraMode.hintText ?? "").trim().length > 0;
  const v2Ready = delegation.ultraMode.multiAgentV2Enabled;
  return (
    <div className="subagents-col">
      <h4><span>3</span>{t("sub.capabilities.title")}</h4>
      <div className="subagents-fact-row subagents-fact-row--switch">
        <span>{t("sub.capabilities.sync")}</span>
        <Switch
          on={delegation.syncCodexDefaults}
          disabled={delegation.saving || !delegation.model}
          label={t("sub.capabilities.sync")}
          onClick={() => delegation.onSave({ syncCodexSubagentDefaults: !delegation.syncCodexDefaults })}
        />
      </div>
      <div className="subagents-fact-row subagents-fact-row--switch">
        <span>{t("sub.capabilities.guidance")}</span>
        <Switch
          on={delegation.guidanceEnabled}
          disabled={delegation.saving}
          label={t("sub.capabilities.guidance")}
          onClick={() => delegation.onSave({ multiAgentGuidanceEnabled: !delegation.guidanceEnabled })}
        />
      </div>
      <div className="subagents-fact-row subagents-fact-row--switch">
        <span className="subagents-v2-label">
          {t("sub.capabilities.v2")}
          <Tooltip content={t("sub.capabilities.v2Hint")} side="bottom">
            <span className="subagents-rail-info" aria-label={t("sub.capabilities.v2Hint")}>
              <IconInfo />
            </span>
          </Tooltip>
        </span>
        <Switch
          on={ultraOn}
          disabled={delegation.saving || delegation.ultraSaving || (!ultraOn && !v2Ready)}
          label={t("sub.capabilities.v2")}
          onClick={() => delegation.onUltraModeSave({
            multiAgentModeHintText: ultraOn ? null : ULTRA_MODE_PRESET,
          })}
        />
      </div>
    </div>
  );
}

function DetailActions() {
  const t = useT();
  return (
    <div className="subagents-detail-links">
      <ActionLink label={t("sub.actions.routing")} onClick={() => navigateHash("startup/routing")} />
      <ActionLink label={t("sub.actions.sessions")} onClick={() => navigateHash("sessions")} />
    </div>
  );
}

function ActionLink({
  label,
  onClick,
}: {
  label: string;
  onClick: () => void;
}) {
  return (
    <button type="button" className="subagents-link" onClick={onClick}>
      <span>{label}</span>
      <IconChevron />
    </button>
  );
}

function OrderTable({
  assignedRows,
  active,
  chosenSet,
  firstCallModel,
  fallbackSet,
  busy,
  onSelect,
  onDragReorder,
}: {
  assignedRows: string[];
  active: string;
  chosenSet: Set<string>;
  firstCallModel: string;
  fallbackSet: Set<string>;
  busy: boolean;
  onSelect: (id: string) => void;
  onDragReorder: (sourceId: string, targetId: string) => void;
}) {
  const t = useT();
  const { draggingId, begin, bindList, bindGhost } = usePointerReorder({
    boundSelector: ".subagents-block--order",
    itemAttr: "data-subagents-order-id",
  });

  const onGripPointerDown = (modelId: string, event: PointerEvent<HTMLButtonElement>) => {
    if (busy) return;
    const label = formatNamespacedModelId(modelId, t);
    const roleLabel = t(ORDER_ROLE_KEYS[roleOf(modelId, chosenSet, firstCallModel, fallbackSet)]);
    begin({
      id: modelId,
      event,
      ids: assignedRows,
      onSelect,
      onCommit: onDragReorder,
      fillGhost: (ghost, hoverIndex) => {
        const index = ghost.querySelector(".subagents-order-ghost-index");
        const model = ghost.querySelector(".subagents-order-ghost-model");
        const role = ghost.querySelector(".subagents-order-ghost-role");
        if (index) index.textContent = String(hoverIndex + 1);
        if (model) model.textContent = label;
        if (role) role.textContent = roleLabel;
      },
      paintItem: (row, id, order) => {
        const cell = row.querySelector(".subagents-order-index");
        if (!(cell instanceof HTMLElement)) return;
        const n = order.indexOf(id);
        if (n >= 0) cell.textContent = String(n + 1);
      },
    });
  };

  return (
    <section className="subagents-block subagents-block--order">
      <h4>{t("sub.order.title")}</h4>
      <p className="subagents-order-hint">{t("sub.order.hint")}</p>
      {assignedRows.length === 0 ? (
        <p className="subagents-empty">{t("sub.noneSelected")}</p>
      ) : (
        <>
          <div className={`subagents-order-scroll${draggingId ? " is-reordering" : ""}`}>
            <table className="subagents-order">
              <thead>
                <tr>
                  <th className="subagents-order-grip" />
                  <th>#</th>
                  <th>{t("sub.pool.model")}</th>
                  <th className="subagents-order-role">{t("sub.fallback.role")}</th>
                </tr>
              </thead>
              <tbody ref={bindList}>
                {assignedRows.map((modelId, index) => (
                  <OrderRow
                    key={modelId}
                    modelId={modelId}
                    index={index}
                    role={roleOf(modelId, chosenSet, firstCallModel, fallbackSet)}
                    active={active === modelId}
                    dragging={draggingId === modelId}
                    busy={busy}
                    onSelect={onSelect}
                    onGripPointerDown={onGripPointerDown}
                  />
                ))}
              </tbody>
            </table>
          </div>
          <div
            ref={bindGhost}
            className="subagents-order-ghost"
            hidden
            aria-hidden="true"
          >
            <span className="subagents-order-ghost-grip"><IconGrip /></span>
            <span className="subagents-order-ghost-index" />
            <span className="subagents-order-ghost-model" />
            <span className="subagents-order-ghost-role" />
          </div>
        </>
      )}
    </section>
  );
}

function OrderRow({
  modelId,
  index,
  role,
  active,
  dragging,
  busy,
  onSelect,
  onGripPointerDown,
}: {
  modelId: string;
  index: number;
  role: SubagentRole;
  active: boolean;
  dragging: boolean;
  busy: boolean;
  onSelect: (id: string) => void;
  onGripPointerDown: (modelId: string, event: PointerEvent<HTMLButtonElement>) => void;
}) {
  const t = useT();

  const onRowPointerDown = (event: PointerEvent<HTMLTableRowElement>) => {
    if (event.button !== 0) return;
    const target = event.target;
    if (!(target instanceof Element)) return;
    if (target.closest(".subagents-order-grip-btn")) return;
    onSelect(modelId);
  };

  const onRowKeyDown = (event: KeyboardEvent<HTMLTableRowElement>) => {
    if (event.key !== "Enter" && event.key !== " ") return;
    event.preventDefault();
    onSelect(modelId);
  };

  return (
    <tr
      data-subagents-order-id={modelId}
      className={[active ? "is-active" : "", dragging ? "is-dragging" : ""].filter(Boolean).join(" ") || undefined}
      tabIndex={0}
      onPointerDown={onRowPointerDown}
      onClick={() => onSelect(modelId)}
      onKeyDown={onRowKeyDown}
    >
      <td className="subagents-order-grip">
        <button
          type="button"
          className="subagents-order-grip-btn"
          disabled={busy}
          aria-label={t("sub.order.hint")}
          onPointerDown={(event) => onGripPointerDown(modelId, event)}
          onDragStart={(event) => event.preventDefault()}
        >
          <IconGrip />
        </button>
      </td>
      <td className="subagents-order-index">{index + 1}</td>
      <td className="subagents-order-model">
        <button type="button" className="subagents-order-select" onClick={() => onSelect(modelId)}>
          {formatNamespacedModelId(modelId, t)}
        </button>
      </td>
      <td className="subagents-order-role">{t(ORDER_ROLE_KEYS[role])}</td>
    </tr>
  );
}
