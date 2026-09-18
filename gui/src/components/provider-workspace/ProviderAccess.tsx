/**
 * ProviderAccess — capability-driven Access tab.
 */
import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { useT, type TFn } from "../../i18n/shared";
import { navigateHash } from "../../hash-routing";
import { isAccountProvider, isLocalProvider, type WorkspaceItem } from "../../provider-workspace/catalog";
import { providerEventLabel } from "../../provider-workspace/events";
import { nextDetailTabIndex } from "../../provider-workspace/connection-test";
import {
  deriveAccessDescriptor,
  showCredentialSelection,
} from "../../provider-workspace/auth";

import {
  accessCredentialLane,
  accessEmptyHintKey,
  accessShowsCredentialLaneSwitch,
  credentialSelectionDisplay,
  credentialSelectionFacts,
  recentAccessEvents,
  type AccessCredentialLane,
} from "../../provider-workspace/access-presentation";
import ProviderAuthPanel from "./ProviderAuthPanel";
import { ProviderEvents } from "./ProviderEvents";
import CodexAccountPool from "../CodexAccountPool";
import CodexPoolStrategySetting from "../CodexPoolStrategySetting";
import CodexAutoSwitchSetting from "../CodexAutoSwitchSetting";
import { useCodexAutoSwitch } from "../../hooks/useCodexAutoSwitch";
import { DEFAULT_ACCOUNT_POOL_STRATEGY } from "../../account-pool-strategy";
import type { CodexAccountLoadObserver, CodexAccountPoolController } from "../../hooks/useCodexAccountPool";
import type { AccountLoadState } from "../../hooks/useProviderCredentials";
import type { LoginHint } from "../../pages/providers-chrome";
import type { ApiKeyRow, OAuthAccountRow } from "../../provider-workspace/provider-credential-api";
import type { ProviderUpdatePatch } from "../../provider-workspace/provider-mutations";
import type { WorkspaceEvent } from "./ProviderWorkspaceShell";

export interface ProviderAuthHandlers {
  onLogin: (provider: string, addAccount?: boolean) => void | Promise<void>;
  onCancelLogin?: (provider: string) => void;
  onLogout: (provider: string) => void | Promise<void>;
  onReauth: (provider: string, accountId?: string) => void | Promise<void>;
  onSwitchAccount: (provider: string, account: OAuthAccountRow) => void | Promise<void>;
  onRemoveAccount: (provider: string, account: OAuthAccountRow) => void | Promise<void>;
  onRetryAccounts?: (provider: string) => void | Promise<void>;
  onAddApiKey: (provider: string, key: string) => Promise<boolean>;
  onSwitchApiKey: (provider: string, entry: ApiKeyRow) => void | Promise<void>;
  onRemoveApiKey: (provider: string, entry: ApiKeyRow) => void | Promise<void>;
  onEditAlias: (provider: string, type: "oauth" | "api-key", id: string, current?: string) => void | Promise<void>;
}

export default function ProviderAccess({
  item, apiBase, oauth, accounts, keys, accountLoadState,
  switchingAccountId, busy, loginHint, authHandlers,
  onCodexActiveNeedsReauthChange, codexController,
  apiLane, recentEvents = [], onUpdateProvider,
}: {
  item: WorkspaceItem;
  apiBase: string;
  oauth?: { loggedIn: boolean; email?: string; error?: string };
  accounts?: OAuthAccountRow[];
  keys?: ApiKeyRow[];
  accountLoadState?: AccountLoadState;
  switchingAccountId?: string | null;
  busy?: boolean;
  loginHint?: LoginHint | null;
  authHandlers?: ProviderAuthHandlers;
  onCodexActiveNeedsReauthChange?: (needs: boolean) => void;
  codexController?: CodexAccountPoolController;
  apiLane?: WorkspaceItem;
  recentEvents?: WorkspaceEvent[];
  onUpdateProvider?: (name: string, patch: ProviderUpdatePatch) => Promise<{ ok: boolean; error?: string }>;
}) {
  const t = useT();
  const access = deriveAccessDescriptor(item, Boolean(apiLane) || Boolean(keys?.length));
  const oauthMethod = access.methods.find(method => method.kind === "oauth");
  const keyMethod = access.methods.find(method => method.kind === "api-key");
  const oauthCount = isAccountProvider(item.name, item)
    ? (codexController?.accounts.length ?? 0)
    : (accounts?.length ?? 0);
  const showSelection = showCredentialSelection(access, oauthCount, keys?.length ?? 0);

  if (access.methods.length === 0) {
    return (
      <div className="providers-access">
        <section className="providers-block">
          <p className="providers-access-hint">{t(accessEmptyHintKey(item.authMode, isLocalProvider(item)))}</p>
        </section>
      </div>
    );
  }

  return (
    <AccessConfiguredBody
      key={item.name}
      t={t}
      item={item}
      apiBase={apiBase}
      oauth={oauth}
      accounts={accounts}
      keys={keys}
      accountLoadState={accountLoadState}
      switchingAccountId={switchingAccountId}
      busy={busy}
      loginHint={loginHint}
      authHandlers={authHandlers}
      onCodexActiveNeedsReauthChange={onCodexActiveNeedsReauthChange}
      codexController={codexController}
      apiLane={apiLane}
      recentEvents={recentEvents}
      onUpdateProvider={onUpdateProvider}
      access={access}
      oauthMethod={oauthMethod}
      keyMethod={keyMethod}
      showSelection={showSelection}
    />
  );
}

