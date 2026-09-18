import { navigateHash, normalizeHashPath, parseHashRoute } from "../hash-routing.ts";

export type StorageTab = "overview" | "cleanup" | "quarantine";

export const STORAGE_TABS: readonly StorageTab[] = ["overview", "cleanup", "quarantine"];

export const STORAGE_TAB_HASHES = ["storage/cleanup", "storage/quarantine"] as const;

export function storageTabHash(tab: StorageTab): string {
  return tab === "overview" ? "storage" : `storage/${tab}`;
}

export function storageTabDomId(tab: StorageTab): string {
  return `storage-tab-${tab}`;
}

export function storagePanelDomId(tab: StorageTab): string {
  return `storage-panel-${tab}`;
}

export function readStorageTab(hash = typeof window !== "undefined" ? window.location.hash : ""): StorageTab {
  const raw = normalizeHashPath(hash);
  if (raw === "storage/cleanup") return "cleanup";
  if (raw === "storage/quarantine") return "quarantine";
  return "overview";
}

export function storageHashIsAllowed(path: string, query: URLSearchParams): boolean {
  if ([...query.keys()].length > 0) return false;
  return path === "storage" || (STORAGE_TAB_HASHES as readonly string[]).includes(path);
}

export function storageHashIsAllowedFromRaw(rawHash: string): boolean {
  const { path, query } = parseHashRoute(rawHash);
  return storageHashIsAllowed(path, query);
}

export function selectStorageTab(tab: StorageTab): void {
  navigateHash(storageTabHash(tab));
}
