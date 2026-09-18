/**
 * Dashboard redaction of account identity.
 *
 * The Access tables list credentials Benes already holds, so an operator has to be able
 * to tell two accounts apart; the identifier itself must not survive the trip to the
 * screen. Every helper below is therefore written so that the returned string is built
 * out of a bounded slice of the input. No branch hands the caller back what it was given.
 */

/** Replaces any account identifier withheld in full. Contains no part of the input. */
export const ACCOUNT_PLACEHOLDER = "account-…";

/** How many trailing characters of an account id stay legible. */
const LEGIBLE_ACCOUNT_CHARS = 4;

/** How many leading characters of an email local part stay legible. */
const LEGIBLE_LOCAL_CHARS = 1;

/** The one login value that carries no identity, so it is reported as unmaskable. */
const PLACEHOLDER_LOGIN = "Codex App login";

/** Shared first step: absent and whitespace-only candidates are the same non-answer. */
function candidateText(value: string | null | undefined): string {
  if (value === null || value === undefined) return "";
  return value.trim();
}

/**
 * `account-…` or `account-…1234`.
 *
 * The cut is a suffix on purpose: an account id's tail is what distinguishes two rows,
 * while a prefix would leak the provider's own naming scheme. Anything at or below the
 * legible width would be reproduced in full by a suffix, so it degrades to the
 * placeholder instead.
 */
export function maskAccountId(value: string | null | undefined): string | null {
  const text = candidateText(value);
  if (text === "") return null;
  if (text.length <= LEGIBLE_ACCOUNT_CHARS) return ACCOUNT_PLACEHOLDER;
  return ACCOUNT_PLACEHOLDER + text.slice(-LEGIBLE_ACCOUNT_CHARS);
}

/**
 * Label form. Called with whatever the config carried, including `null`, so it can never
 * answer with the raw id — the worst case is the placeholder.
 */
export function displayAccountId(value: string | null | undefined): string {
  const masked = maskAccountId(value);
  return masked === null ? ACCOUNT_PLACEHOLDER : masked;
}

/**
 * `j***@gmail.com`.
 *
 * The domain is kept because it is the operator-relevant half (work vs. personal), and
 * exactly one local character is kept so the row is not anonymous. The value has to look
 * like an address before any of it is echoed: a lone `@`, an empty local part, and a
 * domain with no dot are all rejected outright rather than passed through in a partial
 * form.
 */
export function maskEmailAddress(value: string | null | undefined): string | null {
  const text = candidateText(value);
  if (text === "" || text === PLACEHOLDER_LOGIN) return null;

  const separator = text.lastIndexOf("@");
  if (separator < LEGIBLE_LOCAL_CHARS || separator === text.length - 1) return null;

  const local = text.slice(0, separator);
  const domain = text.slice(separator + 1);
  if (!domain.includes(".")) return null;

  return `${local.slice(0, LEGIBLE_LOCAL_CHARS)}***@${domain}`;
}
