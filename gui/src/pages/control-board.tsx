import { type ComponentProps, type ReactNode } from "react";
import { Notice, Select, Switch } from "../ui";
import { DataSurfaceSkeleton } from "../components/data-surface";
import { useT, type TFn } from "../i18n/shared";
import type { StartupHealthData, StartupInstallAction, TrayStatusData } from "./startup-shared";
import { startupSurfaceFor, type StartupLoadState } from "./startup-page-runtime";
import {
  controlProtectionLabel,
  controlServiceLabel,
  controlShimLabel,
  nextTrayAction,
  routingModeOptions,
  traySwitchOn,
} from "./control-board-state";
import { useControlBoardSettings } from "./control-board-settings";

function ControlRow({
  title,
  hint,
  control,
}: {
  title: string;
  hint: string;
  control: ReactNode;
}) {
  return (
    <div className="control-row">
      <div className="control-row-copy">
        <strong>{title}</strong>
        <span className="muted">{hint}</span>
      </div>
      <div className="control-row-control">{control}</div>
    </div>
  );
}

/** The runtime-notice slot: the warning it shows, and whether the read is still in flight. */
export type ControlRuntimeNotice = {
  pending: boolean;
  warning: string | null;
  fix: string | null;
};

/** The notice's fix row: the command the listener suggests, with its copy affordance. */
function ControlNoticeFix({
  t,
  fix,
  copied,
  onCopy,
}: {
  t: TFn;
  fix: string;
  copied: string | null;
  onCopy: (command: string) => void;
}) {
  return (
    <div className="startup-runtime-notice__fix">
      <code>{fix}</code>
      <button type="button" className="btn btn-ghost btn-sm" onClick={() => void onCopy(fix)}>
        {copied === fix ? t("startup.copied") : t("startup.copy")}
      </button>
    </div>
  );
}

/**
 * The Codex runtime notice keeps its height while the runtime settings are still being read,
 * so the board below it does not jump when the notice resolves.
 */
function ControlRuntimeNoticeSlot({
  t,
  notice,
  copied,
  onCopy,
}: {
  t: TFn;
  notice: ControlRuntimeNotice;
  copied: string | null;
  onCopy: (command: string) => void;
}) {
  const { pending, warning, fix } = notice;
  if (!pending && !warning) return null;
  const placeholder = pending && !warning;
  return (
    <div
      className={`startup-runtime-notice-slot${placeholder ? " startup-runtime-notice-slot--pending" : ""}`}
      aria-hidden={placeholder ? true : undefined}
    >
      {warning && (
        <div className="notice notice-warn startup-page-notice startup-runtime-notice" role="status">
          <p className="startup-runtime-notice__text">{warning}</p>
          {fix && <ControlNoticeFix t={t} fix={fix} copied={copied} onCopy={onCopy} />}
        </div>
      )}
    </div>
  );
}

/** The board's own props, forwarded untouched, plus the state the surface wraps around it. */
export type ControlHealthPanelProps = Omit<ComponentProps<typeof ControlBoard>, "data" | "failed"> & {
  t: TFn;
  loadState: StartupLoadState;
  data: StartupHealthData | null;
  /** The staleness the shell reports; the surface turns it into the board's own flag. */
  failed: boolean;
  runtimeNotice: ControlRuntimeNotice;
  copied: string | null;
  onCopy: (command: string) => void;
  onRefresh: () => void;
};

/**
 * The board in every state it can be read in. `startupSurfaceFor` decides which one, so the
 * rule is testable without a renderer; this maps the decision onto the shell slot, the
 * runtime notice, and the board, and forwards the board's own props untouched.
 */
export function ControlHealthPanel(props: ControlHealthPanelProps) {
  const { t, loadState, data, failed, runtimeNotice, copied, onCopy, onRefresh, ...boardProps } = props;
  const surface = startupSurfaceFor({ loadState, data, failed });
  if (surface.kind === "cold-failure") {
    return (
      <div className="startup-page-notice">
        <Notice tone="err">{surface.reason ?? t("startup.error")}</Notice>
        <button type="button" className="btn btn-ghost btn-sm" onClick={onRefresh}>
          {t("common.retry")}
        </button>
      </div>
    );
  }
  if (surface.kind === "loading" || !data) {
    return <DataSurfaceSkeleton label={t("startup.loading")} rows={5} />;
  }
  return (
    <>
      {surface.readFailed && <Notice tone="err">{t("startup.error")}</Notice>}
      {surface.unresolved && (
        <div className="notice notice-warn startup-page-notice" role="alert">
          {t("startup.staleData")}
        </div>
      )}
      <ControlRuntimeNoticeSlot t={t} notice={runtimeNotice} copied={copied} onCopy={onCopy} />
      <ControlBoard data={data} failed={surface.unresolved} {...boardProps} />
    </>
  );
}


