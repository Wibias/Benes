/** Benes dashboard client for the Go proxy (`internal/server`). */

/** Privacy-safe completion state shared by account mutation UI flows. */
export interface CodexAccountMutationCompletion {
  catalogRefreshPending: boolean;
}

/**
 * Project only the public completion flag from an account mutation response.
 *
 * The flag is read through its own property descriptor so an inherited or
 * prototype-supplied `catalogRefreshPending` cannot masquerade as a server signal.
 * Any body that is not a plain object — an error page, a bare token — reports nothing.
 */
export function codexAccountMutationCompletion(value: unknown): CodexAccountMutationCompletion {
  const pending = typeof value === "object"
    && value !== null
    && !Array.isArray(value)
    && Object.getOwnPropertyDescriptor(value, "catalogRefreshPending")?.value === true;
  return { catalogRefreshPending: pending };
}