function AccessCredentialLaneTabs({
  t, lane, onLane,
}: {
  t: TFn;
  lane: AccessCredentialLane;
  onLane: (next: AccessCredentialLane) => void;
}) {
  const tabs: { id: AccessCredentialLane; label: string }[] = [
    { id: "oauth", label: t("prov.access.oauthAccounts") },
    { id: "api-key", label: t("prov.access.apiKeys") },
  ];
  const onTabKeyDown = (event: React.KeyboardEvent<HTMLButtonElement>, index: number) => {
    const next = nextDetailTabIndex(event.key, index, tabs.length);
    if (next == null) return;
    event.preventDefault();
    onLane(tabs[next]!.id);
    event.currentTarget.parentElement
      ?.querySelectorAll<HTMLButtonElement>('[role="tab"]')[next]
      ?.focus();
  };
  return (
    <div className="providers-access-lanes" role="tablist" aria-label={t("prov.access.methods")}>
      {tabs.map((tab, index) => (
        <button
          key={tab.id}
          type="button"
          role="tab"
          aria-selected={lane === tab.id}
          aria-controls={tab.id === "oauth" ? "access-lane-oauth" : "access-lane-keys"}
          tabIndex={lane === tab.id ? 0 : -1}
          className={lane === tab.id ? "is-active" : undefined}
          onClick={() => onLane(tab.id)}
          onKeyDown={event => onTabKeyDown(event, index)}
        >
          {tab.label}
        </button>
      ))}
    </div>
  );
}

function AccessConfiguredBody({
  t, item, apiBase, oauth, accounts, keys, accountLoadState, switchingAccountId, busy, loginHint, authHandlers,
  onCodexActiveNeedsReauthChange, codexController, apiLane, recentEvents, onUpdateProvider,
  access, oauthMethod, keyMethod, showSelection,
}: {
  t: TFn;
  item: WorkspaceItem;
  apiBase: string;
  oauth?: { loggedIn: boolean; email?: string; error?: string };
  accounts?: OAuthAccountRow[];
  keys?: ApiKeyRow[];
  accountLoadState?: AccountLoadState;
  switchingAccountId?: string | null;
  busy?: boolean;
  loginHint?: LoginHint | null;
  authHandlers?: ProviderAuthHandlers;
  onCodexActiveNeedsReauthChange?: (needs: boolean) => void;
  codexController?: CodexAccountPoolController;
  apiLane?: WorkspaceItem;
  recentEvents: WorkspaceEvent[];
  onUpdateProvider?: (name: string, patch: ProviderUpdatePatch) => Promise<{ ok: boolean; error?: string }>;
  access: ReturnType<typeof deriveAccessDescriptor>;
  oauthMethod: ReturnType<typeof deriveAccessDescriptor>["methods"][number] | undefined;
  keyMethod: ReturnType<typeof deriveAccessDescriptor>["methods"][number] | undefined;
  showSelection: boolean;
}) {
  const [lane, setLane] = useState<AccessCredentialLane>("oauth");
  return (
    <div className="providers-access">
      <AccessCredentialTables
        t={t}
        item={item}
        apiBase={apiBase}
        oauth={oauth}
        accounts={accounts}
        keys={keys}
        accountLoadState={accountLoadState}
        switchingAccountId={switchingAccountId}
        busy={busy}
        loginHint={loginHint}
        authHandlers={authHandlers}
        onCodexActiveNeedsReauthChange={onCodexActiveNeedsReauthChange}
        codexController={codexController}
        apiLane={apiLane}
        oauthMethod={oauthMethod}
        keyMethod={keyMethod}
        lane={lane}
        onLane={setLane}
      />
      {access.defaultAccess && (
        <DefaultAccessSection
          t={t}
          item={item}
          methodId={access.defaultMethodId ?? "oauth"}
          apiAvailable={Boolean(keyMethod?.connectionPresent)}
          onUpdateProvider={onUpdateProvider}
        />
      )}
      {access.selection?.supported && (
        <CredentialSelectionSection
          t={t}
          apiBase={apiBase}
          item={item}
          selection={access.selection}
          interactive={showSelection}
          codexController={codexController}
          onUpdateProvider={onUpdateProvider}
        />
      )}
      {access.activitySupported && (
        <AccessActivitySection recentEvents={recentEvents} />
      )}
    </div>
  );
}

