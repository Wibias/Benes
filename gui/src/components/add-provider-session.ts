/**
 * Add Provider session: catalog selection, save, authorization wait, and paste-code.
 */
import type { CatalogPreset } from "./provider-catalog/provider-presets";
import type { ProviderPayloadForm } from "../provider-workspace/provider-create-request";

type Preset = CatalogPreset;
type Draft = ProviderPayloadForm;

export type AddProviderSession = {
  selection: {
    preset: Preset | null;
    form: Draft | null;
    endpointChoice: string;
  };
  save: {
    inFlight: boolean;
    error: string;
  };
  oauth: {
    busy: boolean;
    notice: string;
    noticeTone: "ok" | "warn";
    authorizeUrl: string;
    authorizeFor: string | null;
    tosFor: string | null;
  };
  manual: {
    value: string;
    inFlight: boolean;
    notice: string;
    accepted: boolean;
  };
};

export type AddProviderCommand =
  | { op: "selectPreset"; preset: Preset; form: Draft; endpointChoice: string }
  | { op: "returnToCatalog" }
  | { op: "updateDraft"; form: Draft }
  | { op: "chooseEndpoint"; choice: string }
  | { op: "startSaving" }
  | { op: "finishSaving"; error?: string }
  | { op: "setSaveError"; error: string }
  | { op: "startAuthorization"; busy: boolean }
  | { op: "setAuthorizationNotice"; notice: string; tone?: "ok" | "warn" }
  | { op: "setAuthorizationTone"; tone: "ok" | "warn" }
  | { op: "receiveAuthorizationUrl"; url: string; providerId: string }
  | { op: "updateManualCode"; code: string }
  | { op: "setManualCodeBusy"; busy: boolean }
  | { op: "setManualCodeNotice"; notice: string; ok?: boolean }
  | { op: "resolveTos"; providerId: string | null }
  | { op: "switchToOauth"; form: Draft }
  | { op: "switchToApiKey"; form: Draft }
  | { op: "cancelAuthorization" };

function customDraft(label: string): { preset: Preset; form: Draft } {
  return {
    preset: {
      id: "custom",
      label,
      adapter: "openai-chat",
      baseUrl: "",
      auth: "key",
    },
    form: {
      name: "",
      adapter: "openai-chat",
      baseUrl: "",
      authMode: "key",
      apiKey: "",
      apiKeyTransport: undefined,
      defaultModel: "",
      allowPrivateNetwork: false,
    },
  };
}

function clearAuthorization(tosFor: string | null = null): AddProviderSession["oauth"] {
  return {
    busy: false,
    notice: "",
    noticeTone: "ok",
    authorizeUrl: "",
    authorizeFor: null,
    tosFor,
  };
}

function clearManual(inFlight: boolean): AddProviderSession["manual"] {
  return { value: "", inFlight, notice: "", accepted: true };
}

export function createAddProviderSession(initialCustom: boolean, customLabel: string): AddProviderSession {
  const draft = initialCustom ? customDraft(customLabel) : { preset: null, form: null };
  return {
    selection: { preset: draft.preset, form: draft.form, endpointChoice: "custom" },
    save: { inFlight: false, error: "" },
    oauth: clearAuthorization(),
    manual: clearManual(false),
  };
}

export function applyAddProviderCommand(
  session: AddProviderSession,
  command: AddProviderCommand,
): AddProviderSession {
  switch (command.op) {
    case "selectPreset":
      return {
        ...session,
        selection: { preset: command.preset, form: command.form, endpointChoice: command.endpointChoice },
        save: { ...session.save, error: "" },
        oauth: clearAuthorization(session.oauth.tosFor),
        manual: clearManual(session.manual.inFlight),
      };
    case "returnToCatalog":
      return {
        ...session,
        selection: { preset: null, form: null, endpointChoice: "custom" },
        save: { ...session.save, error: "" },
        oauth: clearAuthorization(session.oauth.tosFor),
        manual: clearManual(session.manual.inFlight),
      };
    case "updateDraft":
      return { ...session, selection: { ...session.selection, form: command.form } };
    case "chooseEndpoint":
      return { ...session, selection: { ...session.selection, endpointChoice: command.choice } };
    case "startSaving":
      return { ...session, save: { ...session.save, inFlight: true } };
    case "finishSaving":
      return { ...session, save: { inFlight: false, error: command.error ?? session.save.error } };
    case "setSaveError":
      return { ...session, save: { ...session.save, error: command.error } };
    case "startAuthorization":
      return { ...session, oauth: { ...session.oauth, busy: command.busy } };
    case "setAuthorizationNotice":
      return {
        ...session,
        oauth: {
          ...session.oauth,
          notice: command.notice,
          noticeTone: command.tone ?? session.oauth.noticeTone,
        },
      };
    case "setAuthorizationTone":
      return { ...session, oauth: { ...session.oauth, noticeTone: command.tone } };
    case "receiveAuthorizationUrl":
      if (session.selection.preset?.oauthProvider !== command.providerId) return session;
      return {
        ...session,
        oauth: { ...session.oauth, authorizeUrl: command.url, authorizeFor: command.providerId },
      };
    case "updateManualCode":
      return { ...session, manual: { ...session.manual, value: command.code } };
    case "setManualCodeBusy":
      return { ...session, manual: { ...session.manual, inFlight: command.busy } };
    case "setManualCodeNotice":
      return {
        ...session,
        manual: {
          ...session.manual,
          notice: command.notice,
          accepted: command.ok ?? session.manual.accepted,
        },
      };
    case "resolveTos":
      return { ...session, oauth: { ...session.oauth, tosFor: command.providerId } };
    case "switchToOauth":
      return {
        ...session,
        selection: { ...session.selection, form: command.form },
        save: { ...session.save, error: "" },
        oauth: { ...session.oauth, authorizeUrl: "", authorizeFor: null },
      };
    case "switchToApiKey":
      return {
        ...session,
        selection: { ...session.selection, form: command.form },
        oauth: clearAuthorization(session.oauth.tosFor),
        manual: clearManual(session.manual.inFlight),
      };
    case "cancelAuthorization":
      return {
        ...session,
        oauth: clearAuthorization(session.oauth.tosFor),
        manual: clearManual(session.manual.inFlight),
      };
    default:
      return session;
  }
}
