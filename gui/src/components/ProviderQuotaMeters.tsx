import type { QuotaMeter, QuotaSurface } from "../provider-workspace/quota-presentation";
import { IconAlert } from "../icons";

function WaitMeters({ layout, className, label }: { layout: "compact" | "stacked"; className?: string; label: string }) {
  const stacked = layout === "stacked";
  const root = stacked ? "pq-meters pq-meters--stack pq-meters--wait" : "pq-meters pq-meters--inline pq-meters--wait";
  return (
    <ul className={className ? `${root} ${className}` : root} aria-busy="true">
      {Array.from({ length: stacked ? 2 : 1 }, (_, index) => (
        <li key={index} className={stacked ? "pq-meter pq-meter--wait-stack" : "pq-meter pq-meter--wait-inline"} aria-hidden="true" />
      ))}
      <li className="sr-only" role="status">{label}</li>
    </ul>
  );
}

function CompactMeter({ meter }: { meter: QuotaMeter }) {
  return (
    <li className="pq-meter pq-meter--inline" data-warn={meter.warn || undefined} data-spent={meter.spent || undefined}>
      <strong className="pq-caption" title={meter.resetTitle}>{meter.caption}</strong>
      <em className="pq-reset-word">{meter.resetWord}</em>
      <time className="pq-reset-day">{meter.resetDay}</time>
      <time className="pq-reset-time">{meter.resetTime}</time>
      <meter
        className={meter.tone === "warn" ? "pq-track pq-track--warn" : "pq-track pq-track--ok"}
        min={0}
        max={1}
        value={meter.fill}
        title={meter.resetTitle}
      />
      <data className={meter.warn ? "pq-value pq-value--warn" : "pq-value"} value={meter.fill} title={meter.spent ? meter.spentCopy : meter.resetTitle}>
        {meter.warn ? <IconAlert width={12} height={12} aria-hidden="true" /> : null}
        {meter.valueLabel}
      </data>
    </li>
  );
}

function StackedMeter({ meter }: { meter: QuotaMeter }) {
  return (
    <li className="pq-meter pq-meter--stack" data-warn={meter.warn || undefined} data-spent={meter.spent || undefined}>
      <header className="pq-meter-head">
        <strong className="pq-limit">{meter.limitCaption}</strong>
        {meter.partial ? (
          <small className="pq-partial" role="note" aria-label={meter.partialA11y} title={meter.partialA11y}>
            {meter.partialCopy}
          </small>
        ) : null}
      </header>
      <div className="pq-meter-body">
        <meter
          className={meter.tone === "warn" ? "pq-track pq-track--warn" : "pq-track pq-track--ok"}
          min={0}
          max={1}
          value={meter.fill}
          title={meter.resetTitle}
        />
        <data className={meter.warn ? "pq-used pq-used--warn" : "pq-used"} value={meter.fill}>{meter.usedLabel}</data>
        <small className="pq-reset muted">{meter.stackedReset}</small>
      </div>
      {meter.spent ? (
        <p className="pq-spent" role="status">
          <IconAlert width={12} height={12} aria-hidden="true" />
          {meter.spentCopy}
        </p>
      ) : null}
    </li>
  );
}

export default function ProviderQuotaMeters({ surface }: { surface: QuotaSurface }) {
  if (surface.mode === "wait") {
    return <WaitMeters layout={surface.layout} className={surface.className} label={surface.loadingLabel} />;
  }
  if (surface.mode === "none") return null;
  const root = surface.layout === "stacked" ? "pq-meters pq-meters--stack" : "pq-meters pq-meters--inline";
  const Item = surface.layout === "stacked" ? StackedMeter : CompactMeter;
  return (
    <ul className={surface.className ? `${root} ${surface.className}` : root}>
      {surface.meters.map(meter => <Item key={meter.id} meter={meter} />)}
    </ul>
  );
}
