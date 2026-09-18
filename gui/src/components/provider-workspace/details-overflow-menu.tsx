import { IconMore } from "../../icons";
import { useT } from "../../i18n/shared";

export function DetailOverflowMenu({
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
}: {
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
}) {
  const t = useT();
  return (
    <details ref={overflowRef} className="providers-menu">
      <summary aria-label={t("prov.menu.more")}><IconMore /></summary>
      <div className="providers-menu-list">
        <button type="button" disabled={menuBusy !== null} onClick={event => { void onValidate(event); }}>
          {menuBusy === "validate" ? t("pws.testing") : t("prov.menu.validate")}
        </button>
        {onSyncModels && (
          <button type="button" disabled={menuBusy !== null || modelsLoading} onClick={onSyncModels}>
            {t("prov.menu.syncModels")}
          </button>
        )}
        {onToggleDisabled && (
          <button type="button" disabled={isDefault || menuBusy !== null} onClick={onToggleDisabled}>
            {isDisabled ? t("prov.menu.enable") : t("prov.menu.disable")}
          </button>
        )}
        {onRemove && (
          <button type="button" disabled={menuBusy !== null} onClick={onRemove}>
            {t("pws.removeConfirmTitle")}
          </button>
        )}
        {onEditConfig && (
          <button type="button" disabled={menuBusy !== null} onClick={onEditConfig}>
            {t("prov.menu.rawConfig")}
          </button>
        )}
      </div>
    </details>
  );
}
