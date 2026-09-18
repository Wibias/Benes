import type { ReactNode } from "react";
import { ToastNotice, type NoticeTone } from "../ui";
import { ProviderNoticeCopy } from "../components/provider-notice-copy";

export type ProvidersNoticeModel = {
  text: string;
  tone: NoticeTone;
  actionLabel?: string;
  actionBusy?: boolean;
};

export function ProvidersNoticeBanner({
  notice,
  closeLabel,
  onDismiss,
  onAction,
}: {
  notice: ProvidersNoticeModel | null;
  closeLabel: string;
  onDismiss: () => void;
  onAction?: () => void;
}): ReactNode {
  if (!notice) return null;
  return (
    <ToastNotice tone={notice.tone} onDismiss={onDismiss} dismissLabel={closeLabel}>
      <ProviderNoticeCopy
        message={notice.text}
        actionLabel={notice.actionLabel ?? ""}
        onAction={notice.actionLabel ? onAction : undefined}
        busy={notice.actionBusy}
      />
    </ToastNotice>
  );
}

export function ProvidersWorkspaceBoot({
  title,
  status,
}: {
  title: string;
  status: string;
}) {
  return (
    <section className="providers-workspace providers-workspace--boot" aria-busy="true" aria-labelledby="providers-boot-title">
      <header className="page-head">
        <h2 id="providers-boot-title">{title}</h2>
      </header>
      <aside className="providers-workspace-rail providers-workspace-rail--boot" aria-hidden="true" />
      <div className="providers-workspace-main">
        <p className="muted" role="status">
          <span className="spin" aria-hidden="true" /> {status}
        </p>
      </div>
    </section>
  );
}
