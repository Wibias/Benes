import { type CSSProperties, type Dispatch, type SetStateAction } from "react";
import { IconChevron, IconSearch } from "../icons";
import type { TFn } from "../i18n/shared";
import { formatProviderDisplayName } from "../provider-icons";
import { EmptyState } from "../ui";
import {
  dashboardModelGroupExpanded,
  toggleExpandedProvider,
} from "./dashboard-model-views";
import type { ModelInfo } from "./dashboard-shared";

type HeadingView = {
  label: string;
  count: number;
  busy: boolean;
};

type SearchView = {
  label: string;
  query: string;
};

type ModelGroupView = {
  provider: string;
  title: string;
  count: number;
  open: boolean;
  chips: Array<{ key: string; id: string }>;
};

type ModelsBoardView =
  | { kind: "empty-catalog"; heading: HeadingView; title: string }
  | { kind: "no-hits"; heading: HeadingView; search: SearchView; miss: string }
  | { kind: "groups"; heading: HeadingView; search: SearchView; groups: ModelGroupView[] };

type ModelsSectionProps = {
  t: TFn;
  models: ModelInfo[];
  modelsLoading: boolean;
  modelQuery: string;
  setModelQuery: (value: string) => void;
  filteredGroups: Array<[string, ModelInfo[]]>;
  expandedProviders: Set<string>;
  setExpandedProviders: Dispatch<SetStateAction<Set<string>>>;
};

const SPIN_STYLE: CSSProperties = { marginLeft: 4 };
const NO_HITS_STYLE: CSSProperties = { margin: "4px 0" };

function toModelGroupView(
  t: TFn,
  query: string,
  expandedProviders: ReadonlySet<string>,
  provider: string,
  models: ModelInfo[],
): ModelGroupView {
  return {
    provider,
    title: formatProviderDisplayName(provider, t),
    count: models.length,
    open: dashboardModelGroupExpanded(query, expandedProviders, provider),
    chips: models.map(model => ({ key: `${model.provider}/${model.id}`, id: model.id })),
  };
}

function toModelsBoardView(
  t: TFn,
  models: ModelInfo[],
  modelsLoading: boolean,
  query: string,
  expandedProviders: ReadonlySet<string>,
  filteredGroups: Array<[string, ModelInfo[]]>,
): ModelsBoardView {
  const heading: HeadingView = {
    label: t("dash.availableModels"),
    count: models.length,
    busy: modelsLoading,
  };
  if (models.length === 0 && !modelsLoading) {
    return { kind: "empty-catalog", heading, title: t("dash.noModels") };
  }
  const search: SearchView = { label: t("models.search"), query };
  if (filteredGroups.length === 0) {
    return { kind: "no-hits", heading, search, miss: t("dash.modelsNoResults") };
  }
  return {
    kind: "groups",
    heading,
    search,
    groups: filteredGroups.map(([provider, rows]) => (
      toModelGroupView(t, query, expandedProviders, provider, rows)
    )),
  };
}

export function DashboardModelsSection(props: ModelsSectionProps) {
  const view = toModelsBoardView(
    props.t,
    props.models,
    props.modelsLoading,
    props.modelQuery,
    props.expandedProviders,
    props.filteredGroups,
  );
  return (
    <>
      <SectionCountHeading heading={view.heading} />
      {view.kind === "empty-catalog" ? (
        <EmptyState title={view.title} />
      ) : (
        <ModelsCatalogBody
          view={view}
          onQuery={props.setModelQuery}
          onToggle={provider => {
            props.setExpandedProviders(current => toggleExpandedProvider(current, provider));
          }}
        />
      )}
    </>
  );
}

function ModelsCatalogBody({
  view,
  onQuery,
  onToggle,
}: {
  view: Exclude<ModelsBoardView, { kind: "empty-catalog" }>;
  onQuery: (value: string) => void;
  onToggle: (provider: string) => void;
}) {
  return (
    <>
      <ModelsSearchField search={view.search} onQuery={onQuery} />
      {view.kind === "no-hits" ? (
        <p className="muted text-control" style={NO_HITS_STYLE}>{view.miss}</p>
      ) : (
        <div className="dash-model-acc">
          {view.groups.map(group => (
            <ModelProviderGroup key={group.provider} view={group} onToggle={() => onToggle(group.provider)} />
          ))}
        </div>
      )}
    </>
  );
}

function SectionCountHeading({ heading }: { heading: HeadingView }) {
  return (
    <div className="h-section">
      {heading.label} <span className="count">{heading.count}</span>
      {heading.busy ? <span className="spin" style={SPIN_STYLE} /> : null}
    </div>
  );
}

function ModelsSearchField({
  search,
  onQuery,
}: {
  search: SearchView;
  onQuery: (value: string) => void;
}) {
  return (
    <div className="pws-search-wrap">
      <IconSearch className="pws-search-icon" width={14} height={14} aria-hidden="true" />
      <input
        type="search"
        className="input pws-search-input"
        placeholder={search.label}
        value={search.query}
        onChange={event => onQuery(event.target.value)}
        aria-label={search.label}
      />
    </div>
  );
}

function chevronOpenStyle(open: boolean): CSSProperties {
  return {
    transform: open ? "rotate(90deg)" : "none",
    transition: "transform .12s",
    color: "var(--muted)",
  };
}

function ModelProviderGroup({
  view,
  onToggle,
}: {
  view: ModelGroupView;
  onToggle: () => void;
}) {
  return (
    <div className="dash-model-group">
      <button type="button" className="dash-model-head" onClick={onToggle} aria-expanded={view.open}>
        <IconChevron width={12} height={12} style={chevronOpenStyle(view.open)} aria-hidden="true" />
        <span className="font-semibold">{view.title}</span>
        <span className="count">{view.count}</span>
      </button>
      {view.open ? <ModelIdChipList chips={view.chips} /> : null}
    </div>
  );
}

function ModelIdChipList({ chips }: { chips: ModelGroupView["chips"] }) {
  return (
    <div className="dash-model-chips">
      {chips.map(chip => (
        <code key={chip.key} className="dash-model-chip">{chip.id}</code>
      ))}
    </div>
  );
}
