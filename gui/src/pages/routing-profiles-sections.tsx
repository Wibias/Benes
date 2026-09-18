/** Benes dashboard client for the Go proxy (`internal/server`). */
import type {
  CSSProperties,
  Dispatch,
  ReactNode,
  RefObject,
  SetStateAction,
  PointerEvent as ReactPointerEvent,
} from "react";
import { useEffect, useId, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { Notice, ToastNotice } from "../ui";
import type { TFn } from "../i18n/shared";
import type { Locale } from "../i18n/shared";
import { IconArrowLeft, IconPencil, IconSearch } from "../icons";
import { computeSelectMenuStyle } from "../select-position";
import {
  type ModelOption,
  type RoutingProfileDraft,
  type RoutingProfileDto,
} from "../routing-profile/profile-model";
import type { LabCatalogSuite } from "../lab/lab-catalog";
import {
  ROUTING_PROFILE_DEFAULT_ICON,
  ensureRoutingProfileIconCatalog,
  filterRoutingProfileIconIds,
  type RoutingProfileIconCatalog,
} from "./routing-profile-icon-data";
import { RoutingProfileIcon } from "./routing-profile-icons";
import {
  type Analytics,
  type AnalyticsPercentilePoint,
  type DryRunResult,
  dryRunEvalResultKind,
  costIncompleteNote,
  fmtCapOutcome,
  fmtCount,
  fmtDryRunEvalResult,
  fmtExclusion,
  fmtMs,
  fmtRate,
  fmtUsd,
  friendlyEnumLabel,
  presentAnalyticsPercentiles,
  presentScoreComponents,
} from "./routing-profiles-format";
import { RoutingProfileDraftForm } from "./routing-profiles-form";
import {
  overviewCompatibilityRows,
  overviewCandidateRows,
  overviewLimitRows,
  overviewRequirementRows,
  overviewUnknownEvidenceRows,
  candidateCountLabel,
  profileMatchesQuery,
  profileSubtitle,
  profileTitle,
  weightPercentLabel,
} from "./routing-profile-summary";

export type RoutingProfilesView = {
  t: TFn;
  locale: Locale;
  unavailable: string;
  loadError: string;
  status: { message: string; ok: boolean } | null;
  clearStatus: () => void;
  profiles: RoutingProfileDto[];
  selected: RoutingProfileDto | null;
  draft: RoutingProfileDraft | null;
  saving: boolean;
  editing: boolean;
  query: string;
  surface: "profiles" | "evaluation" | "analytics";
  context: string;
  tools: boolean;
  image: boolean;
  structured: boolean;
  dryRunResult: DryRunResult | null;
  dryRunError: string;
  running: boolean;
  catalogSuites: LabCatalogSuite[];
  catalogError: boolean;
  analytics: Analytics | null;
  providerNames: string[];
  selectedModelOptions: ModelOption[][];
  startCreate: () => void;
  onRetry: () => void;
  selectProfile: (profile: RoutingProfileDto | null) => void;
  setQuery: (value: string) => void;
  setEditing: (value: boolean) => void;
  setDraft: Dispatch<SetStateAction<RoutingProfileDraft | null>>;
  updateCandidate: (
    index: number,
    field: "provider" | "model",
    value: string,
  ) => void;
  addCandidate: () => void;
  removeCandidate: (index: number) => void;
  moveCandidate: (from: number, to: number) => void;
  onSave: () => void;
  onSaveIcon: (iconId: string) => Promise<boolean>;
  onCancel: () => void;
  onRemove: () => void;
  setContext: (value: string) => void;
  setTools: (value: boolean) => void;
  setImage: (value: boolean) => void;
  setStructured: (value: boolean) => void;
  clearDryRun: () => void;
  runDryRun: () => void;
  onOpenEvaluation: () => void;
  onOpenAnalytics: () => void;
  onOpenOverview: () => void;
};

function RoutingKv({ label, value }: { label: string; value: string }) {
  return (
    <div className="routing-kv">
      <span className="routing-kv-label">{label}</span>
      <span className="routing-kv-value">{value}</span>
    </div>
  );
}

function RoutingProfilesNotices({ view }: { view: RoutingProfilesView }) {
  const { t, loadError, status } = view;
  return (
    <>
      {loadError ? (
        <Notice tone="err">
          {t("routing.loadFailed")}: {loadError}
        </Notice>
      ) : null}
      {status ? (
        <ToastNotice
          tone={status.ok ? "ok" : "err"}
          dismissLabel={t("common.close")}
          onDismiss={view.clearStatus}
        >
          {status.message}
        </ToastNotice>
      ) : null}
    </>
  );
}

function RoutingProfileIconPickerPanel({
  listId,
  label,
  t,
  placement,
  menuStyle,
  query,
  setQuery,
  searchRef,
  menuRef,
  loading,
  loadError,
  visibleIds,
  selectedId,
  confirm,
  saving,
  dirty,
  onPick,
  onSaveClick,
}: {
  listId: string;
  label: string;
  t: TFn;
  placement: "rail" | "detail";
  menuStyle: CSSProperties | null;
  query: string;
  setQuery: (value: string) => void;
  searchRef: RefObject<HTMLInputElement | null>;
  menuRef: RefObject<HTMLDivElement | null>;
  loading: boolean;
  loadError: boolean;
  visibleIds: string[];
  selectedId: string;
  confirm: boolean;
  saving: boolean;
  dirty: boolean;
  onPick: (id: string) => void;
  onSaveClick: () => void;
}) {
  let body: ReactNode;
  if (loading) {
    body = <p className="muted routing-rail-icon-status">{t("common.loading")}</p>;
  } else if (loadError) {
    body = <p className="muted routing-rail-icon-status">{t("routing.form.iconCatalogFailed")}</p>;
  } else if (visibleIds.length === 0) {
    body = <p className="muted routing-rail-icon-status">{t("routing.form.iconSearchEmpty")}</p>;
  } else {
    body = (
      <div className="routing-rail-icon-grid">
        {visibleIds.map((id) => (
          <button
            key={id}
            type="button"
            role="option"
            title={id}
            aria-label={id}
            aria-selected={id === selectedId}
            className={`routing-rail-icon-option${id === selectedId ? " is-active" : ""}`}
            onClick={(event) => {
              event.stopPropagation();
              onPick(id);
            }}
          >
            <RoutingProfileIcon id={id} />
          </button>
        ))}
      </div>
    );
  }
  return (
    <div
      ref={menuRef}
      className={`routing-rail-icon-picker is-portaled${placement === "detail" ? " routing-detail-icon-picker" : ""}`}
      id={listId}
      role="listbox"
      aria-label={label}
      style={menuStyle ?? { position: "fixed", visibility: "hidden" }}
      onClick={(event) => event.stopPropagation()}
      onPointerDown={(event: ReactPointerEvent<HTMLDivElement>) => {
        event.stopPropagation();
      }}
    >
      <input
        ref={searchRef}
        className="input routing-rail-icon-search"
        type="search"
        value={query}
        placeholder={t("routing.form.iconSearch")}
        aria-label={t("routing.form.iconSearch")}
        onChange={(event) => setQuery(event.target.value)}
      />
      {body}
      {confirm ? (
        <div className="routing-detail-icon-picker-footer">
          <button
            type="button"
            className="btn btn-primary btn-sm"
            disabled={saving || !dirty || loading || loadError}
            onClick={(event) => {
              event.stopPropagation();
              onSaveClick();
            }}
          >
            {saving ? t("common.saving") : t("common.save")}
          </button>
        </div>
      ) : null}
    </div>
  );
}

function useRoutingProfileIconPicker(iconId: string, confirm: boolean) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [pending, setPending] = useState(iconId);
  const [catalog, setCatalog] = useState<RoutingProfileIconCatalog | null>(null);
  const [loadError, setLoadError] = useState(false);
  const [menuStyle, setMenuStyle] = useState<CSSProperties | null>(null);
  const rootRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);
  const listId = useId();
  const current = iconId.trim() || ROUTING_PROFILE_DEFAULT_ICON;
  const selectedId = (confirm && open ? pending : current).trim() || ROUTING_PROFILE_DEFAULT_ICON;
  const visibleIds = catalog ? filterRoutingProfileIconIds(catalog.ids, query) : [];
  const loading = open && catalog === null && !loadError;
  const dirty = confirm && selectedId !== current;
  const menuWidth = 220;

  const close = () => {
    setOpen(false);
    setQuery("");
    setPending(current);
    setMenuStyle(null);
  };

  useEffect(() => {
    if (!open) return;
    const dismiss = () => {
      setOpen(false);
      setQuery("");
      setPending(current);
      setMenuStyle(null);
    };
    const onPointerDown = (event: PointerEvent) => {
      const target = event.target as Node;
      if (rootRef.current?.contains(target) || menuRef.current?.contains(target)) return;
      dismiss();
    };
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") dismiss();
    };
    document.addEventListener("pointerdown", onPointerDown);
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("pointerdown", onPointerDown);
      document.removeEventListener("keydown", onKeyDown);
    };
  }, [open, current]);

  useEffect(() => {
    if (!open || catalog) return;
    let cancelled = false;
    void ensureRoutingProfileIconCatalog()
      .then((loaded) => {
        if (cancelled) return;
        setCatalog(loaded);
        setLoadError(false);
        window.requestAnimationFrame(() => searchRef.current?.focus());
      })
      .catch(() => {
        if (!cancelled) setLoadError(true);
      });
    return () => {
      cancelled = true;
    };
  }, [open, catalog]);

  useLayoutEffect(() => {
    if (!open) return;
    const placeMenu = () => {
      const trigger = triggerRef.current;
      if (!trigger) return;
      const rect = trigger.getBoundingClientRect();
      setMenuStyle(computeSelectMenuStyle(rect, {
        align: "right",
        menuHeight: menuRef.current?.offsetHeight,
        match: {
          top: rect.top,
          bottom: rect.bottom,
          left: rect.right - menuWidth,
          right: rect.right,
          width: menuWidth,
          height: rect.height,
        },
      }));
    };
    placeMenu();
    window.addEventListener("resize", placeMenu);
    window.addEventListener("scroll", placeMenu, true);
    return () => {
      window.removeEventListener("resize", placeMenu);
      window.removeEventListener("scroll", placeMenu, true);
    };
  }, [open, menuWidth, catalog, query, loadError, visibleIds.length]);

  return {
    open,
    setOpen,
    query,
    setQuery,
    setPending,
    menuStyle,
    rootRef,
    triggerRef,
    menuRef,
    searchRef,
    listId,
    current,
    selectedId,
    visibleIds,
    loading,
    loadError,
    dirty,
    close,
  };
}

