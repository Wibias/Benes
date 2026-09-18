/** Benes dashboard client for the Go proxy (`internal/server`). */
import type { Locale, TFn } from "../i18n/shared";
import { ArchivedCleanupPanel } from "./storage-archived-cleanup";
import { AutoCleanupPolicyPanel } from "./storage-policy-panel";

type StorageCleanupViewProps = {
  readonly apiBase: string;
  readonly locale: Locale;
  readonly t: TFn;
  readonly archivedCount: number;
  readonly archivedBytes: number;
  readonly truncated: boolean;
  readonly storageGeneration: number;
  readonly onDone: () => void;
};

/**
 * One titled section of the Cleanup tab.
 *
 * Both halves of this tab head themselves the same way, and the heading id is the anchor the
 * section announces itself with, so the title, its id, and the optional help line travel together.
 */
function CleanupSection({
  titleId,
  title,
  help,
  children,
}: {
  titleId: string;
  title: React.ReactNode;
  help?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <section className="storage-cleanup-section" aria-labelledby={titleId}>
      <h3 id={titleId} className="storage-cleanup-section__title">{title}</h3>
      {help}
      {children}
    </section>
  );
}

/**
 * What the run-now half shows.
 *
 * A truncated scan is refused before anything else is offered: the archive totals it reported are
 * a lower bound, so previewing a cleanup from them would remove the wrong files. An archive set
 * that is genuinely empty gets its own line rather than an empty panel.
 */
function ManualCleanupBody({
  apiBase,
  locale,
  t,
  archivedCount,
  archivedBytes,
  truncated,
  storageGeneration,
  onDone,
}: StorageCleanupViewProps) {
  const truncatedCopy = t("storage.cleanup.truncated");
  const emptyCopy = t("storage.cleanup.noArchives");
  if (truncated) {
    return <p className="storage-cleanup-truncated" role="status">{truncatedCopy}</p>;
  }
  if (archivedCount === 0) {
    return <p className="muted storage-manual-panel__status">{emptyCopy}</p>;
  }
  return (
    <ArchivedCleanupPanel
      apiBase={apiBase}
      locale={locale}
      t={t}
      onDone={onDone}
      archivedCount={archivedCount}
      archivedBytes={archivedBytes}
      storageGeneration={storageGeneration}
    />
  );
}

export function StorageCleanupView({
  apiBase,
  locale,
  t,
  archivedCount,
  archivedBytes,
  truncated,
  storageGeneration,
  onDone,
}: StorageCleanupViewProps) {
  return (
    <div className="storage-cleanup-view">
      <CleanupSection
        titleId="storage-auto-cleanup-title"
        title={t("storage.policy.title")}
        help={<p className="muted storage-cleanup-section__help">{t("storage.policy.help")}</p>}
      >
        <AutoCleanupPolicyPanel apiBase={apiBase} locale={locale} t={t} onDone={onDone} />
      </CleanupSection>
      <CleanupSection
        titleId="storage-manual-cleanup-title"
        title={t("storage.cleanup.runNow.title")}
      >
        <ManualCleanupBody
          apiBase={apiBase}
          locale={locale}
          t={t}
          archivedCount={archivedCount}
          archivedBytes={archivedBytes}
          truncated={truncated}
          storageGeneration={storageGeneration}
          onDone={onDone}
        />
      </CleanupSection>
    </div>
  );
}