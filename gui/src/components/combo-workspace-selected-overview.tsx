/** Benes dashboard client for the Go proxy (`internal/server`). */
import type { ComboItem } from "../combo-workspace-data";
import {
  comboClientModelId,
  comboDisplaySecondaryIdentity,
  comboDisplayTitle,
  comboHasLegacyConfig,
  formatLegacySparseWeights,
} from "../combo-workspace-data";
import { IconArrowLeft } from "../icons";
import { navigateHash } from "../hash-routing";
import { useT, type TFn } from "../i18n/shared";
import { sessionsHashForCombo } from "../pages/sessions-hash-filter";
import { formatProviderDisplayName } from "../provider-icons";

function ComboKv({ label, value }: { label: string; value: string }) {
  return (
    <div className="combos-overview-kv">
      <dt>{label}</dt>
      <dd>{value}</dd>
    </div>
  );
}

function IdentitySection({ item, client, t }: { item: ComboItem; client: string; t: TFn }) {
  return (
    <section className="combos-overview-section">
      <h3 className="combos-overview-section-title">{t("cws.section.identity")}</h3>
      <dl className="combos-overview-dl">
        <ComboKv label={t("routing.clientModel")} value={client} />
        {item.alias?.trim() ? <ComboKv label={t("cws.field.alias")} value={item.alias.trim()} /> : null}
        {item.nativeAlias ? (
          <ComboKv label={t("cws.field.nativeAlias")} value={t("cws.overview.nativeAliasOn")} />
        ) : null}
        {item.displayName?.trim() ? (
          <ComboKv label={t("cws.field.displayName")} value={item.displayName.trim()} />
        ) : null}
      </dl>
    </section>
  );
}

function TargetsSection({ item, t }: { item: ComboItem; t: TFn }) {
  return (
    <section className="combos-overview-section">
      <h3 className="combos-overview-section-title">{t("cws.targets")}</h3>
      {item.targets.length === 0 ? (
        <p className="muted">{t("cws.rail.noTargets")}</p>
      ) : (
        <table className="combos-overview-targets">
          <thead>
            <tr>
              <th scope="col">#</th>
              <th scope="col">{t("cws.target.provider")}</th>
              <th scope="col">{t("cws.target.model")}</th>
            </tr>
          </thead>
          <tbody>
            {item.targets.map((row, index) => (
              <tr key={`${row.provider}/${row.model}/${index}`}>
                <td>{index + 1}</td>
                <td>{formatProviderDisplayName(row.provider, t)}</td>
                <td className="mono">{row.model}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      <p className="muted combos-overview-hint">{t("cws.targets.failoverHint")}</p>
    </section>
  );
}

function AvailabilitySection({ item, t }: { item: ComboItem; t: TFn }) {
  const empty = item.targets.length === 0;
  const status = empty ? t("cws.rail.noTargets") : t("cws.rail.notInCatalog");
  // P1-approved truthful support line only for catalog omission (no invented causes).
  const support = empty ? null : t("cws.attention.catalogOmitted");
  return (
    <section className="combos-overview-section">
      <h3 className="combos-overview-section-title">{t("cws.section.availability")}</h3>
      <div className="combos-overview-availability">
        <span className="combos-overview-availability-label">{t("cws.overview.modelCatalog")}</span>
        <div className="combos-overview-availability-body">
          <div className="combos-overview-availability-status">{status}</div>
          {support ? <p className="muted combos-overview-availability-support">{support}</p> : null}
        </div>
      </div>
    </section>
  );
}

function LegacySection({ item, t }: { item: ComboItem; t: TFn }) {
  const weightLabel = formatLegacySparseWeights(item.targets);
  return (
    <section className="combos-overview-section">
      <h3 className="combos-overview-section-title">{t("cws.section.legacy")}</h3>
      <p className="muted combos-overview-hint">{t("cws.legacy.blurb")}</p>
      <dl className="combos-overview-dl">
        {item.strategy === "round-robin" ? (
          <>
            <ComboKv label={t("cws.legacy.storedStrategy")} value={t("cws.strategy.roundRobin")} />
            <ComboKv label={t("cws.legacy.runtimeBehaviour")} value={t("cws.strategy.failover")} />
          </>
        ) : null}
        {item.strategy === "unsupported" ? (
          <>
            <ComboKv
              label={t("cws.legacy.storedStrategy")}
              value={item.storedStrategy?.trim() || t("cws.strategy.unsupported")}
            />
            <ComboKv label={t("cws.legacy.runtimeBehaviour")} value={t("cws.legacy.notExecutable")} />
          </>
        ) : null}
        {item.stickyLimit !== undefined ? (
          <ComboKv label={t("cws.field.stickyLimit")} value={String(item.stickyLimit)} />
        ) : null}
        {weightLabel ? <ComboKv label={t("cws.legacy.weights")} value={weightLabel} /> : null}
        {item.defaultEffort != null ? (
          <ComboKv label={t("cws.field.defaultEffort")} value={item.defaultEffort} />
        ) : null}
        {item.imageInput === "disabled" ? (
          <ComboKv label={t("cws.capability.imageInput")} value={t("cws.legacy.imageDisabled")} />
        ) : null}
      </dl>
    </section>
  );
}

export function ComboSelectedOverview({
  item,
  catalogued,
  onBack,
  onEdit,
}: {
  item: ComboItem;
  catalogued: boolean;
  onBack: () => void;
  onEdit: () => void;
}) {
  const t = useT();
  const title = comboDisplayTitle(item);
  const secondary = comboDisplaySecondaryIdentity(item);
  const client = comboClientModelId(item.id, item.alias, item.nativeAlias);
  const showLegacy = comboHasLegacyConfig(item);
  const showAvailability = item.targets.length === 0 || !catalogued;

  return (
    <div className="combos-workspace-detail">
      <button
        type="button"
        className="routing-detail-back"
        onClick={onBack}
        aria-label={t("cws.backToAll")}
      >
        <IconArrowLeft width={24} height={24} aria-hidden="true" />
      </button>
      <div className="combos-workspace-detail-head">
        <div className="combos-workspace-detail-titles">
          <h2 className="combos-workspace-detail-title">{title}</h2>
          {secondary ? <p className="muted combos-workspace-detail-sub">{secondary}</p> : null}
        </div>
        <div className="combos-workspace-detail-actions">
          {item.id.trim() ? (
            <button
              type="button"
              className="providers-link"
              onClick={() => navigateHash(sessionsHashForCombo(item.id))}
            >
              {t("routing.viewSessions")}
            </button>
          ) : null}
          <button type="button" className="btn btn-primary btn-sm" onClick={onEdit}>
            {t("cws.editCombo")}
          </button>
        </div>
      </div>

      <article className="combos-overview-body">
        <IdentitySection item={item} client={client} t={t} />
        <TargetsSection item={item} t={t} />
        {showAvailability ? <AvailabilitySection item={item} t={t} /> : null}
        {showLegacy ? <LegacySection item={item} t={t} /> : null}
      </article>
    </div>
  );
}
