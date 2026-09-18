import { IconArrowLeft } from "../../icons";
import { useT, type TFn } from "../../i18n/shared";
import { formatProviderDisplayName } from "../../provider-icons";
import type { WorkspaceItem } from "../../provider-workspace/catalog";
import { ProviderMark } from "../ProviderMark";
import { DetailOverflowMenu } from "./details-overflow-menu";

interface IdentityBadge {
  key: string;
  className: string;
  label: string;
}

/**
 * Provenance badges are mutually exclusive: a loopback provider is local by
 * transport, so showing Free next to Local would state two tiers at once.
 */
function identityBadges(local: boolean, free: boolean, t: TFn): IdentityBadge[] {
  if (local) return [{ key: "local", className: "pwi-rail-badge pwi-rail-badge--local", label: t("modal.badge.local") }];
  if (free) return [{ key: "free", className: "pwi-rail-badge pwi-rail-badge--free", label: t("modal.badge.free") }];
  return [];
}

function DetailBackButton({ label, onClick }: { label: string; onClick: () => void }) {
  return (
    <button type="button" className="pws-detail-back" onClick={onClick} aria-label={label}>
      <IconArrowLeft width={24} height={24} aria-hidden="true" />
    </button>
  );
}

export interface DetailHeaderProps {
  item: WorkspaceItem;
  statusClass: string;
  statusLabel: string;
  local: boolean;
  free: boolean;
  onBack?: () => void;
  overflowRef?: React.Ref<HTMLDetailsElement>;
  menuBusy: "validate" | "sync" | null;
  modelsLoading?: boolean;
  isDefault?: boolean;
  isDisabled: boolean;
  onValidate: (event: React.MouseEvent<HTMLElement>) => void;
  onSyncModels?: (event: React.MouseEvent<HTMLElement>) => void;
  onToggleDisabled?: (event: React.MouseEvent<HTMLElement>) => void;
  onRemove?: (event: React.MouseEvent<HTMLElement>) => void;
  onEditConfig?: (event: React.MouseEvent<HTMLElement>) => void;
}

export function DetailHeader({
  item,
  statusClass,
  statusLabel,
  local,
  free,
  onBack,
  overflowRef,
  menuBusy,
  modelsLoading,
  isDefault,
  isDisabled,
  onValidate,
  onSyncModels,
  onToggleDisabled,
  onRemove,
  onEditConfig,
}: DetailHeaderProps) {
  const t = useT();
  const badges = identityBadges(local, free, t);
  return (
    <>
      {onBack && <DetailBackButton label={t("prov.overview.back")} onClick={onBack} />}
      <div className="pws-detail-head-main">
        <ProviderMark name={item.name} adapter={item.adapter} baseUrl={item.baseUrl} className="pws-detail-icon" />
        <div className="pws-detail-title-wrap">
          <div className="providers-detail-title-row">
            <h2 className="pws-detail-title">{formatProviderDisplayName(item.name, t)}</h2>
            <span className={`providers-status ${statusClass}`}>
              <span className="providers-pill-dot" />
              {statusLabel}
            </span>
            {badges.map(badge => (
              <span key={badge.key} className={badge.className}>{badge.label}</span>
            ))}
          </div>
        </div>
        <DetailOverflowMenu
          overflowRef={overflowRef}
          menuBusy={menuBusy}
          modelsLoading={modelsLoading}
          isDefault={isDefault}
          isDisabled={isDisabled}
          onValidate={onValidate}
          onSyncModels={onSyncModels}
          onToggleDisabled={onToggleDisabled}
          onRemove={onRemove}
          onEditConfig={onEditConfig}
        />
      </div>
    </>
  );
}