function RoutingProfileIconPicker({
  iconId,
  label,
  t,
  onChange,
  onSave,
  saving = false,
  pencil = false,
  placement = "rail",
}: {
  iconId: string;
  label: string;
  t: TFn;
  onChange?: (id: string) => void;
  onSave?: (id: string) => Promise<boolean>;
  saving?: boolean;
  pencil?: boolean;
  placement?: "rail" | "detail";
}) {
  const confirm = typeof onSave === "function";
  const pickerState = useRoutingProfileIconPicker(iconId, confirm);
  const {
    open,
    setOpen,
    query,
    setQuery,
    setPending,
    menuStyle,
    rootRef,
    triggerRef,
    menuRef,
    searchRef,
    listId,
    current,
    selectedId,
    visibleIds,
    loading,
    loadError,
    dirty,
    close,
  } = pickerState;

  const picker = open ? (
    <RoutingProfileIconPickerPanel
      listId={listId}
      label={label}
      t={t}
      placement={placement}
      menuStyle={menuStyle}
      query={query}
      setQuery={setQuery}
      searchRef={searchRef}
      menuRef={menuRef}
      loading={loading}
      loadError={loadError}
      visibleIds={visibleIds}
      selectedId={selectedId}
      confirm={confirm}
      saving={saving}
      dirty={dirty}
      onPick={(id) => {
        if (confirm) {
          setPending(id);
          return;
        }
        onChange?.(id);
        close();
      }}
      onSaveClick={() => {
        void (async () => {
          const ok = await onSave?.(selectedId);
          if (ok) close();
        })();
      }}
    />
  ) : null;

  const iconSize = placement === "detail" ? 18 : 14;
  return (
    <div
      className={`routing-rail-icon-wrap${placement === "detail" ? " routing-detail-icon-wrap" : ""}`}
      ref={rootRef}
    >
      <button
        ref={triggerRef}
        type="button"
        className={`routing-rail-icon-btn${placement === "detail" ? " routing-detail-icon-btn" : ""}`}
        aria-label={label}
        title={label}
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-controls={open ? listId : undefined}
        disabled={saving}
        onClick={(event) => {
          event.stopPropagation();
          if (open) {
            close();
            return;
          }
          setPending(current);
          setOpen(true);
        }}
        onPointerDown={(event: ReactPointerEvent<HTMLButtonElement>) => {
          event.stopPropagation();
        }}
      >
        <RoutingProfileIcon id={selectedId} width={iconSize} height={iconSize} />
        {pencil ? (
          <span className="routing-detail-icon-pencil" aria-hidden="true">
            <IconPencil />
          </span>
        ) : null}
      </button>
      {picker ? createPortal(picker, document.body) : null}
    </div>
  );
}