function AccessCredentialTables({
  t, item, apiBase, oauth, accounts, keys, accountLoadState, switchingAccountId, busy, loginHint, authHandlers,
  onCodexActiveNeedsReauthChange, codexController, apiLane, oauthMethod, keyMethod, lane, onLane,
}: {
  t: TFn;
  item: WorkspaceItem;
  apiBase: string;
  oauth?: { loggedIn: boolean; email?: string; error?: string };
  accounts?: OAuthAccountRow[];
  keys?: ApiKeyRow[];
  accountLoadState?: AccountLoadState;
  switchingAccountId?: string | null;
  busy?: boolean;
  loginHint?: LoginHint | null;
  authHandlers?: ProviderAuthHandlers;
  onCodexActiveNeedsReauthChange?: (needs: boolean) => void;
  codexController?: CodexAccountPoolController;
  apiLane?: WorkspaceItem;
  oauthMethod: ReturnType<typeof deriveAccessDescriptor>["methods"][number] | undefined;
  keyMethod: ReturnType<typeof deriveAccessDescriptor>["methods"][number] | undefined;
  lane: AccessCredentialLane;
  onLane: (next: AccessCredentialLane) => void;
}) {
  const showOauth = Boolean(oauthMethod);
  const showKeys = Boolean(keyMethod && authHandlers);
  const activeLane = accessCredentialLane(showOauth, showKeys, lane);
  const switchLanes = accessShowsCredentialLaneSwitch(showOauth, showKeys);
  const tablesRef = useRef<HTMLDivElement>(null);
  const focusLaneTab = useRef(false);
  const selectLane = (next: AccessCredentialLane) => {
    focusLaneTab.current = true;
    onLane(next);
  };
  useLayoutEffect(() => {
    if (!focusLaneTab.current) return;
    focusLaneTab.current = false;
    tablesRef.current
      ?.querySelector<HTMLButtonElement>(".providers-block:not(.is-lane-inactive) .providers-access-lanes [aria-selected='true']")
      ?.focus();
  }, [activeLane]);
  if (!activeLane) return null;
  const laneHeading = () => (
    switchLanes ? <AccessCredentialLaneTabs t={t} lane={activeLane} onLane={selectLane} /> : undefined
  );
  return (
    <div
      ref={tablesRef}
      className={switchLanes ? "providers-access-tables providers-access-tables--switch" : "providers-access-tables"}
    >
      {showOauth && (
        <AccessOauthSection
          item={item}
          apiBase={apiBase}
          oauth={oauth}
          accounts={accounts}
          accountLoadState={accountLoadState}
          switchingAccountId={switchingAccountId}
          busy={busy}
          loginHint={loginHint}
          authHandlers={authHandlers}
          onCodexActiveNeedsReauthChange={onCodexActiveNeedsReauthChange}
          codexController={codexController}
          heading={laneHeading()}
          inactive={switchLanes && activeLane !== "oauth"}
        />
      )}
      {showKeys && keyMethod && authHandlers && (
        <AccessKeysSection
          item={item}
          apiBase={apiBase}
          keys={keys}
          authHandlers={authHandlers}
          apiLane={apiLane}
          connectionId={keyMethod.connectionId}
          heading={laneHeading()}
          inactive={switchLanes && activeLane !== "api-key"}
        />
      )}
    </div>
  );
}

