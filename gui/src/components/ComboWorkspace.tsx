/**
 * Combos workspace controller.
 *
 * The rail picks a combo, the main pane shows what that selection means, and
 * this module owns the single source of truth for both: which pane is open, on
 * which combo, and whether the editor holds unsaved edits. Navigation is one
 * state object rather than parallel `useState`s so a pane change can never
 * update half of the selection, and an unsaved editor turns any navigation into
 * a pending request instead of dropping the draft.
 */

import { useCallback, useMemo, useState } from "react";
import {
  comboModelId,
  emptyDraft,
  filterCombos,
  type ComboItem,
} from "../combo-workspace-data";
import { DetailPanel } from "./combo-workspace-detail-panel";
import { RemoveComboDialog, UnsavedLeaveDialog } from "./combo-workspace-dialogs";
import { followComboAfterSave, otherComboIdentity } from "./combo-workspace-identity";
import { OverviewPanel } from "./combo-workspace-overview-panel";
import { ComboWorkspaceRail } from "./combo-workspace-rail";
import { ComboSelectedOverview } from "./combo-workspace-selected-overview";
import type { ComboWorkspaceProps, ModelOption, ProviderOption } from "./combo-workspace-types";

export type { ModelOption, ProviderOption, ComboWorkspaceProps } from "./combo-workspace-types";

/** Where the workspace is: a create draft, an editor, or a selected combo. */
type NavState = {
  mode: "browse" | "edit" | "create";
  selectedId: string | null;
  /** A just-saved combo the list has not caught up with yet. */
  recent: ComboItem | null;
};

const INITIAL_NAV: NavState = { mode: "browse", selectedId: null, recent: null };

/** A pending navigation the editor has to confirm before it happens. */
type PendingNav =
  | { kind: "select"; id: string | null }
  | { kind: "create" }
  | { kind: "edit" };

function navTargetsSelection(
  next: PendingNav,
  activeId: string | null,
  mode: NavState["mode"],
): boolean {
  if (next.kind === "select") return next.id === activeId && mode === "browse";
  if (next.kind === "edit") return mode === "edit";
  return mode === "create";
}

/** Entering the editor keeps the combo the user is looking at. */
function navFor(next: PendingNav, current: NavState): NavState {
  if (next.kind === "select") return { mode: "browse", selectedId: next.id, recent: null };
  if (next.kind === "create") return { mode: "create", selectedId: null, recent: null };
  return { mode: "edit", selectedId: current.selectedId, recent: current.recent };
}

function providerFlagsOf(providers: ProviderOption[]) {
  return Object.fromEntries(
    providers.map((provider) => [provider.name, { disabled: provider.disabled }]),
  );
}

export default function ComboWorkspace({
  combos,
  providers,
  models,
  cataloguedComboIds,
  onRefresh,
  onSave,
  onRemove,
}: ComboWorkspaceProps) {
  const providerFlags = useMemo(() => providerFlagsOf(providers), [providers]);
  const draft = useMemo(() => emptyDraft(), []);
  const [query, setQuery] = useState("");
  const [nav, setNav] = useState<NavState>(INITIAL_NAV);
  const [pending, setPending] = useState<PendingNav | null>(null);
  const [confirmRemoveId, setConfirmRemoveId] = useState<string | null>(null);
  const [editorDirty, setEditorDirty] = useState(false);

  const filtered = useMemo(() => filterCombos(combos, query), [combos, query]);
  // A selection survives only while the list still holds that combo.
  const activeId = combos.some((combo) => combo.id === nav.selectedId) ? nav.selectedId : null;
  const selected = combos.find((combo) => combo.id === activeId) ?? null;
  const baseline = selected !== null && nav.recent?.id === selected.id ? nav.recent : selected;
  const others = baseline === null
    ? { ids: [], aliases: [] }
    : otherComboIdentity(combos, baseline.id);

  const go = useCallback((next: PendingNav) => {
    setNav((current) => navFor(next, current));
    setEditorDirty(false);
  }, []);

  const requestNav = useCallback((next: PendingNav) => {
    if (navTargetsSelection(next, activeId, nav.mode)) return;
    if (editorDirty) {
      setPending(next);
      return;
    }
    go(next);
  }, [activeId, editorDirty, go, nav.mode]);

  const afterCreate = (item: ComboItem) => {
    setEditorDirty(false);
    setNav({ mode: "browse", selectedId: item.id, recent: item });
    onRefresh();
  };

  const afterEdit = (item: ComboItem, editedId: string) => {
    setEditorDirty(false);
    const follow = followComboAfterSave(item, editedId);
    setNav({ mode: "browse", selectedId: follow.selectedId, recent: follow.localBaseline });
    onRefresh();
  };

  const confirmRemove = async (id: string) => {
    const result = await onRemove(id);
    setConfirmRemoveId(null);
    if (!result.ok) return;
    if (activeId === id) setNav(INITIAL_NAV);
    setEditorDirty(false);
    onRefresh();
  };

  return (
    <div className="combos-workspace-root">
      <ComboWorkspaceRail
        combos={combos}
        query={query}
        onQueryChange={setQuery}
        filtered={filtered}
        activeId={activeId}
        creating={nav.mode === "create"}
        cataloguedComboIds={cataloguedComboIds}
        onSelect={(id) => requestNav({ kind: "select", id })}
        onAdd={() => requestNav({ kind: "create" })}
      />
      <div className="combos-workspace-main">
        <ComboMainPane
          nav={nav}
          baseline={baseline}
          draft={draft}
          combos={combos}
          others={others}
          providerFlags={providerFlags}
          providers={providers}
          models={models}
          cataloguedComboIds={cataloguedComboIds}
          onSave={onSave}
          onDirtyChange={setEditorDirty}
          onNavigate={requestNav}
          onCreated={afterCreate}
          onEdited={afterEdit}
          onRemoveRequested={setConfirmRemoveId}
        />
      </div>
      {confirmRemoveId !== null ? (
        <RemoveComboDialog
          model={combos.find((combo) => combo.id === confirmRemoveId)?.model
            ?? comboModelId(confirmRemoveId)}
          onCancel={() => setConfirmRemoveId(null)}
          onConfirm={() => { void confirmRemove(confirmRemoveId); }}
        />
      ) : null}
      {pending !== null && editorDirty ? (
        <UnsavedLeaveDialog
          onKeep={() => setPending(null)}
          onDiscard={() => {
            if (pending !== null) go(pending);
            setPending(null);
          }}
        />
      ) : null}
    </div>
  );
}