function RoutingProfilesList({ view }: { view: RoutingProfilesView }) {
  const {
    t,
    profiles,
    selected,
    selectProfile,
    query,
    setQuery,
    startCreate,
    editing,
    draft,
    setDraft,
  } = view;
  const creating = Boolean(editing && draft && !selected);
  const visible = profiles.filter((profile) =>
    profileMatchesQuery(profile, query),
  );
  const setIcon = (id: string) => {
    setDraft((current) => (current ? { ...current, icon: id } : current));
  };
  return (
    <section className="routing-rail" aria-label={t("routing.profilesList")}>
      <div className="routing-rail-head">
        <h3>{t("routing.profilesList")}</h3>
      </div>
      <div className="routing-rail-toolbar">
        <label className="routing-search">
          <IconSearch />
          <span className="sr-only">{t("routing.searchProfiles")}</span>
          <input
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder={t("routing.searchProfiles")}
          />
        </label>
        {profiles.length > 0 && !creating ? (
          <button
            type="button"
            className="btn btn-primary routing-rail-create"
            onClick={startCreate}
          >
            {t("routing.createProfilePlus")}
          </button>
        ) : null}
      </div>
      <div className="routing-rail-list">
        {creating ? (
          <div className="routing-rail-row is-active routing-rail-row--draft" aria-current="true">
            <span className="routing-rail-row-copy">
              <strong>
                <RoutingProfileIconPicker
                  iconId={draft?.icon || ROUTING_PROFILE_DEFAULT_ICON}
                  label={t("routing.form.changeIcon")}
                  t={t}
                  onChange={setIcon}
                />
                {t("routing.form.newProfile")}
              </strong>
            </span>
            <span className="routing-rail-draft-badge">{t("routing.form.draft")}</span>
          </div>
        ) : null}
        {visible.map((profile) => {
          const active = selected?.id === profile.id && !creating;
          const iconId = active && draft
            ? draft.icon || ROUTING_PROFILE_DEFAULT_ICON
            : profile.icon || ROUTING_PROFILE_DEFAULT_ICON;
          const canChangeIcon = active && (creating || editing);
          return (
            <button
              key={profile.id}
              type="button"
              className={`routing-rail-row${active ? " is-active" : ""}`}
              onClick={() => selectProfile(active && !editing ? null : profile)}
              aria-pressed={active}
              aria-label={`${profileTitle(profile)} — ${profileSubtitle(profile)} — ${candidateCountLabel(t, profile.candidates.length)}`}
            >
              <span className="routing-rail-row-copy">
                <strong>
                  {canChangeIcon ? (
                    <RoutingProfileIconPicker
                      iconId={iconId}
                      label={t("routing.form.changeIcon")}
                      t={t}
                      onChange={setIcon}
                    />
                  ) : (
                    <span className="routing-rail-icon-wrap" aria-hidden="true">
                      <RoutingProfileIcon id={iconId} />
                    </span>
                  )}
                  {profileTitle(profile)}
                </strong>
                <span className="muted">{profileSubtitle(profile)}</span>
              </span>
              <span className="routing-rail-row-meta">
                {candidateCountLabel(t, profile.candidates.length)}
              </span>
            </button>
          );
        })}
        {profiles.length === 0 && !creating ? (
          <div className="routing-rail-empty">
            <strong>{t("routing.emptyRailTitle")}</strong>
            <p className="muted">{t("routing.emptyRailBody")}</p>
          </div>
        ) : null}
        {profiles.length === 0 && creating ? (
          <p className="muted routing-rail-empty-hint">{t("routing.emptySavedHint")}</p>
        ) : null}
      </div>
    </section>
  );
}

