/** Benes dashboard client for the Go proxy (`internal/server`). */

/** Which surface the add-account dialog is showing. */
export type AddAccountPhase = "choosing" | "authorizing";

/** Where manual redirect-code entry stands inside the authorizing phase. */
export type ManualCodeStatus = "idle" | "submitting" | "awaiting";

export type NoticeTone = "ok" | "warn";

export interface AddAccountNotice {
  message: string;
  tone: NoticeTone;
}

/**
 * The dialog is a two-phase machine. `choosing` and `authorizing` are separate
 * variants, so an authorization URL, a flow id, or an in-flight manual code can
 * never exist while the operator is still picking a method, and cancelling a flow
 * cannot leave a half-open authorization behind.
 */
export type AddAccountState =
  | {
      phase: "choosing";
      accountId: string;
      error: string | null;
    }
  | {
      phase: "authorizing";
      accountId: string;
      error: string | null;
      flowId: string | null;
      authUrl: string;
      manualCode: string;
      manualCodeStatus: ManualCodeStatus;
      notice: AddAccountNotice | null;
    };

export type AddAccountEvent =
  | { kind: "account-id-changed"; accountId: string }
  | { kind: "authorization-requested" }
  | { kind: "authorization-opened"; flowId: string | null; authUrl: string }
  | { kind: "authorization-failed"; error: string }
  | { kind: "authorization-closed" }
  | { kind: "returned-to-choosing" }
  | { kind: "manual-code-changed"; manualCode: string }
  | { kind: "manual-code-submitted" }
  | { kind: "manual-code-awaiting" }
  | { kind: "manual-code-submit-failed"; error: string }
  | { kind: "manual-code-reset" }
  | { kind: "notice-raised"; notice: AddAccountNotice }
  | { kind: "notice-cleared" };

const EMPTY_AUTHORIZATION = {
  flowId: null,
  authUrl: "",
  manualCode: "",
  manualCodeStatus: "idle",
  notice: null,
} as const;

/** Starting state: a re-authentication opens straight into the authorizing phase. */
export function initialAddAccountState(reauthAccountId?: string): AddAccountState {
  if (reauthAccountId === undefined || reauthAccountId === "") {
    return { phase: "choosing", accountId: "", error: null };
  }
  return { phase: "authorizing", accountId: "", error: null, ...EMPTY_AUTHORIZATION };
}

function authorizedFields(state: AddAccountState) {
  return state.phase === "authorizing" ? state : null;
}

export function addAccountReducer(state: AddAccountState, event: AddAccountEvent): AddAccountState {
  switch (event.kind) {
    case "account-id-changed":
      return state.accountId === event.accountId ? state : { ...state, accountId: event.accountId };

    case "authorization-requested": {
      const fields = authorizedFields(state);
      if (fields === null) return state;
      return { ...fields, error: null, ...EMPTY_AUTHORIZATION };
    }

    case "authorization-opened": {
      if (state.phase === "choosing") {
        return {
          phase: "authorizing",
          accountId: state.accountId,
          error: null,
          flowId: event.flowId,
          authUrl: event.authUrl,
          manualCode: "",
          manualCodeStatus: "idle",
          notice: null,
        };
      }
      return { ...state, flowId: event.flowId, authUrl: event.authUrl, error: null };
    }

    case "authorization-failed":
      return { ...state, error: event.error };

    case "authorization-closed": {
      const fields = authorizedFields(state);
      if (fields === null) return state;
      return { ...fields, flowId: null, authUrl: "" };
    }

    case "returned-to-choosing":
      return { phase: "choosing", accountId: state.accountId, error: state.error };

    case "manual-code-changed": {
      const fields = authorizedFields(state);
      if (fields === null) return state;
      return { ...fields, manualCode: event.manualCode };
    }

    case "manual-code-submitted": {
      const fields = authorizedFields(state);
      if (fields === null) return state;
      return { ...fields, manualCodeStatus: "submitting", notice: null, error: null };
    }

    case "manual-code-awaiting": {
      const fields = authorizedFields(state);
      if (fields === null) return state;
      return { ...fields, manualCode: "", manualCodeStatus: "awaiting", notice: null, error: null };
    }

    case "manual-code-submit-failed": {
      const fields = authorizedFields(state);
      if (fields === null) return state;
      return { ...fields, manualCodeStatus: "idle", error: event.error };
    }

    case "manual-code-reset": {
      const fields = authorizedFields(state);
      if (fields === null) return state;
      return { ...fields, manualCode: "", manualCodeStatus: "idle", notice: null };
    }

    case "notice-raised": {
      const fields = authorizedFields(state);
      if (fields === null) return state;
      return { ...fields, notice: event.notice };
    }

    case "notice-cleared": {
      const fields = authorizedFields(state);
      if (fields === null) return state;
      return { ...fields, notice: null };
    }

    default:
      return state;
  }
}
