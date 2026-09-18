import { useCallback, useId } from "react";
import { useT } from "../../i18n/shared";

export type ProviderJsonSession = {
  visible: boolean;
  text: string;
  dirty: boolean;
  setText: (value: string) => void;
  save: () => Promise<boolean>;
  close: () => void;
  restore?: () => void;
};

export default function ProviderConfigJson({
  session,
  providerName,
  saving,
  commit,
}: {
  session: ProviderJsonSession;
  providerName: string;
  saving: boolean;
  commit: () => void;
}) {
  const t = useT();
  const headingId = useId();
  const fieldId = useId();
  const focusField = useCallback((node: HTMLTextAreaElement | null) => {
    node?.focus();
  }, []);
  if (!session.visible) return null;

  const blocked = saving || !session.dirty;
  return (
    <form
      className="provider-json"
      aria-labelledby={headingId}
      onSubmit={event => {
        event.preventDefault();
        if (!blocked) commit();
      }}
    >
      <fieldset className="provider-json-frame">
        <legend id={headingId} className="provider-json-heading">
          {t("pws.jsonEditorTitle", { name: providerName })}
        </legend>
        <menu className="provider-json-actions" type="toolbar">
          {session.restore && session.dirty ? (
            <li>
              <button type="button" className="btn btn-ghost btn-sm" onClick={session.restore}>
                {t("pws.jsonRestore")}
              </button>
            </li>
          ) : null}
          <li>
            <button type="button" className="btn btn-ghost btn-sm" onClick={session.close}>
              {t("common.cancel")}
            </button>
          </li>
          <li>
            <button type="submit" className="btn btn-primary btn-sm" disabled={blocked}>
              {saving ? t("pws.saving") : t("pws.jsonSave")}
            </button>
          </li>
        </menu>
        <p className="provider-json-help muted" id={fieldId}>{t("pws.jsonEditorDesc")}</p>
        <textarea
          className="input provider-json-field"
          value={session.text}
          onChange={event => session.setText(event.target.value)}
          spellCheck={false}
          rows={20}
          ref={focusField}
          aria-describedby={fieldId}
          aria-label={t("pws.jsonEditorDesc")}
        />
      </fieldset>
    </form>
  );
}
