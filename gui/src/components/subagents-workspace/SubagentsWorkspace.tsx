/**
 * Sub-agents board: roster, first-call delegation, fallbacks.
 * Compact header matches Providers. Named profiles are not a Benes object;
 * assignment is a model roster. An empty modelId uses the parent model.
 */
import { useMemo, useState } from "react";
import { useT } from "../../i18n/shared";
import { IconMore, IconRefresh } from "../../icons";
import { navigateHash } from "../../hash-routing";
import type { DelegationPatch, DelegationModelOption, UltraModePatch, UltraModeState } from "../../pages/use-subagent-delegation";
import { SubagentsRail, SubagentsKpi } from "./SubagentsRail";
import { SubagentsDetail } from "./SubagentsDetail";
import {
  FEATURED_MAX,
  applyRole,
  availableIds,
  moveToIndex,
  reconcileOrder,
  splitRosterOrder,
  type SubagentRole,
} from "./roster";

export interface SubagentsWorkspaceProps {
  available: string[];
  chosen: string[];
  busy?: boolean;
  onReorder: (models: string[]) => void;
  onRefresh: () => void;
  fallbacks: string[];
  fallbackBusy?: boolean;
  onFallbacks: (models: string[]) => void;
  delegation: {
    model: string;
    effort: string;
    efforts: string[];
    available: DelegationModelOption[];
    guidanceEnabled: boolean;
    syncCodexDefaults: boolean;
    saving: boolean;
    onSave: (patch: DelegationPatch) => void;
    ultraMode: UltraModeState;
    ultraSaving: boolean;
    onUltraModeSave: (patch: UltraModePatch) => void;
    ultraLoadFailed: boolean;
    onUltraModeRetry: () => void;
  };
}

export { FEATURED_MAX };

export default function SubagentsWorkspace({
  available,
  chosen,
  busy = false,
  onReorder,
  onRefresh,
  fallbacks,
  fallbackBusy = false,
  onFallbacks,
  delegation,
}: SubagentsWorkspaceProps) {
  const t = useT();
  const [query, setQuery] = useState("");
  const [selected, setSelected] = useState<string | null>(null);
  const [pinnedOrder, setPinnedOrder] = useState<string[]>([]);

  const chosenSet = useMemo(() => new Set(chosen), [chosen]);
  const fallbackSet = useMemo(() => new Set(fallbacks), [fallbacks]);
  const assigned = useMemo(
    () => reconcileOrder(pinnedOrder, chosen, fallbacks),
    [pinnedOrder, chosen, fallbacks],
  );
  const unassigned = useMemo(() => availableIds(available, assigned), [available, assigned]);
  const q = query.trim().toLowerCase();
  const assignedRows = useMemo(
    () => (q ? assigned.filter((id) => id.toLowerCase().includes(q)) : assigned),
    [assigned, q],
  );
  const availableRows = useMemo(
    () => (q ? unassigned.filter((id) => id.toLowerCase().includes(q)) : unassigned),
    [unassigned, q],
  );
  const active = selected && (chosenSet.has(selected) || available.includes(selected) || fallbackSet.has(selected))
    ? selected
    : (assigned[0] ?? available[0] ?? null);
  const ultraOn = (delegation.ultraMode.hintText ?? "").trim().length > 0;
  const policies = [
    delegation.syncCodexDefaults,
    delegation.guidanceEnabled,
    Boolean(delegation.model),
  ].filter(Boolean).length;
  const full = chosen.length >= FEATURED_MAX;

  const applyRoster = (order: string[]) => {
    const split = splitRosterOrder(order, chosenSet, fallbackSet);
    if (split.chosen.join("\0") !== chosen.join("\0")) onReorder(split.chosen);
    if (split.fallbacks.join("\0") !== fallbacks.join("\0")) onFallbacks(split.fallbacks);
  };

  const commitOrder = (next: string[]) => {
    if (next.join("\0") === assigned.join("\0")) return;
    setPinnedOrder(next);
    applyRoster(next);
  };

  const setRole = (id: string, role: SubagentRole) => {
    if (busy || fallbackBusy) return;
    const next = applyRole(id, role, chosen, fallbacks, delegation.model);
    if (next.chosen.join("\0") !== chosen.join("\0")) onReorder(next.chosen);
    if (next.fallbacks.join("\0") !== fallbacks.join("\0")) onFallbacks(next.fallbacks);
    if (next.firstCall !== delegation.model) {
      delegation.onSave({
        model: next.firstCall || null,
        effort: next.firstCall ? (delegation.effort || null) : null,
      });
    }
    setSelected(id);
  };

  const onDragReorder = (sourceId: string, targetId: string) => {
    if (busy) return;
    const toIndex = assigned.indexOf(targetId);
    if (toIndex < 0) return;
    commitOrder(moveToIndex(assigned, sourceId, toIndex));
  };

  return (
    <div className="subagents-board">
      <header className="subagents-head">
        <h2>{t("nav.subagents")}</h2>
        <section className="subagents-kpis" aria-label={t("sub.kpis.label")}>
          <SubagentsKpi value={assigned.length} label={t("sub.kpis.featured")} />
          <SubagentsKpi value={unassigned.length} label={t("sub.kpis.pool")} />
          <SubagentsKpi value={fallbacks.length} label={t("sub.kpis.fallbacks")} />
          <SubagentsKpi value={policies} label={t("sub.kpis.policies")} />
        </section>
        <div className="page-head-actions">
          <button
            type="button"
            className="btn btn-ghost btn-icon"
            onClick={() => {
              setPinnedOrder([]);
              onRefresh();
            }}
            aria-label={t("sub.refresh")}
          >
            <IconRefresh />
          </button>
          <details className="subagents-head-overflow">
            <summary aria-label={t("sub.head.more")}><IconMore /></summary>
            <div className="subagents-kebab-list">
              <button
                type="button"
                onClick={(event) => {
                  navigateHash("models");
                  event.currentTarget.closest("details")?.removeAttribute("open");
                }}
              >
                {t("sub.pool.manage")}
              </button>
              <button
                type="button"
                onClick={(event) => {
                  navigateHash("logs");
                  event.currentTarget.closest("details")?.removeAttribute("open");
                }}
              >
                {t("sub.actions.logs")}
              </button>
            </div>
          </details>
        </div>
      </header>
      <div className="subagents-split">
        <SubagentsRail
          query={query}
          onQueryChange={setQuery}
          assignedRows={assignedRows}
          availableRows={availableRows}
          chosenSet={chosenSet}
          firstCallModel={delegation.model}
          fallbackSet={fallbackSet}
          active={active}
          busy={busy}
          full={full}
          onSelect={setSelected}
          onAdd={(id) => setRole(id, "featured")}
          onRole={setRole}
          onDragReorder={onDragReorder}
          agentMode={delegation.ultraMode.multiAgentMode}
          agentModeBusy={delegation.ultraSaving}
          onAgentModeChange={(mode) => {
            delegation.onUltraModeSave({ multiAgentMode: mode });
          }}
        />
        <SubagentsDetail
          active={active}
          assignedRows={assigned}
          chosenSet={chosenSet}
          fallbackSet={fallbackSet}
          busy={busy}
          fallbackBusy={fallbackBusy}
          onSelect={setSelected}
          onRole={setRole}
          onDragReorder={onDragReorder}
          onPosition={(id, index) => {
            if (busy) return;
            commitOrder(moveToIndex(assigned, id, index));
          }}
          delegation={delegation}
          ultraOn={ultraOn}
        />
      </div>
    </div>
  );
}
