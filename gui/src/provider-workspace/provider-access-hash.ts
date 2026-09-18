/**
 * `#providers/<name>/access` — the canonical Providers deep link.
 *
 * The retired standalone Codex Auth page redirects here, so this is a real route the
 * Provider workspace has to accept: normalized away, it would be stripped before
 * Providers could read it.
 */
import { parseHashRoute } from "../hash-routing.ts";

const PROVIDERS_SEGMENT = "providers";
const ACCESS_SEGMENT = "access";
/** The retired standalone page. Its bookmarks resolve to the tab below. */
const RETIRED_CODEX_AUTH_PAGE = "codex-auth";

/** The OpenAI account provider the retired Codex Auth page used to show. */
export const CODEX_ACCOUNT_PROVIDER = "openai";

export function providerAccessHash(providerName: string): string {
  return `${PROVIDERS_SEGMENT}/${providerName}/${ACCESS_SEGMENT}`;
}

/** The provider named by `#providers/<name>/access`, or null for any other hash. */
export function readProviderAccessHash(rawHash: string): string | null {
  const { path, query } = parseHashRoute(rawHash);
  if ([...query.keys()].length > 0) return null;
  const segments = path.split("/");
  if (segments.length !== 3) return null;
  if (segments[0] !== PROVIDERS_SEGMENT || segments[2] !== ACCESS_SEGMENT) return null;
  const name = segments[1] ?? "";
  return name.length > 0 ? name : null;
}

/** The OpenAI provider's Access tab: where the retired `#codex-auth` bookmark lands. */
export const OPENAI_ACCOUNT_ACCESS_HASH = providerAccessHash(CODEX_ACCOUNT_PROVIDER);

/**
 * Apply-once bookkeeping for the deep link: a given hash is consumed the first time it
 * appears and re-armed once the location moves away from it. Back/Forward replay it,
 * while an unrelated re-render or a config reload does not.
 */
export interface ProviderAccessDeepLinkState {
  applied: string | null;
}

/**
 * The canonical hash a location resolves to for this deep link, or null when it is not one.
 *
 * The retired page is resolved here as well as in the router because the router rewrites it
 * with `replaceState`, which emits no `hashchange`: on a cold bookmark the rewrite lands after
 * this reader has already looked. Resolving both spellings to one key also keeps the rewrite
 * from being read as a second deep link.
 */
export function resolveProviderAccessHash(rawHash: string): string | null {
  if (rawHash === RETIRED_CODEX_AUTH_PAGE || rawHash.startsWith(`${RETIRED_CODEX_AUTH_PAGE}/`)) {
    return OPENAI_ACCOUNT_ACCESS_HASH;
  }
  return readProviderAccessHash(rawHash) === null ? null : rawHash;
}

export function nextProviderAccessTarget(
  state: ProviderAccessDeepLinkState,
  rawHash: string,
): { state: ProviderAccessDeepLinkState; provider: string | null } {
  const target = resolveProviderAccessHash(rawHash);
  if (target === null) return { state: { applied: null }, provider: null };
  if (state.applied === target) return { state, provider: null };
  return { state: { applied: target }, provider: readProviderAccessHash(target) };
}