interface MainPaneProps {
  nav: NavState;
  baseline: ComboItem | null;
  draft: ComboItem;
  combos: ComboItem[];
  others: { ids: string[]; aliases: string[] };
  providerFlags: Readonly<Record<string, { disabled?: boolean }>>;
  providers: ProviderOption[];
  models: ModelOption[];
  cataloguedComboIds?: ReadonlySet<string>;
  onSave: ComboWorkspaceProps["onSave"];
  onDirtyChange: (dirty: boolean) => void;
  onNavigate: (next: PendingNav) => void;
  onCreated: (item: ComboItem) => void;
  onEdited: (item: ComboItem, editedId: string) => void;
  onRemoveRequested: (id: string) => void;
}

/** The right-hand pane: whichever of the four editor/overview states applies. */
function ComboMainPane({
  nav,
  baseline,
  draft,
  combos,
  others,
  providerFlags,
  providers,
  models,
  cataloguedComboIds,
  onSave,
  onDirtyChange,
  onNavigate,
  onCreated,
  onEdited,
  onRemoveRequested,
}: MainPaneProps) {
  if (nav.mode === "create") {
    return (
      <DetailPanel
        key="create-combo"
        baseline={draft}
        isCreate
        otherIds={combos.map((combo) => combo.id)}
        otherAliases={combos.flatMap((combo) => (combo.alias ? [combo.alias] : []))}
        providerMap={providerFlags}
        providers={providers}
        models={models}
        onCancel={() => onNavigate({ kind: "select", id: null })}
        onSaved={onCreated}
        onSave={onSave}
        onDirtyChange={onDirtyChange}
      />
    );
  }
  if (nav.mode === "edit" && baseline !== null) {
    return (
      <DetailPanel
        key={`edit-${baseline.id}`}
        baseline={baseline}
        otherIds={others.ids}
        otherAliases={others.aliases}
        providerMap={providerFlags}
        providers={providers}
        models={models}
        onBack={() => onNavigate({ kind: "select", id: null })}
        onCancel={() => onNavigate({ kind: "select", id: baseline.id })}
        onSaved={(item) => onEdited(item, baseline.id)}
        onRequestRemove={() => onRemoveRequested(baseline.id)}
        onSave={onSave}
        onDirtyChange={onDirtyChange}
      />
    );
  }
  if (baseline !== null) {
    return (
      <ComboSelectedOverview
        item={baseline}
        catalogued={Boolean(cataloguedComboIds?.has(baseline.id))}
        onBack={() => onNavigate({ kind: "select", id: null })}
        onEdit={() => onNavigate({ kind: "edit" })}
      />
    );
  }
  return <OverviewPanel hasCombos={combos.length > 0} onAdd={() => onNavigate({ kind: "create" })} />;
}