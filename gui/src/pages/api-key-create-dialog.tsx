/**
 * Compact create-key dialog. After a successful create it shows the one-time
 * secret exactly once; Done dismisses that transient reveal.
 *
 * Portaled to body like Add Provider: `.app`/`.main` overflow:hidden makes a
 * nested overlay's backdrop-filter sample an empty layer and paint black.
 */
import { useEffect, useRef } from "react";
import { createPortal } from "react-dom";
import { IconX } from "../icons";
import { useT } from "../i18n/shared";
import { API_KEY_NAME_MAX_LENGTH } from "../api-access/key-display";

export function ApiKeyCreateDialog({
  open,
  name,
  creating,
  newKey,
  copied,
  onNameChange,
  onCreate,
  onCopy,
  onClose,
}: {
  open: boolean;
  name: string;
  creating: boolean;
  newKey: string | null;
  copied: boolean;
  onNameChange: (value: string) => void;
  onCreate: () => void;
  onCopy: () => void;
  onClose: () => void;
}) {
  const t = useT();
  const cardRef = useRef<HTMLDivElement>(null);
  const previousFocusRef = useRef<HTMLElement | null>(null);
  const titleId = "api-create-key-title";
  const revealing = newKey !== null;

  useEffect(() => {
    if (!open) return;
    previousFocusRef.current = document.activeElement as HTMLElement | null;
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("keydown", onKey);
      previousFocusRef.current?.focus();
    };
  }, [open, onClose]);

  useEffect(() => {
    if (!open) return;
    const card = cardRef.current;
    const input = card?.querySelector<HTMLElement>("input:not([disabled])");
    const primary = card?.querySelector<HTMLElement>(".add-provider-foot .add-provider-continue");
    (input ?? primary)?.focus();
  }, [open, revealing]);

  if (!open) return null;

  return createPortal(
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby={titleId}
      className="modal-overlay add-provider-overlay"
    >
      <div ref={cardRef} className="modal-card api-create-dialog" role="document">
        <div className="modal-head api-create-head">
          <h3 id={titleId}>{revealing ? t("api.newKeyTitle") : t("api.createTitle")}</h3>
          <button type="button" className="btn btn-ghost btn-icon" aria-label={t("common.close")} onClick={onClose}>
            <IconX />
          </button>
        </div>
        {revealing ? (
          <>
            <div className="api-create-body">
              <p className="muted small">{t("api.newKeyNote")}</p>
              <code className="api-code api-create-secret">{newKey}</code>
            </div>
            <div className="add-provider-foot">
              <button type="button" className="btn btn-ghost" onClick={onCopy}>
                {copied ? t("api.copied") : t("api.copy")}
              </button>
              <button type="button" className="btn btn-primary add-provider-continue" onClick={onClose}>
                {t("api.done")}
              </button>
            </div>
          </>
        ) : (
          <>
            <div className="api-create-body">
              <div className="api-create-field">
                <label className="field-label" htmlFor="api-create-name">{t("api.key.name")}</label>
                <input
                  id="api-create-name"
                  className="input"
                  type="text"
                  value={name}
                  maxLength={API_KEY_NAME_MAX_LENGTH}
                  disabled={creating}
                  onChange={event => onNameChange(event.target.value)}
                  onKeyDown={event => {
                    if (event.key === "Enter") { event.preventDefault(); onCreate(); }
                  }}
                />
              </div>
            </div>
            <div className="add-provider-foot">
              <button type="button" className="btn btn-ghost" onClick={onClose} disabled={creating}>
                {t("common.cancel")}
              </button>
              <button
                type="button"
                className="btn btn-primary add-provider-continue"
                onClick={onCreate}
                disabled={creating}
              >
                {creating ? t("api.generating") : t("api.create")}
              </button>
            </div>
          </>
        )}
      </div>
    </div>,
    document.body,
  );
}
