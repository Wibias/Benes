/** Save / retry commands for the Subagents Ultra-mode board. */
import type { TFn } from "../i18n/shared.ts";
import type { UltraModePatch } from "./subagents-delegation-contract.ts";
import { putUltraModePatch, ultraSaveToastKey } from "./subagents-ultra-mode.ts";

export async function runUltraModeSave(options: {
  apiBase: string;
  patch: UltraModePatch;
  t: TFn;
  boundApiBase: string;
  loadUltraMode: () => Promise<boolean>;
  showToast: (ok: boolean, message: string) => void;
  setStatus: (value: string) => void;
  begin: () => void;
  end: () => void;
}): Promise<void> {
  options.begin();
  options.setStatus("");
  try {
    await putUltraModePatch(options.apiBase, options.patch, options.t("sub.ultraModeSaveFail"));
    if (options.boundApiBase !== options.apiBase || !await options.loadUltraMode()) return;
    options.showToast(true, options.t(ultraSaveToastKey(options.patch)));
  } catch (error) {
    if (options.boundApiBase !== options.apiBase) return;
    options.showToast(
      false,
      error instanceof Error && error.message ? error.message : options.t("sub.networkError"),
    );
  } finally {
    options.end();
  }
}

export async function runUltraModeRetry(options: {
  t: TFn;
  loadUltraMode: () => Promise<boolean>;
  showToast: (ok: boolean, message: string) => void;
  setStatus: (value: string | ((current: string) => string)) => void;
  markFailed: () => void;
}): Promise<void> {
  try {
    if (!await options.loadUltraMode()) return;
    options.setStatus((current) => (current === options.t("sub.ultraModeLoadFail") ? "" : current));
  } catch {
    options.markFailed();
    options.showToast(false, options.t("sub.ultraModeLoadFail"));
  }
}