function RoutingProfilesEmptyDetail({ view }: { view: RoutingProfilesView }) {
  const { t, startCreate, profiles } = view;
  const hasProfiles = profiles.length > 0;
  return (
    <div className="routing-empty-detail">
      <h3>{hasProfiles ? t("routing.selectTitle") : t("routing.emptyTitle")}</h3>
      <p className="muted routing-empty-detail-body">
        {hasProfiles ? t("routing.selectBody") : t("routing.emptyBody")}
      </p>
      <button type="button" className="btn btn-primary routing-empty-detail-action" onClick={startCreate}>
        {t("routing.createProfile")}
      </button>
      {!hasProfiles ? (
        <p className="muted routing-empty-detail-footer">{t("routing.emptyFooter")}</p>
      ) : null}
    </div>
  );
}

function RoutingProfileReadOnly({ view }: { view: RoutingProfilesView }) {
  const { t, selected, unavailable } = view;
  if (!selected) {
    return view.profiles.length === 0 ? null : (
      <p className="muted">{t("routing.empty")}</p>
    );
  }
  const requirementRows = overviewRequirementRows(selected, {
    tools: t("routing.require.tools"),
    image: t("routing.require.image"),
    structured: t("routing.require.structured"),
    minContext: t("routing.minContext"),
    reasoningEffort: t("routing.require.reasoningEffort"),
    serviceTier: t("routing.require.serviceTier"),
    minQuotaHeadroom: t("routing.require.minQuotaHeadroom"),
    localOnly: t("routing.require.localOnly"),
    remoteAllowed: t("routing.require.remoteAllowed"),
    encryptedCodexTasks: t("routing.require.encryptedCodexTasks"),
    optional: t("routing.require.optional"),
    required: t("routing.require.required"),
    off: t("routing.require.off"),
    unavailable,
  });
  const limitRows = overviewLimitRows(selected, {
    maxCost: t("routing.form.maxEstimatedCost"),
    onUnknownCost: t("routing.form.unknownCost"),
    allow: t("routing.evidence.allow"),
    exclude: t("routing.evidence.skip"),
    unavailable,
  });
  const compatibilityRows = overviewCompatibilityRows(selected, {
    gates: t("routing.compatibility.title"),
    suites: t("routing.evidence.suites"),
    minStatus: t("routing.evidence.minStatus"),
    maxAge: t("routing.evidence.maxAge"),
    unknown: t("routing.evidence.unknown"),
    degraded: t("routing.evidence.degraded"),
    allow: t("routing.evidence.allow"),
    warn: t("routing.evidence.warn"),
    skip: t("routing.evidence.skip"),
    probed: t("routing.evidence.probed"),
    verified: t("routing.evidence.verified"),
    off: t("routing.require.off"),
    unavailable,
    days: (n) => t("routing.age.days", { n: String(n) }),
  });
  const unknownEvidenceRows = overviewUnknownEvidenceRows(selected, {
    capability: t("routing.form.evidence.capability"),
    health: t("routing.form.evidence.health"),
    quota: t("routing.form.evidence.quota"),
    cost: t("routing.form.evidence.cost"),
    allow: t("routing.evidence.allow"),
    warn: t("routing.evidence.warn"),
    skip: t("routing.evidence.skip"),
    unavailable,
  });
  const candidateRows = overviewCandidateRows(selected);
  const clientRoute = selected.model?.trim() || `policy/${selected.id}`;
  const compatibilityOff =
    compatibilityRows.length === 1 && compatibilityRows[0]?.key === "compatibility";
  return (
    <article className="routing-detail-body">
      <section className="routing-section routing-section--flush">
        <RoutingKv label={t("routing.form.clientRoute")} value={clientRoute} />
        {selected.alias?.trim() ? (
          <RoutingKv label={t("routing.nickname")} value={selected.alias.trim()} />
        ) : null}
      </section>

      <section className="routing-section">
        <h4>{t("routing.candidates")}</h4>
        {candidateRows.length === 0 ? (
          <RoutingKv label={t("routing.candidates")} value={unavailable} />
        ) : (
          candidateRows.map((candidate) => (
            <div
              key={`${candidate.provider}/${candidate.model}/${candidate.index}`}
              className="routing-overview-candidate"
            >
              <span className="routing-overview-candidate-index">{candidate.index}</span>
              <span className="routing-overview-candidate-provider">{candidate.provider}</span>
              <span className="routing-overview-candidate-model">{candidate.model}</span>
            </div>
          ))
        )}
      </section>

      <section className="routing-section">
        <h4>{t("routing.section.requirements")}</h4>
        {requirementRows.length === 0 ? (
          <p className="muted routing-overview-empty">{t("routing.overview.noAdditionalRequirements")}</p>
        ) : (
          requirementRows.map((row) => (
            <RoutingKv key={row.key} label={row.label} value={row.value} />
          ))
        )}
      </section>

      <section className="routing-section">
        <h4>{t("routing.section.optimisation")}</h4>
        <RoutingKv
          label={t("routing.optimize.latency")}
          value={weightPercentLabel(selected.optimize.latency)}
        />
        <RoutingKv
          label={t("routing.optimize.health")}
          value={weightPercentLabel(selected.optimize.health)}
        />
        <RoutingKv
          label={t("routing.optimize.cost")}
          value={weightPercentLabel(selected.optimize.cost)}
        />
        <RoutingKv
          label={t("routing.optimize.quota")}
          value={weightPercentLabel(selected.optimize.quota)}
        />
        <p className="muted routing-overview-hint">
          {t("routing.optimizeCostHint")}
        </p>
      </section>

      {limitRows.length > 0 ? (
        <section className="routing-section">
          <h4>{t("routing.section.limits")}</h4>
          {limitRows.map((row) => (
            <RoutingKv key={row.key} label={row.label} value={row.value} />
          ))}
        </section>
      ) : null}

      <section className="routing-section">
        <h4>{t("routing.unknownEvidence")}</h4>
        {unknownEvidenceRows.map((row) => (
          <RoutingKv key={row.key} label={row.label} value={row.value} />
        ))}
      </section>

      <section className="routing-section">
        {compatibilityOff ? (
          compatibilityRows.map((row) => (
            <RoutingKv key={row.key} label={row.label} value={row.value} />
          ))
        ) : (
          <>
            <h4>{t("routing.compatibility.title")}</h4>
            {compatibilityRows.map((row) => (
              <RoutingKv key={row.key} label={row.label} value={row.value} />
            ))}
          </>
        )}
      </section>
    </article>
  );
}

