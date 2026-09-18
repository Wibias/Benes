/** Sessions hash/query bridge for Routing (`GET /api/sessions?policy=` / `?combo=`). */

function sessionsPathAndQuery(hash: string): { path: string; query: URLSearchParams } {
  const raw = hash.replace(/^#\/?/, "");
  const q = raw.indexOf("?");
  if (q < 0) return { path: raw, query: new URLSearchParams() };
  return { path: raw.slice(0, q), query: new URLSearchParams(raw.slice(q + 1)) };
}

export function sessionsPolicyFromHash(hash: string): string {
  const { path, query } = sessionsPathAndQuery(hash);
  if (path !== "sessions") return "";
  return query.get("policy")?.trim() ?? "";
}

export function sessionsComboFromHash(hash: string): string {
  const { path, query } = sessionsPathAndQuery(hash);
  if (path !== "sessions") return "";
  return query.get("combo")?.trim() ?? "";
}

export function sessionsHashForPolicy(policyId: string): string {
  const params = new URLSearchParams();
  params.set("policy", policyId.trim());
  return ["sessions", params.toString()].join("?");
}

export function sessionsHashForCombo(comboId: string): string {
  const params = new URLSearchParams();
  params.set("combo", comboId.trim());
  return ["sessions", params.toString()].join("?");
}

export function sessionsHashIsAllowed(path: string, query: URLSearchParams): boolean {
  if (path !== "sessions") return false;
  const keys = [...query.keys()];
  if (keys.length === 0) return true;
  if (keys.length > 1) return false;
  const key = keys[0];
  if (key !== "policy" && key !== "combo") return false;
  return (query.get(key)?.trim() ?? "") !== "";
}
