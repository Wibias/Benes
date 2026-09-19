/** Benes dashboard client for the Go proxy (`internal/server`). */
import type { ReactNode } from "react";
import { IconExternal, IconLink } from "../icons";
import { useT } from "../i18n/shared";
import { useCopyFeedback } from "./use-copy-feedback";
import type { CopyOutcome } from "../copy-feedback";

/** Class vocabulary for the surface, kept in one place. */
const SURFACE_CLASS = "login-url";
const VALUE_ROW_CLASS = "login-url-value-row";
const VALUE_CLASS = "login-url-value";
const COPY_BUTTON_CLASS = "btn btn-ghost btn-sm login-url-copy";
const MANUAL_LINK_CLASS = "login-url-manual";
/** Icon box for the two affordances. */
const GLYPH_WIDTH = 13;
const GLYPH_HEIGHT = 13;
const GLYPH_SIZE = { width: GLYPH_WIDTH, height: GLYPH_HEIGHT } as const;

/** Copy label per clipboard outcome; `null` is the idle label. */
const COPY_LABEL_KEYS = {
  idle: "prov.copyLink",
  copied: "prov.linkCopied",
  unavailable: "prov.linkCopyUnavailable",
} as const;

function copyLabelKey(outcome: CopyOutcome | null) {
  return COPY_LABEL_KEYS[outcome ?? "idle"];
}

function safeOAuthManualHref(raw: string): string | null {
  let parsed: URL;
  try {
    parsed = new URL(raw);
  } catch {
    return null;
  }

  if (parsed.username.length > 0 || parsed.password.length > 0) return null;
  if (parsed.protocol === "https:") return parsed.href;
  if (parsed.protocol !== "http:") return null;

  const host = parsed.hostname.replace(/^\[|\]$/g, "").toLowerCase();
  return host === "localhost" || host === "127.0.0.1" || host === "::1" ? parsed.href : null;
}

/**
 * External navigation for a URL the proxy could not open itself.
 *
 * `noopener` is spelled out even though `noreferrer` already implies it, so the
 * intent survives a future edit that drops `noreferrer`.
 */
function ManualOpenLink({
  href,
  icon,
  children,
}: {
  href: string;
  icon: ReactNode;
  children: ReactNode;
}) {
  return (
    <a href={href} target="_blank" rel="noopener noreferrer" className={MANUAL_LINK_CLASS}>
      {icon} {children}
    </a>
  );
}

/**
 * Recovery affordance for an OAuth waiting state: the proxy already tried to open the
 * browser server-side, so this surface only matters once that failed.
 *
 * The authorization URL is the value the user has to carry into a browser, so it owns
 * the row and the copy control sits on that row with it: the primary action is attached
 * to the data it copies, and a long query string wraps inside the value surface instead
 * of pushing the action out of view. Manual open stays a separate secondary line.
 *
 * The single owner for all three login surfaces.
 */
export function LoginUrlBlock({ url }: { url: string }) {
  const translate = useT();
  const feedback = useCopyFeedback<string>();
  if (url.length === 0) return null;
  const manualHref = safeOAuthManualHref(url);

  return (
    <div className={SURFACE_CLASS}>
      <div className={VALUE_ROW_CLASS}>
        <code className={VALUE_CLASS}>{url}</code>
        <button
          type="button"
          className={COPY_BUTTON_CLASS}
          onClick={() => feedback.copy(url, url)}
        >
          <IconLink style={GLYPH_SIZE} aria-hidden="true" />
          <span aria-live="polite">{translate(copyLabelKey(feedback.outcomeFor(url)))}</span>
        </button>
      </div>
      {manualHref && (
        <ManualOpenLink href={manualHref} icon={<IconExternal style={GLYPH_SIZE} aria-hidden="true" />}>
          {translate("prov.didntOpen")}
        </ManualOpenLink>
      )}
    </div>
  );
}
