/**
 * The dashboard shell around every board: the sidebar, its mobile topbar, and the two
 * listener orbs they share.
 *
 * The information architecture is declared once, as a flat list of destinations grouped into
 * labelled sections. Agent Fabric gates exactly one destination (Tasks), so the gate is
 * carried on that destination rather than as a second copy of the Observation list — the
 * previous shape kept two lists in step, which is how a gated destination quietly stops
 * tracking its neighbours.
 */
import type { ReactNode, RefObject } from "react";
import { SidebarGithubRow } from "./components/sidebar-github-row";
import { IconGlobe, IconMenu, IconMonitor, IconMoon, IconPower, IconRefresh, IconSun, IconX } from "./icons";
import { LOCALES, localeDisplayName, type Locale, type TFn, type TKey } from "./i18n/shared";
import { Select, type SelectOption } from "./ui";
import type { Page } from "./app-routing";
import { sidebarClassName, type AppTheme } from "./app-chrome-policy";

/** Features that can add or withhold a sidebar destination. */
type SidebarFeature = "fabric";

/** A place the sidebar can send the user. */
interface SidebarDestination {
  readonly id: Page;
  readonly tkey: TKey;
  /** Feature gate; absent means the destination is always offered. */
  readonly gate?: SidebarFeature;
}

/** A labelled block of destinations. The unlabelled block is the primary one. */
interface SidebarSection {
  readonly label?: TKey;
  readonly items: readonly SidebarDestination[];
}

/**
 * The sidebar's information architecture, in render order.
 *
 * Configuration holds the surfaces that change what Benes does; Observation holds what it
 * recorded; System holds the listener itself and the access it exposes.
 */
const SIDEBAR_NAV_SECTIONS: readonly SidebarSection[] = [
  { items: [{ id: "dashboard", tkey: "nav.dashboard" }] },
  {
    label: "nav.group.configuration",
    items: [
      { id: "harnesses", tkey: "nav.harnesses" },
      { id: "subagents", tkey: "nav.subagents" },
      { id: "routing", tkey: "nav.routing" },
      { id: "models", tkey: "nav.models" },
      { id: "providers", tkey: "nav.providers" },
    ],
  },
  {
    label: "nav.group.observation",
    items: [
      { id: "sessions", tkey: "nav.sessions" },
      { id: "usage", tkey: "nav.usage" },
      { id: "logs", tkey: "nav.diagnostics" },
      { id: "tasks", tkey: "nav.tasks", gate: "fabric" },
    ],
  },
  {
    label: "nav.group.system",
    items: [
      { id: "startup", tkey: "nav.control" },
      { id: "api", tkey: "nav.api" },
      { id: "storage", tkey: "nav.storage" },
    ],
  },
];

/** Which features are on. Every gate reads from here, so a gate cannot be half-applied. */
type SidebarFeatures = Readonly<Record<SidebarFeature, boolean>>;

function destinationIsOffered(destination: SidebarDestination, features: SidebarFeatures): boolean {
  if (destination.gate === undefined) return true;
  return features[destination.gate];
}

/** Catalogue keys for the theme display names. The cycling policy itself stays in `app-shell.ts`. */
const THEME_LABEL: Record<AppTheme, TKey> = {
  light: "theme.light",
  dark: "theme.dark",
  system: "theme.system",
};

/**
 * The locale choices and the Select's own style never change, so they are built once: a fresh
 * array or style object on every render would churn the Select's props for nothing.
 */
const LOCALE_OPTIONS: SelectOption[] = LOCALES.map(choice => ({
  value: choice.code,
  label: localeDisplayName(choice.code),
}));

const LOCALE_SELECT_STYLE = { flex: 1, minWidth: 0, width: "100%" } as const;

function SidebarNavLink({
  destination,
  active,
  t,
  onNavigate,
}: {
  destination: SidebarDestination;
  active: boolean;
  t: TFn;
  onNavigate: (id: Page) => void;
}) {
  return (
    <div className="nav-entry">
      <button
        type="button"
        className={active ? "nav-item active" : "nav-item"}
        data-page={destination.id}
        onClick={() => onNavigate(destination.id)}
        aria-current={active ? "page" : undefined}
      >
        {t(destination.tkey)}
      </button>
    </div>
  );
}

function SidebarNav({
  page,
  t,
  onNavigate,
  fabricEnabled,
}: {
  page: Page;
  t: TFn;
  onNavigate: (id: Page) => void;
  fabricEnabled: boolean;
}) {
  const features: SidebarFeatures = { fabric: fabricEnabled };
  return (
    <nav>
      {SIDEBAR_NAV_SECTIONS.map((section, index) => (
        <div key={section.label ?? `sidebar-section-${index}`} className="nav-group">
          {section.label !== undefined && <div className="nav-group-label">{t(section.label)}</div>}
          {section.items
            .filter(destination => destinationIsOffered(destination, features))
            .map(destination => (
              <SidebarNavLink
                key={destination.id}
                destination={destination}
                active={destination.id === page}
                t={t}
                onNavigate={onNavigate}
              />
            ))}
        </div>
      ))}
    </nav>
  );
}

/**
 * One circular chrome action. The accessible name doubles as the tooltip, so an icon-only
 * control is never left unlabelled; `danger` is the only visual difference between them.
 */
function ChromeOrb({
  icon,
  label,
  danger,
  disabled,
  onClick,
}: {
  icon: ReactNode;
  label: string;
  danger?: boolean;
  disabled?: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      className={danger === true ? "sidebar-orb sidebar-orb--danger" : "sidebar-orb"}
      onClick={onClick}
      disabled={disabled}
      aria-label={label}
      title={label}
    >
      {icon}
    </button>
  );
}

