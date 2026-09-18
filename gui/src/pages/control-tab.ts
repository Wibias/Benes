/**
 * Compatibility facade for the former Control-owned Routing tab module.
 *
 * Production Routing state now lives in `routing-tab.ts`; keep this tiny re-export only
 * while older tests/imports migrate. No Control page code should import this module.
 */
export * from "./routing-tab";
