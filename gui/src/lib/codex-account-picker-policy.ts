/** Decode, copy, and transition policy for the Codex account-picker setting. */

/**
 * The card's whole decision surface: how a response is decoded, what the copy should say,
 * and how the card moves between reads, saves, and failures. Keeping the transitions here
 * rather than in the component is what lets the card stay a render layer, and it makes the
 * read/save ordering testable without a DOM.
 */

export function decodeAccountPickerEnabled(payload: unknown): boolean | null {
  if (!payload || typeof payload !== "object" || Array.isArray(payload)) return null;
  const value = (payload as { codexAccountPickerEnabled?: unknown }).codexAccountPickerEnabled;
  return typeof value === "boolean" ? value : null;
}

export type AccountPickerSaveDecode =
  | { ok: false }
  | { ok: true; enabled: boolean; catalogRefreshPending: boolean };

/** A save is only confirmed by a body that says so and repeats the stored boolean. */
export function decodeAccountPickerSave(payload: unknown): AccountPickerSaveDecode {
  if (!payload || typeof payload !== "object" || Array.isArray(payload)) return { ok: false };
  const view = payload as {
    ok?: unknown;
    codexAccountPickerEnabled?: unknown;
    catalogRefreshPending?: unknown;
  };
  if (view.ok !== true || typeof view.codexAccountPickerEnabled !== "boolean") return { ok: false };
  return {
    ok: true,
    enabled: view.codexAccountPickerEnabled,
    catalogRefreshPending: view.catalogRefreshPending === true,
  };
}

export type AccountPickerCopyKind = "load-failed" | "loading" | "on" | "off";

export function accountPickerCopyKind(input: {
  loadError: boolean;
  hydrated: boolean;
  enabled: boolean;
}): AccountPickerCopyKind {
  if (input.loadError && !input.hydrated) return "load-failed";
  if (!input.hydrated) return "loading";
  return input.enabled ? "on" : "off";
}

export function accountPickerShowsCompatibility(hydrated: boolean, enabled: boolean): boolean {
  return hydrated && enabled;
}

export function accountPickerShowsRefreshFailed(hydrated: boolean, loadError: boolean): boolean {
  return hydrated && loadError;
}

export function accountPickerSaveFeedbackKind(
  catalogRefreshPending: boolean,
): "pending" | "updated" {
  return catalogRefreshPending ? "pending" : "updated";
}

export function accountPickerInitialLoadFailed(loadError: boolean, hydrated: boolean): boolean {
  return loadError && !hydrated;
}

/** What the card has to say after a save, before any copy is attached to it. */
export type AccountPickerFeedbackKind = "updated" | "refresh-pending" | "update-failed";

/** Everything the card paints, in one record so a read and a save cannot half-apply. */
export interface AccountPickerState {
  /** The value the listener holds, optimistically the requested one while a save is out. */
  enabled: boolean;
  /** False until a read or a save confirms something; the toggle stays hidden until then. */
  hydrated: boolean;
  saving: boolean;
  /** Raised only by a cold read failure, so a later miss leaves the controls usable. */
  loadError: boolean;
  feedback: AccountPickerFeedbackKind | null;
  /** Monotonic revision shared by reads and saves; the ordering rule for both. */
  generation: number;
  /** The value a rejected save restores. */
  rollbackTo: boolean;
}

export function initialAccountPickerState(): AccountPickerState {
  return {
    enabled: false,
    hydrated: false,
    saving: false,
    loadError: false,
    feedback: null,
    generation: 0,
    rollbackTo: false,
  };
}

export type AccountPickerEvent =
  | { kind: "read-began"; generation: number }
  | { kind: "read-arrived"; generation: number; enabled: boolean }
  | { kind: "read-failed"; generation: number }
  | { kind: "save-began"; generation: number; enabled: boolean }
  | { kind: "save-arrived"; enabled: boolean; catalogRefreshPending: boolean }
  | { kind: "save-failed" };

/**
 * One revision counter orders every read against every save.
 *
 * A read carries the revision it started at and is dropped unless that is still current,
 * so a GET issued before a save cannot land afterwards and undo what the listener
 * confirmed. A save claims the next revision when it starts, which is also what retires
 * the read that was already in flight.
 */
export function accountPickerReducer(
  state: AccountPickerState,
  event: AccountPickerEvent,
): AccountPickerState {
  switch (event.kind) {
    case "read-began":
      return { ...state, generation: event.generation };

    case "read-arrived": {
      if (event.generation !== state.generation) return state;
      return { ...state, enabled: event.enabled, hydrated: true, loadError: false };
    }

    case "read-failed": {
      if (event.generation !== state.generation) return state;
      // Only the cold path is something the operator can act on. Once a value has been
      // confirmed, a later miss leaves the working controls in place.
      return state.hydrated ? state : { ...state, loadError: true };
    }

    case "save-began": {
      if (state.saving) return state;
      // The optimistic paint is what makes the toggle feel instant; the recorded fallback
      // is the value the listener still holds if the write turns out to be refused.
      return {
        ...state,
        generation: event.generation,
        saving: true,
        enabled: event.enabled,
        rollbackTo: state.enabled,
        feedback: null,
      };
    }

    case "save-arrived":
      return {
        ...state,
        enabled: event.enabled,
        hydrated: true,
        loadError: false,
        saving: false,
        feedback: accountPickerSaveFeedbackKind(event.catalogRefreshPending) === "pending"
          ? "refresh-pending"
          : "updated",
      };

    case "save-failed":
      return { ...state, enabled: state.rollbackTo, saving: false, feedback: "update-failed" };

    default:
      return state;
  }
}
