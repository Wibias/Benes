/** Benes dashboard client for the Go proxy (`internal/server`). */
import type { TFn } from "../i18n/shared";
import { CUSTOM_OPTION, THREAD_OPTION_SET } from "./models-shared";

export function v2ThreadsSelectValue(
  showThreadsCustom: boolean,
  maxConcurrent: number | null | undefined,
): string {
  if (showThreadsCustom) return CUSTOM_OPTION;
  if (maxConcurrent === null || maxConcurrent === undefined) return "";
  return THREAD_OPTION_SET.has(maxConcurrent) ? String(maxConcurrent) : CUSTOM_OPTION;
}

export function v2ThreadsSelectOptions(
  t: TFn,
  maxConcurrent: number | null | undefined,
  showThreadsCustom: boolean,
  threadOptions: readonly number[],
): Array<{ value: string; label: string }> {
  const missing = maxConcurrent === null || maxConcurrent === undefined;
  const extraCustom = !missing && !THREAD_OPTION_SET.has(maxConcurrent) && !showThreadsCustom;
  return [
    ...(missing ? [{ value: "", label: t("models.v2ThreadsDefault") }] : []),
    ...(extraCustom ? [{ value: CUSTOM_OPTION, label: String(maxConcurrent) }] : []),
    ...threadOptions.map(value => ({ value: String(value), label: String(value) })),
    { value: CUSTOM_OPTION, label: t("models.custom") },
  ];
}