function remapKeyHandlers(handlers: ProviderAuthHandlers, connectionId: string): ProviderAuthHandlers {
  return {
    ...handlers,
    onAddApiKey: (_name, key) => handlers.onAddApiKey(connectionId, key),
    onSwitchApiKey: (_name, entry) => handlers.onSwitchApiKey(connectionId, entry),
    onRemoveApiKey: (_name, entry) => handlers.onRemoveApiKey(connectionId, entry),
    onEditAlias: (_name, type, id, current) => handlers.onEditAlias(connectionId, type, id, current),
  };
}

function AccessOauthSection({
  item, apiBase, oauth, accounts, accountLoadState, switchingAccountId, busy, loginHint, authHandlers,
  onCodexActiveNeedsReauthChange, codexController, heading, inactive = false,
}: {
  item: WorkspaceItem;
  apiBase: string;
  oauth?: { loggedIn: boolean; email?: string; error?: string };
  accounts?: OAuthAccountRow[];
  accountLoadState?: AccountLoadState;
  switchingAccountId?: string | null;
  busy?: boolean;
  loginHint?: LoginHint | null;
  authHandlers?: ProviderAuthHandlers;
  onCodexActiveNeedsReauthChange?: (needs: boolean) => void;
  codexController?: CodexAccountPoolController;
  heading?: ReactNode;
  inactive?: boolean;
}) {
  const t = useT();
  const label = t("prov.access.oauthAccounts");
  const sectionProps = {
    id: "access-lane-oauth",
    className: inactive ? "providers-block is-lane-inactive" : "providers-block",
    "aria-label": label,
    "aria-hidden": inactive || undefined,
  };
  if (isAccountProvider(item.name, item)) {
    return (
      <section {...sectionProps}>
        <CodexAccountPool
          apiBase={apiBase}
          controller={codexController}
          accountModeState={item.codexAccountMode === "direct" ? "direct" : "pool"}
          onActiveNeedsReauthChange={onCodexActiveNeedsReauthChange}
          accessHeading={heading}
        />
      </section>
    );
  }
  if (!authHandlers) return null;
  return (
    <section {...sectionProps}>
      <div className="providers-block-head">
        {heading ?? <h4>{label}</h4>}
      </div>
      <ProviderAuthPanel
        item={item}
        apiBase={apiBase}
        oauth={oauth}
        accounts={accounts}
        keys={[]}
        accountLoadState={accountLoadState}
        switchingAccountId={switchingAccountId}
        busy={busy}
        loginHint={loginHint}
        authHandlers={authHandlers}
        forceSurface="oauth-accounts"
        omitChrome
      />
    </section>
  );
}

function AccessKeysSection({
  item, apiBase, keys, authHandlers, apiLane, connectionId, heading, inactive = false,
}: {
  item: WorkspaceItem;
  apiBase: string;
  keys?: ApiKeyRow[];
  authHandlers: ProviderAuthHandlers;
  apiLane?: WorkspaceItem;
  connectionId: string;
  heading?: ReactNode;
  inactive?: boolean;
}) {
  const t = useT();
  const keyItem = apiLane ? { ...item, ...apiLane, name: connectionId } : item;
  return (
    <section
      id="access-lane-keys"
      className={inactive ? "providers-block is-lane-inactive" : "providers-block"}
      aria-label={t("prov.access.apiKeys")}
      aria-hidden={inactive || undefined}
    >
      <ProviderAuthPanel
        item={keyItem}
        apiBase={apiBase}
        keys={keys}
        accountLoadState="ready"
        authHandlers={remapKeyHandlers(authHandlers, connectionId)}
        forceSurface="api-keys"
        omitChrome
        heading={heading}
      />
    </section>
  );
}

