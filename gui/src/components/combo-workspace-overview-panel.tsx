/**
 * Right-hand pane for the Combos workspace while no combo is open.
 *
 * The current product keeps this quiet: a prompt to pick from the rail, or the
 * first-run empty state whose primary action starts creation. Copy comes from
 * `combosOverviewCopy`, so both states share one markup path and no KPI or
 * attention board can creep back in.
 */

import { useT } from "../i18n/shared";
import { combosOverviewCopy } from "./combo-workspace-utils";

interface OverviewPanelProps {
  hasCombos: boolean;
  onAdd: () => void;
}

export function OverviewPanel({ hasCombos, onAdd }: OverviewPanelProps) {
  const t = useT();
  const copy = combosOverviewCopy(hasCombos);
  const title = t(copy.title);
  const body = t(copy.body);
  const footer = copy.footer === null ? null : t(copy.footer);

  return (
    <div className="combos-workspace-quiet">
      <h2 className="combos-workspace-quiet-title">{title}</h2>
      <p className="muted combos-workspace-quiet-body">{body}</p>
      {footer === null ? null : (
        <>
          <button type="button" className="btn btn-primary" onClick={onAdd}>{t("cws.create")}</button>
          <p className="muted combos-workspace-quiet-footer">{footer}</p>
        </>
      )}
    </div>
  );
}