function ControlRuntimeColumn({
  data,
  actionsDisabled,
  tray,
  trayBusy,
  trayError,
  settingsOn,
  settingsReady,
  settingsSaving,
  onRoutingNative,
  onToggleAutostart,
  onTrayAction,
}: {
  data: StartupHealthData;
  actionsDisabled: boolean;
  tray: TrayStatusData | null;
  trayBusy: boolean;
  trayError: boolean;
  settingsOn: boolean;
  settingsReady: boolean;
  settingsSaving: boolean;
  onRoutingNative: () => void;
  onToggleAutostart: () => void;
  onTrayAction: (action: "install" | "start" | "stop" | "uninstall") => void;
}) {
  const t = useT();
  const routingOptions = routingModeOptions(data.routingKind, {
    "benes-local": t("control.routingMode.benesLocal"),
    native: t("startup.routing.native"),
    "custom-local": t("startup.routing.customLocal"),
    "custom-remote": t("startup.routing.customRemote"),
    unknown: t("startup.routing.unknown"),
  });
  const onTrayToggle = () => {
    if (trayBusy || trayError) return;
    const action = nextTrayAction(tray);
    if (action) onTrayAction(action);
  };
  return (
    <section className="control-column" aria-label={t("control.runtime")}>
      <h3>{t("control.runtime")}</h3>
      <ControlRow
        title={t("control.routingMode")}
        hint={t("control.routingModeHint")}
        control={(
          <Select
            value={data.routingKind}
            options={routingOptions}
            onChange={value => { if (value === "native" && data.routingKind !== "native") onRoutingNative(); }}
            disabled={actionsDisabled}
            label={t("control.routingMode")}
            align="right"
            chevron="down"
          />
        )}
      />
      <ControlRow
        title={t("control.restartProtection")}
        hint={t("control.restartProtectionHint")}
        control={<span className="control-status">{t(controlProtectionLabel(data))}</span>}
      />
      <ControlRow
        title={t("control.benesService")}
        hint={t("control.benesServiceHint")}
        control={<span className="control-status">{t(controlServiceLabel(data))}</span>}
      />
      <ControlRow
        title={t("control.codexShim")}
        hint={t("control.codexShimHint")}
        control={<span className="control-status">{t(controlShimLabel(data))}</span>}
      />
      <ControlRow
        title={t("control.autostart")}
        hint={t("control.autostartHint")}
        control={(
          <Switch
            on={settingsOn}
            onClick={onToggleAutostart}
            disabled={!settingsReady || settingsSaving || actionsDisabled}
            label={t("control.autostart")}
          />
        )}
      />
      <ControlRow
        title={t("control.windowsTray")}
        hint={t("control.windowsTrayHint")}
        control={(
          <Switch
            on={traySwitchOn(tray)}
            onClick={onTrayToggle}
            disabled={data.platform !== "win32" || trayBusy || actionsDisabled}
            label={t("control.windowsTray")}
          />
        )}
      />
    </section>
  );
}

function ControlRequestColumn({
  extras,
}: {
  extras: ReturnType<typeof useControlBoardSettings>;
}) {
  const t = useT();
  const {
    shadowCall, shadowSaving, sidecar, sidecarSaving, shadowOptions, visionOptions,
    visionOn, saveShadow, saveVisionModel, toggleVision,
  } = extras;
  return (
    <section className="control-column" aria-label={t("control.requestHandling")}>
      <h3>{t("control.requestHandling")}</h3>
      <ControlRow
        title={t("control.shadowIntercept")}
        hint={t("control.shadowInterceptHint")}
        control={(
          <Switch
            on={shadowCall?.enabled ?? false}
            onClick={() => { void saveShadow({ enabled: !shadowCall?.enabled }); }}
            disabled={!shadowCall || shadowSaving}
            label={t("control.shadowIntercept")}
          />
        )}
      />
      <ControlRow
        title={t("control.shadowModel")}
        hint={t("control.shadowModelHint")}
        control={(
          <Select
            value={shadowCall?.model ?? ""}
            options={shadowOptions}
            onChange={value => { void saveShadow({ model: value }); }}
            disabled={!shadowCall || shadowSaving || !shadowCall.enabled}
            label={t("control.shadowModel")}
            align="right"
            chevron="down"
          />
        )}
      />
      <ControlRow
        title={t("control.visionSidecar")}
        hint={t("control.visionSidecarHint")}
        control={(
          <Switch
            on={visionOn}
            onClick={toggleVision}
            disabled={!sidecar || sidecarSaving}
            label={t("control.visionSidecar")}
          />
        )}
      />
      <ControlRow
        title={t("control.visionModel")}
        hint={t("control.visionModelHint")}
        control={(
          <Select
            value={sidecar?.vision.model ?? ""}
            options={visionOptions}
            onChange={saveVisionModel}
            disabled={!sidecar || sidecarSaving || !visionOn}
            label={t("control.visionModel")}
            align="right"
            chevron="down"
          />
        )}
      />
      <ControlContextProjectionRow extras={extras} />
      <ControlFabricRow extras={extras} />
    </section>
  );
}