function AccessActivitySection({ recentEvents }: { recentEvents: WorkspaceEvent[] }) {
  const t = useT();
  const events = recentAccessEvents(recentEvents).map((event, index) => ({
    key: `${event.timestamp}:${event.type}:${index}`,
    at: relativeClock(event.timestamp),
    label: providerEventLabel(event, t),
    severity: event.severity,
  }));
  return (
    <section className="providers-block">
      <div className="providers-block-head">
        <h4>{t("prov.access.credentialActivity")}</h4>
        <button type="button" className="providers-link" onClick={() => navigateHash("logs")}>
          {t("prov.access.viewActivity")}
        </button>
      </div>
      {events.length === 0 ? (
        <p className="muted">{t("prov.access.noActivity")}</p>
      ) : (
        <ProviderEvents events={events} />
      )}
    </section>
  );
}

function DefaultAccessSection({
  t, item, methodId, apiAvailable, onUpdateProvider,
}: {
  t: TFn;
  item: WorkspaceItem;
  methodId: string;
  apiAvailable: boolean;
  onUpdateProvider?: (name: string, patch: ProviderUpdatePatch) => Promise<{ ok: boolean; error?: string }>;
}) {
  const [editing, setEditing] = useState(false);
  const [saving, setSaving] = useState(false);
  const label = methodId === "api" ? t("prov.access.openaiApi") : t("prov.access.chatgptPool");
  return (
    <section className="providers-block">
      <div className="providers-block-head">
        <h4>{t("prov.access.defaultAccess")}</h4>
        {onUpdateProvider && (
          <button
            type="button"
            className="providers-link providers-link--plain"
            disabled={saving}
            onClick={() => setEditing(open => !open)}
          >
            {t("prov.access.changeDefault")}
          </button>
        )}
      </div>
      <dl className="providers-facts">
        <div>
          <dt>{t("prov.access.currentDefault")}</dt>
          <dd>
            {editing ? (
              <select
                className="input"
                value={methodId === "api" ? "api" : "oauth"}
                disabled={saving}
                onChange={event => {
                  const next = event.target.value === "api" ? "api" : "oauth";
                  setSaving(true);
                  void onUpdateProvider?.(item.name, { defaultAccess: next }).finally(() => {
                    setSaving(false);
                    setEditing(false);
                  });
                }}
              >
                <option value="oauth">{t("prov.access.chatgptPool")}</option>
                <option value="api" disabled={!apiAvailable}>{t("prov.access.openaiApi")}</option>
              </select>
            ) : label}
          </dd>
        </div>
      </dl>
    </section>
  );
}

function CredentialSelectionSection({
  t, apiBase, item, selection, interactive, codexController, onUpdateProvider,
}: {
  t: TFn;
  apiBase: string;
  item: WorkspaceItem;
  selection: NonNullable<ReturnType<typeof deriveAccessDescriptor>["selection"]>;
  interactive: boolean;
  codexController?: CodexAccountPoolController;
  onUpdateProvider?: (name: string, patch: ProviderUpdatePatch) => Promise<{ ok: boolean; error?: string }>;
}) {
  const [editing, setEditing] = useState(false);
  const [modeSaving, setModeSaving] = useState(false);
  const autoSwitch = useCodexAutoSwitch(apiBase, {
    updated: t("codexAuth.autoSwitchUpdated"),
    updateFailed: t("codexAuth.autoSwitchUpdateFailed"),
    invalid: t("codexAuth.autoSwitchThresholdInvalid"),
  });
  const [strategy, setStrategy] = useState(selection.strategy ?? DEFAULT_ACCOUNT_POOL_STRATEGY);

  // Credential selection is the single auto-switch policy owner for the account provider:
  // it subscribes to the shared pool's reads rather than opening a second observer, and
  // seeds from the pool's last payload on a late mount.
  const { beginServerRead, acceptServerRead, rejectServerRead, hydrateServerValue } = autoSwitch;
  const { subscribeLoadObserver, readLastThreshold, readLastActive } = codexController ?? {};
  useEffect(() => {
    if (!subscribeLoadObserver) return;
    return subscribeLoadObserver({
      beginActiveRead: beginServerRead,
      acceptActiveRead: acceptServerRead,
      rejectActiveRead: rejectServerRead,
    });
  }, [subscribeLoadObserver, beginServerRead, acceptServerRead, rejectServerRead]);
  useEffect(() => {
    if (!readLastThreshold) return;
    const cached = readLastThreshold();
    if (cached !== undefined) hydrateServerValue(cached);
  }, [readLastThreshold, hydrateServerValue]);

  const facts = credentialSelectionFacts(selection);
  const display = credentialSelectionDisplay(facts, strategy, interactive);
  return (
    <section className="providers-block">
      <div className="providers-block-head">
        <h4>{t("prov.access.credentialSelection")}</h4>
        {interactive && onUpdateProvider && (
          <button type="button" className="providers-link" disabled={modeSaving} onClick={() => setEditing(open => !open)}>
            {t("prov.access.editSelection")}
          </button>
        )}
      </div>
      <CredentialSelectionFacts t={t} display={display} />
      {editing && interactive && onUpdateProvider && (
        <CredentialSelectionEdit
          t={t}
          apiBase={apiBase}
          poolMode={facts.poolMode}
          strategy={strategy}
          modeSaving={modeSaving}
          autoSwitch={autoSwitch}
          subscribeLoadObserver={subscribeLoadObserver}
          readLastActive={readLastActive}
          onStrategyResolved={setStrategy}
          onModeChange={next => {
            setModeSaving(true);
            void onUpdateProvider(item.name, { codexAccountMode: next }).finally(() => {
              setModeSaving(false);
              setEditing(false);
            });
          }}
        />
      )}
    </section>
  );
}

