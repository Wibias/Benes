/**
 * Combo create/edit pane.
 *
 * One module owns the whole editor: the draft being edited, whether that draft
 * still matches the stored combo, the save it issues, and the fields the user
 * types into. The field markup previously lived in a second module whose only
 * caller was this pane, so every field edit crossed a file boundary without the
 * fields gaining a contract of their own — they are the editor's own controls.
 *
 * The stored combo is the baseline. A clean editor adopts it whenever the list
 * refreshes; a dirty editor keeps the draft, the message and the dirty flag, so
 * a background reload cannot discard in-progress edits (the Routing Profiles
 * contract).
 */

import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import {
  comboClientModelId,
  comboPublicModelId,
  draftEquals,
  shouldAdoptComboBaselineOnSync,
  updateComboAliasDraft,
  validateComboDraft,
  type ComboItem,
} from "../combo-workspace-data";
import { IconArrowLeft, IconInfo, IconTrash } from "../icons";
import { useT, type TFn } from "../i18n/shared";
import { ToastNotice, Tooltip } from "../ui";
import { TargetEditor } from "./combo-workspace-controls";
import type { ComboSaveHandler, ModelOption, ProviderOption } from "./combo-workspace-types";
import {
  comboEditorSyncKey,
  comboEditorTitle,
  comboSavePayload,
  legacyTargetNote,
} from "./combo-workspace-utils";

type EditorMessage = { ok: boolean; text: string };

/** Draft state machine: edit, diff against the baseline, then save. */
function useComboDraft(props: DetailPanelProps) {
  const {
    baseline,
    isCreate = false,
    otherIds,
    otherAliases,
    providerMap,
    onSave,
    onSaved,
    onDirtyChange,
  } = props;
  const t = useT();
  const [draft, setDraft] = useState<ComboItem>(baseline);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState<EditorMessage | null>(null);
  const [dirty, setDirty] = useState(false);
  const dirtyRef = useRef(false);
  const syncKey = comboEditorSyncKey(baseline);

  const markDirty = useCallback((next: boolean) => {
    dirtyRef.current = next;
    setDirty(next);
    onDirtyChange(next);
  }, [onDirtyChange]);

  const updateDraft = useCallback((updater: (prev: ComboItem) => ComboItem) => {
    const next = updater(draft);
    setDraft(next);
    markDirty(!draftEquals(next, baseline));
  }, [baseline, draft, markDirty]);

  useEffect(() => {
    const timer = window.setTimeout(() => {
      // Dirty editor: keep the draft, the message and the dirty flag.
      if (!shouldAdoptComboBaselineOnSync(dirtyRef.current)) return;
      setDraft(baseline);
      setMessage(null);
    }, 0);
    return () => window.clearTimeout(timer);
  }, [syncKey, baseline]);

  async function commit(): Promise<void> {
    const code = validateComboDraft(draft, {
      existingIds: otherIds,
      existingAliases: otherAliases,
      isCreate,
      providers: providerMap,
    });
    if (code !== null) {
      setMessage({ ok: false, text: t(`cws.err.${code}`) });
      return;
    }
    setBusy(true);
    const item = comboSavePayload(draft, isCreate);
    const renameFrom = !isCreate && item.id !== baseline.id ? baseline.id : undefined;
    try {
      const result = await onSave(item, isCreate, renameFrom);
      if (!result.ok) {
        setMessage({ ok: false, text: result.error || t("cws.saveFailed") });
        return;
      }
      setMessage({
        ok: true,
        text: isCreate ? t("cws.created", { model: item.model }) : t("cws.saved"),
      });
      onSaved(item);
    } finally {
      setBusy(false);
    }
  }

  return {
    draft,
    busy,
    message,
    dirty,
    updateDraft,
    commit,
    clearMessage: () => setMessage(null),
  };
}

/** The `?` beside a label that explains what the field does. */
function FieldTip({ content }: { content: string }) {
  return (
    <Tooltip content={content} side="top" maxWidth={320}>
      <span className="cwi-field-tip" aria-label={content}>
        <IconInfo width={13} height={13} aria-hidden="true" />
      </span>
    </Tooltip>
  );
}

/** One editor field, described as data so a single renderer owns the markup. */
type EditorField =
  | {
      readonly kind: "text";
      readonly fieldId: string;
      readonly label: string;
      readonly hint: string;
      readonly tip?: string;
      readonly value: string;
      readonly className: string;
      readonly placeholder?: string;
      readonly maxLength?: number;
      readonly disabled: boolean;
      readonly onValue: (value: string) => void;
    }
  | {
      readonly kind: "toggle";
      readonly fieldId: string;
      readonly label: string;
      readonly hint: string;
      readonly tip: string;
      readonly checked: boolean;
      readonly disabled: boolean;
      readonly onChecked: (checked: boolean) => void;
    };

