/** Benes dashboard client for the Go proxy (`internal/server`). */
import type { TFn } from "../i18n/shared.ts";
import { navigateHash } from "../hash-routing.ts";
import {
  SESSIONS_ROUTING_BANNER_ROOT_CLASS,
  projectSessionsRoutingBanner,
  sessionsRoutingBannerIsVisible,
} from "./sessions-routing-banner-model.ts";

type SessionsRoutingBannerProps = {
  t: TFn;
  policyId: string;
  comboId: string;
};

function dismissSessionsRoutingFilter(clearHash: string): void {
  navigateHash(clearHash);
}

export function SessionsRoutingBanner(props: SessionsRoutingBannerProps) {
  const projection = projectSessionsRoutingBanner(props.policyId, props.comboId);
  if (!sessionsRoutingBannerIsVisible(projection) || projection.sentenceKey === null) {
    return null;
  }

  const sentence = props.t(projection.sentenceKey, { id: projection.subjectId });
  const clearLabel = props.t(projection.clearKey);

  return (
    <div className={SESSIONS_ROUTING_BANNER_ROOT_CLASS} data-routing-filter={projection.kind}>
      <span className="muted">{sentence}</span>
      <button
        type="button"
        className="btn btn-ghost btn-sm"
        onClick={() => dismissSessionsRoutingFilter(projection.clearHash)}
      >
        {clearLabel}
      </button>
    </div>
  );
}