function ControlFabricRow({
  extras,
}: {
  extras: ReturnType<typeof useControlBoardSettings>;
}) {
  const t = useT();
  return (
    <ControlRow
      title={t("control.fabric")}
      hint={t("control.fabricHint")}
      control={(
        <Switch
          on={extras.fabric?.enabled ?? false}
          onClick={() => { void extras.saveFabric(!extras.fabric?.enabled); }}
          disabled={!extras.fabric || extras.fabricSaving}
          label={t("control.fabric")}
        />
      )}
    />
  );
}

function ControlContextProjectionRow({
  extras,
}: {
  extras: ReturnType<typeof useControlBoardSettings>;
}) {
  const t = useT();
  const { contextProjection, projectionSaving, saveContextProjection } = extras;
  return (
    <ControlRow
      title={t("control.contextProjection")}
      hint={t("control.contextProjectionHint")}
      control={(
        <Select
          value={contextProjection?.mode ?? "off"}
          options={[
            { value: "off", label: t("control.contextProjection.off") },
            { value: "shadow", label: t("control.contextProjection.shadow") },
            { value: "duplicate", label: t("control.contextProjection.duplicate") },
            { value: "recovery", label: t("control.contextProjection.recovery") },
          ]}
          onChange={saveContextProjection}
          disabled={!contextProjection || projectionSaving}
          label={t("control.contextProjection")}
          align="right"
          chevron="down"
        />
      )}
    />
  );
}

function ControlRecoveryColumn({
  data,
  actionsDisabled,
  installBusy,
  restoreBusy,
  onInstall,
  onRestoreNative,
}: {
  data: StartupHealthData;
  actionsDisabled: boolean;
  installBusy: StartupInstallAction | null;
  restoreBusy: boolean;
  onInstall: (action: StartupInstallAction, opts?: { repair?: boolean }) => void;
  onRestoreNative: () => void;
}) {
  const t = useT();
  return (
    <section className="control-column" aria-label={t("control.recovery")}>
      <h3>{t("control.recovery")}</h3>
      <ControlRow
        title={t("control.restoreNative")}
        hint={t("control.restoreNativeHint")}
        control={(
          <button type="button" className="btn btn-ghost" disabled={restoreBusy || actionsDisabled} onClick={onRestoreNative}>
            {t("control.restore")}
          </button>
        )}
      />
      <ControlRow
        title={t("control.repairService")}
        hint={t("control.repairServiceHint")}
        control={(
          <button
            type="button"
            className="btn btn-ghost"
            disabled={actionsDisabled || !data.serviceSupported}
            onClick={() => onInstall("install-service", { repair: data.serviceInstalled })}
          >
            {t(installBusy === "install-service" ? "startup.repairing" : "control.repair")}
          </button>
        )}
      />
      <ControlRow
        title={t("control.repairShim")}
        hint={t("control.repairShimHint")}
        control={(
          <button
            type="button"
            className="btn btn-ghost"
            disabled={actionsDisabled}
            onClick={() => onInstall("install-shim", { repair: data.shimInstalled })}
          >
            {t(installBusy === "install-shim" ? "startup.repairing" : "control.repair")}
          </button>
        )}
      />
    </section>
  );
}

export function ControlBoard({
  data,
  failed,
  loading,
  installBusy,
  tray,
  trayBusy,
  trayError,
  onInstall,
  onTrayAction,
  onRestoreNative,
  restoreBusy,
  apiBase,
  refreshNonce,
}: {
  data: StartupHealthData;
  failed: boolean;
  loading: boolean;
  installBusy: StartupInstallAction | null;
  tray: TrayStatusData | null;
  trayBusy: boolean;
  trayError: boolean;
  onInstall: (action: StartupInstallAction, opts?: { repair?: boolean }) => void;
  onTrayAction: (action: "install" | "start" | "stop" | "uninstall") => void;
  onRestoreNative: () => void;
  restoreBusy: boolean;
  apiBase: string;
  refreshNonce: number;
}) {
  const t = useT();
  const actionsDisabled = installBusy !== null || failed || loading;
  const extras = useControlBoardSettings(apiBase, refreshNonce, t);
  return (
    <div className="control-board">
      <ControlRuntimeColumn
        data={data}
        actionsDisabled={actionsDisabled}
        tray={tray}
        trayBusy={trayBusy}
        trayError={trayError}
        settingsOn={Boolean(extras.settings?.codexAutoStart)}
        settingsReady={Boolean(extras.settings)}
        settingsSaving={extras.settingsSaving}
        onRoutingNative={onRestoreNative}
        onToggleAutostart={() => { void extras.toggleAutostart(); }}
        onTrayAction={onTrayAction}
      />
      <div className="control-column-stack">
        <ControlRequestColumn extras={extras} />
        <ControlRecoveryColumn
          data={data}
          actionsDisabled={actionsDisabled}
          installBusy={installBusy}
          restoreBusy={restoreBusy}
          onInstall={onInstall}
          onRestoreNative={onRestoreNative}
        />
      </div>
    </div>
  );
}
