/**
 * Failover target editor for the Combos create/edit pane.
 *
 * Combos are failover only: a row is a provider/model pair in a fixed order.
 * `combo-workspace-utils` resolves rows, picker lists and list edits, so this
 * file owns interaction state and markup only: a grip plus keyboard-focusable
 * move buttons for ordering, one select shape reused for provider and model.
 * The legacy round-robin weight field left with that strategy.
 */

import { useState, type ReactNode } from "react";
import { newComboTarget, type ComboTarget } from "../combo-workspace-data";
import { IconArrowDown, IconArrowUp, IconGrip, IconPlus, IconTrash } from "../icons";
import { useT, type TFn } from "../i18n/shared";
import { formatProviderDisplayName } from "../provider-icons";
import type { ModelOption, ProviderOption } from "./combo-workspace-types";
import {
  comboFirstModelId,
  comboMoveTarget,
  comboPatchTarget,
  comboRemoveTarget,
  comboTargetRowClass,
  comboTargetRows,
  type ComboTargetRow,
} from "./combo-workspace-utils";

type DragPosition = { from: number; over: number };

interface TargetEditorProps {
  targets: ComboTarget[];
  providers: ProviderOption[];
  models: ModelOption[];
  onChange: (next: ComboTarget[]) => void;
}

interface TargetSelectProps {
  label: string;
  placeholder: string;
  value: string;
  disabled?: boolean;
  onChange: (value: string) => void;
  children: ReactNode;
}

/** Both target selects share one shape; only their option lists differ. */
function TargetSelect({
  label,
  placeholder,
  value,
  disabled,
  onChange,
  children,
}: TargetSelectProps) {
  return (
    <select
      className="input"
      value={value}
      disabled={disabled}
      aria-label={label}
      onChange={(event) => onChange(event.target.value)}
    >
      <option value="">{placeholder}</option>
      {children}
    </select>
  );
}

function providerChoiceLabel(choice: ProviderOption, t: TFn): string {
  const display = formatProviderDisplayName(choice.name, t);
  return choice.disabled ? t("cws.target.disabled", { name: display }) : display;
}
export function TargetEditor({ targets, providers, models, onChange }: TargetEditorProps) {
  const t = useT();
  const [drag, setDrag] = useState<DragPosition | null>(null);
  const rows = comboTargetRows(targets, providers, models);
  const labels = {
    drag: t("cws.target.drag"),
    moveUp: t("cws.target.moveUp"),
    moveDown: t("cws.target.moveDown"),
    provider: t("cws.target.provider"),
    model: t("cws.target.model"),
    remove: t("common.remove"),
    add: t("cws.target.add"),
  };

  const patch = (row: ComboTargetRow, next: Partial<ComboTarget>) => {
    const patched = comboPatchTarget(targets, row.index, next);
    if (patched !== targets) onChange(patched);
  };

  const move = (from: number, to: number) => {
    const reordered = comboMoveTarget(targets, from, to);
    if (reordered !== targets) onChange(reordered);
  };

  /** Finish a drag: reorder when a row was crossed, then clear the drag state. */
  const settle = (droppedOn?: number) => {
    if (droppedOn !== undefined && drag) move(drag.from, droppedOn);
    setDrag(null);
  };

  const trackDragOver = (row: ComboTargetRow) => {
    if (!drag) return;
    if (drag.over !== row.index) setDrag({ from: drag.from, over: row.index });
  };

  return (
    <div className="cwi-target-list">
      {rows.map((row) => {
        const dragging = drag?.from === row.index;
        const dropping = drag !== null && drag.from !== row.index && drag.over === row.index;
        return (
          <div
            key={row.key}
            className={comboTargetRowClass(dragging, dropping)}
            onDragOver={(event) => {
              if (!drag) return;
              event.preventDefault();
              event.dataTransfer.dropEffect = "move";
              trackDragOver(row);
            }}
            onDrop={(event) => {
              event.preventDefault();
              settle(row.index);
            }}
            onDragEnd={() => setDrag(null)}
          >
            <button
              type="button"
              className="cwi-target-grip cwi-target-grip--numbered"
              draggable
              aria-label={labels.drag}
              title={labels.drag}
              onDragStart={(event) => {
                setDrag({ from: row.index, over: row.index });
                event.dataTransfer.effectAllowed = "move";
                event.dataTransfer.setData("text/plain", String(row.index));
              }}
            >
              <span className="cwi-target-index" aria-hidden="true">{row.index + 1}</span>
              <IconGrip width={12} height={12} aria-hidden="true" />
            </button>
            <div className="cwi-target-reorder">
              <button
                type="button"
                className="cwi-target-move"
                disabled={!row.canMoveUp}
                aria-label={labels.moveUp}
                onClick={() => move(row.index, row.index - 1)}
              >
                <IconArrowUp width={14} height={14} aria-hidden="true" />
              </button>
              <button
                type="button"
                className="cwi-target-move"
                disabled={!row.canMoveDown}
                aria-label={labels.moveDown}
                onClick={() => move(row.index, row.index + 1)}
              >
                <IconArrowDown width={14} height={14} aria-hidden="true" />
              </button>
            </div>
            <TargetSelect
              label={labels.provider}
              placeholder={t("cws.target.pickProvider")}
              value={row.target.provider}
              onChange={(provider) => patch(row, {
                provider,
                model: comboFirstModelId(models, provider, providers),
              })}
            >
              {row.providerChoices.map((choice) => (
                <option key={choice.name} value={choice.name}>{providerChoiceLabel(choice, t)}</option>
              ))}
            </TargetSelect>
            <TargetSelect
              label={labels.model}
              placeholder={t(row.modelPlaceholder)}
              value={row.target.model}
              disabled={!row.target.provider}
              onChange={(model) => patch(row, { model })}
            >
              {row.modelChoices.map((id) => (
                <option key={id} value={id}>{id}</option>
              ))}
            </TargetSelect>
            <div className="cwi-target-actions">
              <button
                type="button"
                className="btn btn-ghost btn-sm"
                disabled={!row.canRemove}
                aria-label={labels.remove}
                onClick={() => {
                  const remaining = comboRemoveTarget(targets, row.index);
                  if (remaining !== targets) onChange(remaining);
                }}
              >
                <IconTrash width={14} height={14} />
              </button>
            </div>
          </div>
        );
      })}
      <button
        type="button"
        className="cwi-add-target"
        onClick={() => onChange([...targets, newComboTarget()])}
      >
        <IconPlus width={14} height={14} aria-hidden="true" /> {labels.add}
      </button>
    </div>
  );
}

