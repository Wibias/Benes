/**
 * Combos rail: title, create action, search, and the combos it lists.
 *
 * The rail decides what a row *says* through `combo-workspace-utils` (which
 * combo is flagged, and how a member count reads) and `combo-workspace-data`
 * (how a combo names itself). What stays here is presentation: markup, classes,
 * and the translation calls.
 */

import { buildComboAttention, comboDisplaySecondaryIdentity, comboDisplayTitle, type ComboItem } from "../combo-workspace-data";
import { IconPlus, IconSearch } from "../icons";
import { useT, type TFn } from "../i18n/shared";
import { comboRailNote, comboTargetCountCopy, type ComboRailNote } from "./combo-workspace-utils";

interface RailProps {
  combos: ComboItem[];
  query: string;
  onQueryChange: (query: string) => void;
  filtered: ComboItem[];
  activeId: string | null;
  creating?: boolean;
  cataloguedComboIds?: ReadonlySet<string>;
  onSelect: (id: string) => void;
  onAdd: () => void;
}

/** Singular for a lone member, counted otherwise. */
function targetCountText(t: TFn, targets: number): string {
  const copy = comboTargetCountCopy(targets);
  return copy.key === "cws.targetCountOne"
    ? t(copy.key)
    : t(copy.key, { count: copy.count });
}

export function ComboWorkspaceRail({
  combos,
  query,
  onQueryChange,
  filtered,
  activeId,
  creating,
  cataloguedComboIds,
  onSelect,
  onAdd,
}: RailProps) {
  const t = useT();
  const attention = buildComboAttention(combos, { cataloguedComboIds });
  const reasonById = new Map(attention.map((row) => [row.id, row.reason]));

  return (
    <aside className="combos-workspace-rail" aria-label={t("cws.railAria")}>
      <div className="combos-workspace-rail-header">
        <div>
          <div className="combos-workspace-rail-title">{t("nav.combos")}</div>
          {combos.length > 0 ? (
            <div className="combos-workspace-rail-count">{combos.length}</div>
          ) : null}
        </div>
        <button
          type="button"
          className="btn btn-primary btn-sm"
          onClick={onAdd}
          aria-label={t("cws.create")}
        >
          <IconPlus width={14} height={14} /> {t("cws.create")}
        </button>
      </div>
      <div className="cwi-search-row">
        <div className="cwi-search-wrap">
          <IconSearch className="cwi-search-icon" aria-hidden="true" />
          <input
            className="input cwi-search-input"
            value={query}
            onChange={(event) => onQueryChange(event.target.value)}
            placeholder={t("cws.searchPlaceholder")}
            aria-label={t("cws.searchPlaceholder")}
          />
        </div>
      </div>
      <div className="combos-workspace-rail-list">
        <RailList
          combos={combos}
          filtered={filtered}
          activeId={activeId}
          creating={creating}
          reasonById={reasonById}
          onSelect={onSelect}
        />
      </div>
    </aside>
  );
}

interface RailListProps {
  combos: ComboItem[];
  filtered: ComboItem[];
  activeId: string | null;
  creating?: boolean;
  reasonById: ReadonlyMap<string, "few-targets" | "empty-targets" | "catalog-omitted">;
  onSelect: (id: string) => void;
}

function RailList({
  combos,
  filtered,
  activeId,
  creating,
  reasonById,
  onSelect,
}: RailListProps) {
  const t = useT();
  if (combos.length === 0 && !creating) {
    return (
      <div className="combos-workspace-rail-empty">
        <strong>{t("cws.emptyRailTitle")}</strong>
      </div>
    );
  }
  if (filtered.length === 0 && combos.length > 0) {
    return <p className="muted combos-workspace-rail-empty-hint">{t("cws.noSearchResults")}</p>;
  }
  return (
    <>
      {creating ? (
        <div className="combos-workspace-rail-row combos-workspace-rail-row--selected combos-workspace-rail-row--draft" aria-current="true">
          <span className="combos-workspace-rail-copy">
            <strong>{t("cws.createTitle")}</strong>
            <span className="muted">{t("cws.draftBadge")}</span>
          </span>
        </div>
      ) : null}
      {filtered.map((item) => (
        <RailRow
          key={item.id}
          item={item}
          selected={activeId === item.id && !creating}
          note={comboRailNote(item, reasonById.get(item.id))}
          onSelect={onSelect}
        />
      ))}
    </>
  );
}

interface RailRowProps {
  item: ComboItem;
  selected: boolean;
  note: ComboRailNote | null;
  onSelect: (id: string) => void;
}

function RailRow({ item, selected, note, onSelect }: RailRowProps) {
  const t = useT();
  const meta = targetCountText(t, item.targets.length);
  // A row shows the client-facing id when it differs; a note replaces the
  // member count there, and both always keep the count on the right.
  const secondary = comboDisplaySecondaryIdentity(item);
  const subtitle = secondary ?? (note === null ? meta : null);
  return (
    <button
      type="button"
      className={`combos-workspace-rail-row${selected ? " combos-workspace-rail-row--selected" : ""}`}
      onClick={() => onSelect(item.id)}
      aria-current={selected ? "true" : undefined}
    >
      <span className="combos-workspace-rail-copy">
        <strong className="combos-workspace-rail-name">{comboDisplayTitle(item)}</strong>
        {subtitle ? <span className="muted combos-workspace-rail-sub">{subtitle}</span> : null}
        {note ? <span className="muted combos-workspace-rail-note">{t(note)}</span> : null}
      </span>
      <span className="combos-workspace-rail-meta">{meta}</span>
    </button>
  );
}