/** Label row shared by every field kind: label first, then its explainer. */
function fieldLabelRow(fieldId: string, label: ReactNode, tip?: string) {
  return (
    <div className="cwi-field-label-row">
      <label htmlFor={fieldId}>{label}</label>
      {tip === undefined ? null : <FieldTip content={tip} />}
    </div>
  );
}

/** The only place an editor field becomes markup. */
function EditorFieldView({ field }: { field: EditorField }) {
  if (field.kind === "toggle") {
    return (
      <div className="cwi-field">
        {fieldLabelRow(field.fieldId, (
          <>
            <input
              id={field.fieldId}
              type="checkbox"
              checked={field.checked}
              disabled={field.disabled}
              onChange={(event) => field.onChecked(event.target.checked)}
            /> {field.label}
          </>
        ), field.tip)}
        <p className="muted cwi-field-hint">{field.hint}</p>
      </div>
    );
  }
  return (
    <div className="cwi-field">
      {fieldLabelRow(field.fieldId, field.label, field.tip)}
      <input
        id={field.fieldId}
        className={field.className}
        value={field.value}
        placeholder={field.placeholder}
        maxLength={field.maxLength}
        disabled={field.disabled}
        onChange={(event) => field.onValue(event.target.value)}
      />
      <p className="muted cwi-field-hint">{field.hint}</p>
    </div>
  );
}

/** One titled form section; both editor sections render through it. */
function ComboFormSection({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="combos-overview-section">
      <h3 className="combos-overview-section-title">{title}</h3>
      {children}
    </section>
  );
}

interface IdentityFieldsProps {
  draft: ComboItem;
  isCreate: boolean;
  busy: boolean;
  clientModel: string;
  updateDraft: (updater: (prev: ComboItem) => ComboItem) => void;
  t: TFn;
}

/**
 * The identity fields, in the order the editor shows them. The display-name
 * field only exists once the combo carries a native takeover or a stored label,
 * so it is appended rather than rendered conditionally at the call site.
 */
function identityFieldSpecs(props: IdentityFieldsProps): EditorField[] {
  const { draft, isCreate, busy, clientModel, updateDraft, t } = props;
  const fields: EditorField[] = [
    {
      kind: "text",
      fieldId: "cwi-edit-id",
      label: t("cws.field.id"),
      hint: isCreate
        ? t("cws.field.idInternalHint")
        : t("cws.field.idHintEdit", { model: clientModel || "combo/<id>" }),
      tip: isCreate ? t("cws.field.idInternalTip") : undefined,
      value: draft.id,
      className: "input mono",
      disabled: busy,
      onValue: (id) => updateDraft((prev) => ({
        ...prev,
        id,
        model: comboPublicModelId(id, prev.alias),
      })),
    },
    {
      kind: "text",
      fieldId: "cwi-edit-alias",
      label: t("cws.field.alias"),
      hint: t("cws.field.aliasHint"),
      tip: t("cws.field.aliasTip"),
      value: draft.alias ?? "",
      className: draft.nativeAlias ? "input mono" : "input",
      placeholder: t("cws.field.aliasPlaceholder"),
      disabled: busy,
      onValue: (alias) => updateDraft((prev) => updateComboAliasDraft(prev, alias)),
    },
    {
      kind: "toggle",
      fieldId: "cwi-edit-native-alias",
      label: t("cws.field.nativeAlias"),
      hint: t("cws.field.nativeAliasHint"),
      tip: t("cws.field.nativeAliasTip"),
      checked: draft.nativeAlias,
      disabled: busy,
      onChecked: (nativeAlias) => updateDraft((prev) => ({ ...prev, nativeAlias })),
    },
  ];
  if (draft.nativeAlias || draft.displayName?.trim()) {
    fields.push({
      kind: "text",
      fieldId: "cwi-edit-display-name",
      label: t("cws.field.displayName"),
      hint: t("cws.field.displayNameHint"),
      value: draft.displayName ?? "",
      className: "input",
      maxLength: 128,
      disabled: busy,
      onValue: (label) => updateDraft((prev) => ({ ...prev, displayName: label || null })),
    });
  }
  return fields;
}

/** Identity section: the id clients address, the nickname, and the takeover. */
function IdentityFields(props: IdentityFieldsProps) {
  const { t, isCreate, clientModel } = props;
  return (
    <ComboFormSection title={t("cws.section.identity")}>
      {identityFieldSpecs(props).map((field) => (
        <EditorFieldView key={field.fieldId} field={field} />
      ))}
      {isCreate && clientModel ? (
        <p className="muted cwi-field-hint">{t("routing.clientModelHint", { model: clientModel })}</p>
      ) : null}
    </ComboFormSection>
  );
}

interface TargetsFieldsProps {
  draft: ComboItem;
  isCreate: boolean;
  providers: ProviderOption[];
  models: ModelOption[];
  updateDraft: (updater: (prev: ComboItem) => ComboItem) => void;
  t: TFn;
}