function CredentialSelectionFacts({
  t, display,
}: {
  t: TFn;
  display: ReturnType<typeof credentialSelectionDisplay>;
}) {
  const modeLabel = display.mode === "pool"
    ? t("prov.openaiModePool")
    : display.mode === "direct"
      ? t("prov.openaiModeDirect")
      : display.mode;
  return (
    <dl className="providers-facts">
      <div>
        <dt>{t("prov.access.mode")}</dt>
        <dd>{modeLabel}</dd>
      </div>
      <div>
        <dt>{t("prov.access.strategy")}</dt>
        <dd>{display.strategy}</dd>
      </div>
      <div>
        <dt>{t("prov.access.autoSwitch")}</dt>
        <dd>{display.autoSwitch}</dd>
      </div>
      <div>
        <dt>{t("prov.access.stickyLimit")}</dt>
        <dd>{display.sticky}</dd>
      </div>
    </dl>
  );
}

function CredentialSelectionEdit({
  t, apiBase, poolMode, strategy, modeSaving, autoSwitch, subscribeLoadObserver, readLastActive,
  onStrategyResolved, onModeChange,
}: {
  t: TFn;
  apiBase: string;
  poolMode: boolean;
  strategy: string;
  modeSaving: boolean;
  autoSwitch: ReturnType<typeof useCodexAutoSwitch>;
  subscribeLoadObserver?: (observer: CodexAccountLoadObserver) => () => void;
  readLastActive?: () => unknown;
  onStrategyResolved: (next: string) => void;
  onModeChange: (next: "direct" | "pool") => void;
}) {
  return (
    <div className="providers-access-selection-edit">
      <label>
        <span>{t("prov.access.mode")}</span>
        <select
          className="input"
          value={poolMode ? "pool" : "direct"}
          disabled={modeSaving}
          onChange={event => onModeChange(event.target.value === "direct" ? "direct" : "pool")}
        >
          <option value="pool">{t("prov.openaiModePool")}</option>
          <option value="direct">{t("prov.openaiModeDirect")}</option>
        </select>
      </label>
      {poolMode && (
        <>
          <CodexPoolStrategySetting
            apiBase={apiBase}
            subscribeLoadObserver={subscribeLoadObserver}
            readLastActive={readLastActive}
            onStrategyResolved={next => onStrategyResolved(next)}
          />
          {strategy === "quota" && (
            <CodexAutoSwitchSetting
              controller={autoSwitch}
              strategy={strategy}
            />
          )}
        </>
      )}
    </div>
  );
}

function relativeClock(ms: number): string {
  if (!ms || !Number.isFinite(ms)) return "—";
  const date = new Date(ms > 10_000_000_000 ? ms : ms * 1000);
  return new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit", hour12: false }).format(date);
}
