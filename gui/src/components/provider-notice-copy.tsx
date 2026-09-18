import type { ReactNode } from "react";
import { providerNoticeActionParts } from "../lib/provider-notice-policy";

export function ProviderNoticeCopy({
  message,
  actionLabel,
  onAction,
  busy,
}: {
  message: string;
  actionLabel: string;
  onAction?: () => void;
  busy?: boolean;
}): ReactNode {
  const parts = providerNoticeActionParts(message);
  if (!parts || !onAction) return message;
  return (
    <>
      {parts.pre}
      <button type="button" className="toast-notice-action" onClick={onAction} disabled={busy}>
        {actionLabel}
      </button>
      {parts.post}
    </>
  );
}
