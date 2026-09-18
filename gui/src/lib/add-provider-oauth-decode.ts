/** Wire decode for add-provider OAuth start, status poll, and paste-code. */

export const UNKNOWN_OAUTH_PROVIDER_ERROR = "unknown oauth provider";

export type AddProviderOAuthStart =
  | { kind: "unknown-provider" }
  | { kind: "start-failed"; error?: string }
  | { kind: "opened"; url: string }
  | { kind: "instructions"; instructions?: string };

export function decodeAddProviderOAuthStart(
  ok: boolean,
  data: { error?: string; url?: string; instructions?: string },
): AddProviderOAuthStart {
  if (!ok) {
    return data.error === UNKNOWN_OAUTH_PROVIDER_ERROR
      ? { kind: "unknown-provider" }
      : { kind: "start-failed", error: data.error };
  }
  if (data.url) return { kind: "opened", url: data.url };
  return { kind: "instructions", instructions: data.instructions };
}

export type AddProviderOAuthPollTick =
  | { kind: "error"; error: string }
  | { kind: "logged-in" }
  | { kind: "continue" };

export function decodeAddProviderOAuthPollTick(
  status: { loggedIn?: boolean; error?: string; pending?: boolean } | null | undefined,
): AddProviderOAuthPollTick {
  if (status?.error) return { kind: "error", error: status.error };
  if (status?.pending) return { kind: "continue" };
  if (status?.loggedIn) return { kind: "logged-in" };
  return { kind: "continue" };
}

export function decodeAddProviderManualCodeFailure(
  data: { error?: string } | undefined,
  statusText: string,
): string {
  return data?.error || statusText;
}

export function isOAuthLoginBusyError(error: string | undefined): boolean {
  return typeof error === "string" && /login for .+ is already in progress/i.test(error);
}

/** True when Cancel (or a newer login) replaced the attempt that started this poll. */
export function addProviderOAuthAttemptStale(started: number, current: number): boolean {
  return started !== current;
}

export type OAuthPopup = {
  opened: boolean;
  navigate: (url: string) => void;
  close: () => void;
};

type OpenWindow = (url: string, target?: string, features?: string) => Window | null;

/** Open a blank window in the click stack, then navigate after POST /api/oauth/login returns a URL. */
export function createOAuthPopup(openWindow: OpenWindow = (url, target, features) => window.open(url, target, features)): OAuthPopup {
  let win: Window | null = null;
  try {
    win = openWindow("about:blank", "benes-oauth");
  } catch {
    win = null;
  }
  return {
    opened: Boolean(win),
    navigate(url: string) {
      if (!url.trim()) return;
      if (win && !win.closed) {
        try {
          win.location.href = url;
          return;
        } catch { /* fall through to a fresh tab */ }
        try {
          win.close();
        } catch { /* ignore */ }
        win = null;
      }
      try {
        win = openWindow(url, "_blank", "noopener,noreferrer");
      } catch { /* popup blocked; LoginUrlBlock is the fallback */ }
    },
    close() {
      try {
        win?.close();
      } catch { /* ignore */ }
      win = null;
    },
  };
}