function RoutingProfileDetailTabs({ view }: { view: RoutingProfilesView }) {
  const { t, surface, onOpenEvaluation, onOpenAnalytics, onOpenOverview, setEditing } = view;
  const tabs = [
    {
      id: "profiles" as const,
      label: t("routing.tab.overview"),
      active: surface === "profiles",
      onSelect: () => {
        setEditing(false);
        onOpenOverview();
      },
    },
    {
      id: "evaluation" as const,
      label: t("routing.tab.evaluation"),
      active: surface === "evaluation",
      onSelect: onOpenEvaluation,
    },
    {
      id: "analytics" as const,
      label: t("routing.tab.analytics"),
      active: surface === "analytics",
      onSelect: onOpenAnalytics,
    },
  ];
  return (
    <div className="routing-detail-tabs" role="tablist" aria-label={t("routing.detailTabsLabel")}>
      {tabs.map((tab) => (
        <button
          key={tab.id}
          type="button"
          role="tab"
          aria-selected={tab.active}
          className={`routing-detail-tab${tab.active ? " is-active" : ""}`}
          onClick={tab.onSelect}
        >
          {tab.label}
        </button>
      ))}
    </div>
  );
}

function RoutingProfileDetail({ view }: { view: RoutingProfilesView }) {
  const { t, selected, setEditing, surface, saving, onSaveIcon, selectProfile } = view;
  if (!selected) {
    return view.profiles.length === 0 ? null : (
      <p className="muted">{t("routing.empty")}</p>
    );
  }
  return (
    <article className="routing-detail">
      <button
        type="button"
        className="routing-detail-back"
        onClick={() => selectProfile(null)}
        aria-label={t("routing.backToOverview")}
      >
        <IconArrowLeft width={24} height={24} aria-hidden="true" />
      </button>
      <div className="routing-detail-head">
        <div className="routing-detail-head-copy">
          <div className="routing-detail-title-row">
            <h3>
              {profileTitle(selected)}
            </h3>
            <RoutingProfileIconPicker
              iconId={selected.icon || ROUTING_PROFILE_DEFAULT_ICON}
              label={t("routing.form.changeIcon")}
              t={t}
              pencil
              placement="detail"
              saving={saving}
              onSave={onSaveIcon}
            />
          </div>
          <span className="muted">{profileSubtitle(selected)}</span>
        </div>
        <div className="routing-detail-actions">
          <button
            type="button"
            className="providers-link providers-link--plain"
            onClick={() => setEditing(true)}
          >
            {t("routing.editProfile")}
          </button>
        </div>
      </div>
      <RoutingProfileDetailTabs view={view} />
      {surface === "evaluation" ? (
        <RoutingProfilesDryRun view={view} />
      ) : surface === "analytics" ? (
        <RoutingProfilesAnalytics view={view} />
      ) : (
        <RoutingProfileReadOnly view={view} />
      )}
    </article>
  );
}