/** Stop and Codex restart, for whichever placeholder is showing them. */
function ListenerOrbs({
  t,
  stopping,
  onStop,
  codexRestarting,
  onCodexRestart,
}: {
  t: TFn;
  stopping: boolean;
  onStop: () => void;
  codexRestarting: boolean;
  /** May resolve a promise; the orb does not wait for it. */
  onCodexRestart: () => unknown;
}) {
  return (
    <>
      <ChromeOrb
        icon={<IconPower />}
        danger
        disabled={stopping}
        onClick={onStop}
        label={t(stopping ? "dash.stopping" : "dash.stop")}
      />
      <ChromeOrb
        icon={<IconRefresh />}
        disabled={codexRestarting}
        onClick={() => { void onCodexRestart(); }}
        label={t(codexRestarting ? "dash.codexRestarting" : "dash.codexRestart")}
      />
    </>
  );
}

/** Locale, theme, listener actions, and the GitHub/update row. */
function SidebarFoot({
  apiBase,
  t,
  locale,
  setLocale,
  theme,
  cycleTheme,
  stopping,
  handleStop,
  codexRestarting,
  handleCodexRestart,
}: {
  apiBase: string;
  t: TFn;
  locale: Locale;
  setLocale: (locale: Locale) => void;
  theme: AppTheme;
  cycleTheme: () => void;
  stopping: boolean;
  handleStop: () => void;
  codexRestarting: boolean;
  handleCodexRestart: () => unknown;
}) {
  const themeLabel = `${t("theme.label")}: ${t(THEME_LABEL[theme])}`;
  return (
    <div className="sidebar-foot">
      <div className="lang-toggle">
        <IconGlobe aria-hidden />
        <Select
          value={locale}
          options={LOCALE_OPTIONS}
          onChange={value => setLocale(value as Locale)}
          label={t("lang.label")}
          placement="right"
          portal={false}
          style={LOCALE_SELECT_STYLE}
        />
      </div>
      <button type="button" className="theme-toggle" onClick={cycleTheme} aria-label={themeLabel} title={themeLabel}>
        {theme === "light" ? <IconSun /> : theme === "dark" ? <IconMoon /> : <IconMonitor />}{" "}
        <span className="mode">{t(THEME_LABEL[theme])}</span>
      </button>
      <div className="sidebar-action-row">
        <span className="sidebar-action-label">{t("dash.actions")}</span>
        <div className="sidebar-action-orbs">
          <ListenerOrbs
            t={t}
            stopping={stopping}
            onStop={handleStop}
            codexRestarting={codexRestarting}
            onCodexRestart={handleCodexRestart}
          />
        </div>
      </div>
      <SidebarGithubRow apiBase={apiBase} />
    </div>
  );
}

export function AppSidebar({
  apiBase,
  navOpen,
  sidebarRef,
  brand,
  t,
  page,
  locale,
  setLocale,
  theme,
  cycleTheme,
  stopping,
  handleStop,
  codexRestarting,
  handleCodexRestart,
  onClose,
  onNavigate,
  fabricEnabled,
}: {
  apiBase: string;
  navOpen: boolean;
  sidebarRef: RefObject<HTMLElement | null>;
  brand: ReactNode;
  t: TFn;
  page: Page;
  locale: Locale;
  setLocale: (locale: Locale) => void;
  theme: AppTheme;
  cycleTheme: () => void;
  stopping: boolean;
  handleStop: () => void;
  codexRestarting: boolean;
  handleCodexRestart: () => unknown;
  onClose: () => void;
  onNavigate: (id: Page) => void;
  fabricEnabled: boolean;
}) {
  const closeLabel = t("nav.closeMenu");
  return (
    <aside id="app-sidebar" className={sidebarClassName(navOpen)} ref={sidebarRef} tabIndex={-1}>
      <div className="drawer-head">
        {brand}
        <button
          type="button"
          className="menu-toggle drawer-close"
          onClick={onClose}
          aria-label={closeLabel}
          title={closeLabel}
        >
          <IconX />
        </button>
      </div>
      <SidebarNav page={page} t={t} onNavigate={onNavigate} fabricEnabled={fabricEnabled} />
      <SidebarFoot
        apiBase={apiBase}
        t={t}
        locale={locale}
        setLocale={setLocale}
        theme={theme}
        cycleTheme={cycleTheme}
        stopping={stopping}
        handleStop={handleStop}
        codexRestarting={codexRestarting}
        handleCodexRestart={handleCodexRestart}
      />
    </aside>
  );
}

export function AppMobileTopbar({
  navOpen,
  brand,
  t,
  menuBtnRef,
  stopping,
  handleStop,
  codexRestarting,
  handleCodexRestart,
  onToggleNav,
}: {
  navOpen: boolean;
  brand: ReactNode;
  t: TFn;
  menuBtnRef: RefObject<HTMLButtonElement | null>;
  stopping: boolean;
  handleStop: () => void;
  codexRestarting: boolean;
  handleCodexRestart: () => unknown;
  onToggleNav: () => void;
}) {
  const menuLabel = t(navOpen ? "nav.closeMenu" : "nav.openMenu");
  return (
    <header className="mobile-topbar" inert={navOpen}>
      <button
        ref={menuBtnRef}
        type="button"
        className="menu-toggle"
        onClick={onToggleNav}
        aria-expanded={navOpen}
        aria-controls="app-sidebar"
        aria-label={menuLabel}
        title={menuLabel}
      >
        <IconMenu />
      </button>
      {brand}
      <div className="mobile-topbar-actions">
        <ListenerOrbs
          t={t}
          stopping={stopping}
          onStop={handleStop}
          codexRestarting={codexRestarting}
          onCodexRestart={handleCodexRestart}
        />
      </div>
    </header>
  );
}
