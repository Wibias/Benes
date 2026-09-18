/**
 * Providers consumes `#providers/<name>/access` once per appearance: a cold bookmark, a
 * refresh, or Back/Forward onto it. The token is what a consumer keys its effect on, so an
 * unrelated config reload cannot re-run the reveal.
 */
import { useEffect, useRef, useState } from "react";
import { normalizeHashPath } from "../hash-routing.ts";
import {
  nextProviderAccessTarget,
  type ProviderAccessDeepLinkState,
} from "../provider-workspace/provider-access-hash.ts";

const ROUTE_EVENTS = ["hashchange", "popstate"] as const;

export interface ProviderAccessDeepLink {
  provider: string;
  /** Increments per consumed deep link, so replaying one is observably a new request. */
  token: number;
}

export function useProviderAccessDeepLink(): ProviderAccessDeepLink | null {
  const [target, setTarget] = useState<ProviderAccessDeepLink | null>(null);
  const stateRef = useRef<ProviderAccessDeepLinkState>({ applied: null });
  const tokenRef = useRef(0);

  useEffect(() => {
    const apply = () => {
      const next = nextProviderAccessTarget(stateRef.current, normalizeHashPath(window.location.hash));
      stateRef.current = next.state;
      if (next.provider === null) return;
      tokenRef.current += 1;
      setTarget({ provider: next.provider, token: tokenRef.current });
    };
    apply();
    for (const event of ROUTE_EVENTS) window.addEventListener(event, apply);
    return () => {
      for (const event of ROUTE_EVENTS) window.removeEventListener(event, apply);
    };
  }, []);

  return target;
}