function RoutingProfilesDryRun({ view }: { view: RoutingProfilesView }) {
  const {
    t,
    unavailable,
    selected,
    context,
    tools,
    image,
    structured,
    dryRunResult,
    dryRunError,
    running,
    setContext,
    setTools,
    setImage,
    setStructured,
    clearDryRun,
    runDryRun,
  } = view;
  return (
    <div className="routing-eval">
      <div className="routing-eval-form">
        <h3>{t("routing.dryRun")}</h3>
        <div className="routing-form-field routing-eval-context">
          <label className="routing-form-field-label" htmlFor="routing-context">
            {t("routing.dryRunContext")}
          </label>
          <input
            id="routing-context"
            className="input"
            type="number"
            min={1}
            value={context}
            onChange={(event) => {
              setContext(event.target.value);
              clearDryRun();
            }}
          />
        </div>
        <div className="routing-eval-flags">
          <label className="checkbox">
            <input
              type="checkbox"
              checked={tools}
              onChange={(event) => {
                setTools(event.target.checked);
                clearDryRun();
              }}
            />
            <span>{t("routing.dryRunTools")}</span>
          </label>
          <label className="checkbox">
            <input
              type="checkbox"
              checked={image}
              onChange={(event) => {
                setImage(event.target.checked);
                clearDryRun();
              }}
            />
            <span>{t("routing.dryRunImage")}</span>
          </label>
          <label className="checkbox">
            <input
              type="checkbox"
              checked={structured}
              onChange={(event) => {
                setStructured(event.target.checked);
                clearDryRun();
              }}
            />
            <span>{t("routing.dryRunStructured")}</span>
          </label>
        </div>
        <div className="routing-eval-actions">
          <button
            type="button"
            className="btn btn-primary btn-sm routing-eval-run"
            disabled={!selected || running}
            onClick={() => void runDryRun()}
          >
            {running ? t("common.loading") : t("routing.dryRunRun")}
          </button>
        </div>
      </div>
      {dryRunError ? <Notice tone="err">{dryRunError}</Notice> : null}
      {dryRunResult ? (
        <div className="routing-eval-results">
          <table className="tbl">
            <thead>
              <tr>
                <th>#</th>
                <th>{t("routing.candidate")}</th>
                <th>{t("routing.eligible")}</th>
                <th>{t("routing.score")}</th>
                <th>{t("routing.estimatedCost")}</th>
                <th>{t("routing.evalResult")}</th>
              </tr>
            </thead>
            <tbody>
              {dryRunResult.candidates.map((candidate, index) => {
                const resultKind = dryRunEvalResultKind(
                  index,
                  dryRunResult.selectedIndex,
                  candidate.eligible,
                );
                return (
                  <tr key={`${candidate.provider}/${candidate.model}`}>
                    <td>{index + 1}</td>
                    <td>
                      {candidate.provider}/{candidate.model}
                    </td>
                    <td>
                      {candidate.eligible ? t("routing.yes") : t("routing.no")}
                    </td>
                    <td>
                      {candidate.score
                        ? candidate.score.total.toFixed(3)
                        : unavailable}
                    </td>
                    <td>
                      {candidate.cost?.estimatedUsd !== undefined
                        ? fmtUsd(candidate.cost.estimatedUsd, unavailable)
                        : unavailable}
                    </td>
                    <td>{fmtDryRunEvalResult(resultKind, t)}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
          {dryRunResult.selectedIndex !== null &&
          dryRunResult.candidates[dryRunResult.selectedIndex] ? (
            <RoutingEvalSelectedSummary
              candidate={dryRunResult.candidates[dryRunResult.selectedIndex]}
              revision={dryRunResult.trace?.profile?.revision}
              t={t}
              unavailable={unavailable}
            />
          ) : null}
          <RoutingEvalExclusions candidates={dryRunResult.candidates} t={t} />
        </div>
      ) : null}
    </div>
  );
}

function RoutingEvalSelectedSummary({
  candidate,
  revision,
  t,
  unavailable,
}: {
  candidate: import("./routing-profiles-format").DryRunCandidate;
  revision: string | undefined;
  t: TFn;
  unavailable: string;
}) {
  const components = presentScoreComponents(candidate.score?.components);
  const incompleteNote = costIncompleteNote(
    candidate.cost?.incomplete,
    t("routing.costIncomplete"),
  );
  return (
    <div className="routing-eval-summary">
      <div className="routing-eval-summary-col">
        <h4>{t("routing.selectedCandidate")}</h4>
        <dl className="routing-eval-kv">
          <div className="routing-eval-kv-row">
            <dt>{t("routing.candidate")}</dt>
            <dd>
              {candidate.provider}/{candidate.model}
            </dd>
          </div>
          <div className="routing-eval-kv-row">
            <dt>{t("routing.finalScore")}</dt>
            <dd>
              {candidate.score ? candidate.score.total.toFixed(3) : unavailable}
            </dd>
          </div>
          {candidate.cost?.estimatedUsd !== undefined ? (
            <div className="routing-eval-kv-row">
              <dt>{t("routing.estimatedCost")}</dt>
              <dd>{fmtUsd(candidate.cost.estimatedUsd, unavailable)}</dd>
            </div>
          ) : null}
          {candidate.cost?.limitUsd !== undefined ? (
            <div className="routing-eval-kv-row">
              <dt>{t("routing.costLimit")}</dt>
              <dd>{fmtUsd(candidate.cost.limitUsd, unavailable)}</dd>
            </div>
          ) : null}
          {candidate.cost?.capOutcome ? (
            <div className="routing-eval-kv-row">
              <dt>{t("routing.costCap")}</dt>
              <dd>{fmtCapOutcome(candidate.cost.capOutcome, t, unavailable)}</dd>
            </div>
          ) : null}
        </dl>
        {incompleteNote ? (
          <p className="muted routing-eval-cost-incomplete-note">{incompleteNote}</p>
        ) : null}
        {revision ? (
          <p className="muted routing-eval-revision">
            {t("routing.revision")} {revision}
          </p>
        ) : null}
      </div>
      <div className="routing-eval-summary-col">
        <h4>{t("routing.scoreComponents")}</h4>
        {components.length === 0 ? (
          <p className="muted">{unavailable}</p>
        ) : (
          <dl className="routing-eval-kv">
            {components.map((row) => (
              <div key={row.key} className="routing-eval-kv-row">
                <dt>
                  {row.key === "latency"
                    ? t("routing.optimize.latency")
                    : row.key === "health"
                      ? t("routing.optimize.health")
                      : row.key === "cost"
                        ? t("routing.optimize.cost")
                        : t("routing.optimize.quota")}
                </dt>
                <dd>{row.value.toFixed(3)}</dd>
              </div>
            ))}
          </dl>
        )}
      </div>
    </div>
  );
}

function RoutingEvalExclusions({
  candidates,
  t,
}: {
  candidates: import("./routing-profiles-format").DryRunCandidate[];
  t: TFn;
}) {
  const excluded = candidates.filter((candidate) => !candidate.eligible);
  if (excluded.length === 0) return null;
  return (
    <section className="routing-eval-exclusions">
      <h4>{t("routing.exclusions")}</h4>
      <ul className="routing-eval-exclusion-list">
        {excluded.map((candidate) => (
          <li
            key={`${candidate.provider}/${candidate.model}`}
            className="routing-eval-exclusion-item"
          >
            <div className="routing-eval-exclusion-id">
              {candidate.provider}/{candidate.model}
            </div>
            {candidate.exclusions.length === 0 ? (
              <p className="muted">{t("routing.none")}</p>
            ) : (
              <ul className="routing-eval-exclusion-reasons">
                {candidate.exclusions.map((exclusion, index) => {
                  const detail = exclusion.detail?.trim();
                  return (
                    <li key={`${exclusion.code}-${index}`}>
                      <span>{fmtExclusion(exclusion.code, t)}</span>
                      {detail ? <span className="muted"> — {detail}</span> : null}
                    </li>
                  );
                })}
              </ul>
            )}
          </li>
        ))}
      </ul>
    </section>
  );
}

function RoutingPercentileChart({
  points,
  unavailable,
  formatValue,
}: {
  points: AnalyticsPercentilePoint[];
  unavailable: string;
  formatValue: (value: number) => string;
}) {
  if (points.length === 0) {
    return <p className="muted">{unavailable}</p>;
  }
  const max = Math.max(...points.map((point) => point.value));
  return (
    <div className="routing-percentile-chart">
      {points.map((point) => {
        const width = max > 0 ? Math.max(4, Math.round((point.value / max) * 100)) : 0;
        return (
          <div key={point.key} className="routing-percentile-row">
            <span className="muted">{point.key}</span>
            <div className="routing-percentile-track" aria-hidden="true">
              <div className="routing-percentile-fill" style={{ width: `${width}%` }} />
            </div>
            <span>{formatValue(point.value)}</span>
          </div>
        );
      })}
    </div>
  );
}

function RoutingProfilesAnalytics({ view }: { view: RoutingProfilesView }) {
  const { t, locale, unavailable, analytics } = view;
  if (!analytics || analytics.totalRequests <= 0) {
    return (
      <div className="routing-eval">
        <p className="muted">{t("routing.analyticsEmpty")}</p>
      </div>
    );
  }
  const durationPoints = presentAnalyticsPercentiles(analytics.durationMs);
  const firstOutputPoints = presentAnalyticsPercentiles(analytics.firstOutputMs);
  const volumeMax = Math.max(0, ...analytics.breakdown.map((row) => row.requests));
  const firstCoverage =
    analytics.firstOutputMs.coverage === null || analytics.firstOutputMs.coverage === undefined
      ? unavailable
      : fmtRate(analytics.firstOutputMs.coverage, unavailable);
  return (
    <div className="routing-eval">
      <div className="routing-analytics-kpis">
        <div className="routing-analytics-kpi">
          <span className="routing-analytics-kpi-value">{fmtCount(analytics.totalRequests, unavailable, locale)}</span>
          <span className="routing-analytics-kpi-label">{t("routing.analyticsTotal")}</span>
        </div>
        <div className="routing-analytics-kpi">
          <span className="routing-analytics-kpi-value">{fmtRate(analytics.successRate, unavailable)}</span>
          <span className="routing-analytics-kpi-label">{t("routing.analyticsSuccessRate")}</span>
        </div>
        <div className="routing-analytics-kpi">
          <span className="routing-analytics-kpi-value">{fmtRate(analytics.fallbackRate, unavailable)}</span>
          <span className="routing-analytics-kpi-label">{t("routing.analyticsFallbackRate")}</span>
        </div>
        <div className="routing-analytics-kpi">
          <span className="routing-analytics-kpi-value">{friendlyEnumLabel(analytics.confidence, unavailable)}</span>
          <span className="routing-analytics-kpi-label">{t("routing.analyticsConfidence")}</span>
        </div>
      </div>

      <section className="routing-analytics-section">
        <h4>{t("routing.analyticsLatency")}</h4>
        <div className="routing-analytics-latency-grid">
          <div>
            <p className="muted">{t("routing.analyticsDurationHint", {
              n: fmtCount(analytics.durationMs.sampleCount, unavailable, locale),
            })}</p>
            <RoutingPercentileChart
              points={durationPoints}
              unavailable={unavailable}
              formatValue={(value) => fmtMs(value, unavailable, { ms: t("routing.unit.milliseconds"), s: t("routing.unit.seconds") })}
            />
          </div>
          <div>
            <p className="muted">{t("routing.analyticsFirstOutputHint", {
              coverage: firstCoverage,
            })}</p>
            <RoutingPercentileChart
              points={firstOutputPoints}
              unavailable={unavailable}
              formatValue={(value) => fmtMs(value, unavailable, { ms: t("routing.unit.milliseconds"), s: t("routing.unit.seconds") })}
            />
          </div>
        </div>
      </section>

      <section className="routing-analytics-section">
        <h4>{t("routing.analyticsRouteVolume")}</h4>
        <p className="muted">{t("routing.analyticsRouteVolumeHint")}</p>
        {analytics.breakdown.length === 0 ? (
          <p className="muted">{unavailable}</p>
        ) : (
          <div className="routing-volume-list">
            {analytics.breakdown.map((row) => {
              const width =
                volumeMax > 0 ? Math.max(4, Math.round((row.requests / volumeMax) * 100)) : 0;
              return (
                <div key={`${row.provider}/${row.model}`} className="routing-volume-row">
                  <div className="routing-volume-meta">
                    <strong>
                      {row.provider}/{row.model}
                    </strong>
                    <div className="routing-volume-track" aria-hidden="true">
                      <div className="routing-volume-fill" style={{ width: `${width}%` }} />
                    </div>
                  </div>
                  <span className="routing-volume-count">{fmtCount(row.requests, unavailable, locale)}</span>
                </div>
              );
            })}
          </div>
        )}
      </section>

      <section className="routing-analytics-section">
        <h4>{t("routing.analyticsPerformance")}</h4>
        <table className="tbl">
          <thead>
            <tr>
              <th>#</th>
              <th>{t("routing.candidate")}</th>
              <th>{t("routing.analyticsRequests")}</th>
              <th>{t("routing.analyticsSuccessRate")}</th>
              <th>{t("routing.analyticsP50")}</th>
            </tr>
          </thead>
          <tbody>
            {analytics.breakdown.map((row, index) => (
              <tr key={`${row.provider}/${row.model}`}>
                <td>{index + 1}</td>
                <td>
                  {row.provider}/{row.model}
                </td>
                <td>{fmtCount(row.requests, unavailable, locale)}</td>
                <td>{fmtRate(row.successRate, unavailable)}</td>
                <td>{fmtMs(row.p50DurationMs, unavailable, { ms: t("routing.unit.milliseconds"), s: t("routing.unit.seconds") })}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </section>

      <section className="routing-analytics-section">
        <h4>{t("routing.analyticsDataQuality")}</h4>
        <div className="routing-analytics-quality">
          <div className="routing-analytics-quality-item">
            <strong>{firstCoverage}</strong>
            <span className="muted">{t("routing.analyticsFirstOutputCoverage")}</span>
          </div>
          <div className="routing-analytics-quality-item">
            <strong>{fmtCount(analytics.firstOutputMs.sampleCount, unavailable, locale)}</strong>
            <span className="muted">{t("routing.analyticsFirstOutputSamples")}</span>
          </div>
          <div className="routing-analytics-quality-item">
            <strong>{fmtCount(analytics.durationMs.sampleCount, unavailable, locale)}</strong>
            <span className="muted">{t("routing.analyticsDurationSamples")}</span>
          </div>
          <div className="routing-analytics-quality-item">
            <strong>{fmtCount(analytics.cooldownTriggeringFailures, unavailable, locale)}</strong>
            <span className="muted">{t("routing.analyticsCooldown")}</span>
          </div>
          <div className="routing-analytics-quality-item">
            <strong>
              {analytics.historyTruncated
                ? t("routing.analyticsHistoryTruncated")
                : t("routing.analyticsHistoryComplete")}
            </strong>
            <span className="muted">
              {analytics.historyTruncated
                ? t("routing.analyticsHistoryTruncatedHint")
                : t("routing.analyticsHistoryCompleteHint")}
            </span>
          </div>
        </div>
      </section>
    </div>
  );
}

export function RoutingProfilesPage({ view }: { view: RoutingProfilesView }) {
  const creating = view.editing || (view.draft && !view.selected);
  return (
    <div className="routing-board" data-page="routing">
      <RoutingProfilesNotices view={view} />
      <div className="routing-split">
        <RoutingProfilesList view={view} />
        {creating ? (
          <RoutingProfileDraftForm view={view} />
        ) : view.selected ? (
          <RoutingProfileDetail view={view} />
        ) : (
          <RoutingProfilesEmptyDetail view={view} />
        )}
      </div>
    </div>
  );
}