/** Failover targets, in the order the walker tries them. */
function TargetsFields({
  draft,
  isCreate,
  providers,
  models,
  updateDraft,
  t,
}: TargetsFieldsProps) {
  const note = isCreate ? null : legacyTargetNote(draft, t);
  return (
    <ComboFormSection title={t("cws.targets")}>
      <p className="muted cwi-field-hint">{t("cws.targets.orderHint")}</p>
      {note === null ? null : <p className="muted cwi-field-hint" role="note">{note}</p>}
      <TargetEditor
        targets={draft.targets}
        providers={providers}
        models={models}
        onChange={(targets) => updateDraft((prev) => ({ ...prev, targets }))}
      />
    </ComboFormSection>
  );
}

interface ComboEditorFieldsProps {
  draft: ComboItem;
  isCreate: boolean;
  busy: boolean;
  providers: ProviderOption[];
  models: ModelOption[];
  updateDraft: (updater: (prev: ComboItem) => ComboItem) => void;
}

/** Both editor sections; the client-facing id is resolved once per render. */
function ComboEditorFields({
  draft,
  isCreate,
  busy,
  providers,
  models,
  updateDraft,
}: ComboEditorFieldsProps) {
  const t = useT();
  const id = draft.id.trim();
  const clientModel = id.length > 0 ? comboClientModelId(id, draft.alias, draft.nativeAlias) : "";
  return (
    <div className="cwi-form-grid">
      <IdentityFields
        draft={draft}
        isCreate={isCreate}
        busy={busy}
        clientModel={clientModel}
        updateDraft={updateDraft}
        t={t}
      />
      <TargetsFields
        draft={draft}
        isCreate={isCreate}
        providers={providers}
        models={models}
        updateDraft={updateDraft}
        t={t}
      />
    </div>
  );
}

interface DetailPanelProps {
  baseline: ComboItem;
  isCreate?: boolean;
  otherIds: string[];
  otherAliases: string[];
  providerMap: Readonly<Record<string, { disabled?: boolean }>>;
  providers: ProviderOption[];
  models: ModelOption[];
  onBack?: () => void;
  onCancel?: () => void;
  onSaved: (item: ComboItem) => void;
  onRequestRemove?: () => void;
  onSave: ComboSaveHandler;
  onDirtyChange: (dirty: boolean) => void;
}

export function DetailPanel(props: DetailPanelProps) {
  const {
    baseline,
    isCreate = false,
    providers,
    models,
    onBack,
    onCancel,
    onRequestRemove,
  } = props;
  const t = useT();
  const editor = useComboDraft({ ...props, isCreate });
  const saveDisabled = (!isCreate && !editor.dirty) || editor.busy;

  return (
    <div className="combos-workspace-detail">
      {onBack && !isCreate ? (
        <button
          type="button"
          className="routing-detail-back"
          onClick={onBack}
          aria-label={t("cws.backToAll")}
        >
          <IconArrowLeft width={24} height={24} aria-hidden="true" />
        </button>
      ) : null}
      <div className="combos-workspace-detail-head">
        <h2 className="combos-workspace-detail-title">
          {comboEditorTitle(isCreate, editor.draft, baseline, t("cws.createTitle"))}
        </h2>
        <div className="combos-workspace-detail-actions">
          {onCancel ? (
            <button
              type="button"
              className="btn btn-ghost btn-sm"
              onClick={onCancel}
              disabled={editor.busy}
            >
              {t("common.cancel")}
            </button>
          ) : null}
          <button
            id={isCreate ? "cwi-edit-create" : "cwi-edit-save"}
            type="button"
            className="btn btn-primary btn-sm"
            disabled={saveDisabled}
            onClick={() => { void editor.commit(); }}
          >
            {editor.busy ? t("common.saving") : t(isCreate ? "cws.create" : "common.save")}
          </button>
        </div>
      </div>

      {editor.message ? (
        <ToastNotice
          tone={editor.message.ok ? "ok" : "err"}
          dismissLabel={t("common.close")}
          onDismiss={editor.clearMessage}
        >
          {editor.message.text}
        </ToastNotice>
      ) : null}

      <ComboEditorFields
        draft={editor.draft}
        isCreate={isCreate}
        busy={editor.busy}
        providers={providers}
        models={models}
        updateDraft={editor.updateDraft}
      />

      {!isCreate && onRequestRemove ? (
        <div className="combos-workspace-danger-zone">
          <button
            type="button"
            className="combos-workspace-remove-quiet"
            onClick={onRequestRemove}
            disabled={editor.busy}
          >
            <IconTrash width={14} height={14} aria-hidden="true" /> {t("cws.removeCombo")}
          </button>
        </div>
      ) : null}
    </div>
  );
}
