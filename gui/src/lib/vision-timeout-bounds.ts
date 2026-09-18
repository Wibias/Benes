/**
 * Inclusive integer bounds for `visionSidecar.timeoutMs`.
 * The ceiling is the 32-bit timer delay used by `setTimeout`.
 */
export const DEFAULT_VISION_TIMEOUT_MS = 45_000;
export const MIN_VISION_TIMEOUT_MS = 1;
export const MAX_VISION_TIMEOUT_MS = 2_147_483_647;
