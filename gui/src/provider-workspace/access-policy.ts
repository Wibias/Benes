import type { ProviderAccessDescriptor } from "./auth";

export interface AccessPolicyItem {
  name: string;
  authMode?: string;
  hasApiKey?: boolean;
  keyOptional?: boolean;
  codexAccountMode?: "direct" | "pool";
  defaultAccess?: "oauth" | "api";
  access?: ProviderAccessDescriptor;
}

export interface AccessPolicyContext {
  accountProvider: boolean;
  localProvider: boolean;
  apiLanePresent: boolean;
}

function noAccess(): ProviderAccessDescriptor {
  return { methods: [], activitySupported: false };
}

function accountAccessOverlay(
  item: AccessPolicyItem,
  access: ProviderAccessDescriptor,
  apiLanePresent: boolean,
): ProviderAccessDescriptor {
  const mode = item.codexAccountMode === "direct" ? "direct" : access.selection?.mode;
  return {
    ...access,
    methods: access.methods.map(method => method.kind === "api-key" && apiLanePresent
      ? { ...method, connectionPresent: true }
      : method),
    ...(access.selection
      ? { selection: { ...access.selection, ...(mode ? { mode } : {}) } }
      : {}),
  };
}

function defaultAccountAccess(item: AccessPolicyItem, apiLanePresent: boolean): ProviderAccessDescriptor {
  return {
    methods: [
      {
        id: "oauth",
        kind: "oauth",
        connectionId: "openai",
        supportsMultiple: true,
        quotaAvailable: true,
        selectionOrder: true,
        poolSupported: true,
        connectionPresent: true,
      },
      {
        id: "api",
        kind: "api-key",
        connectionId: "openai-apikey",
        supportsMultiple: true,
        poolSupported: true,
        connectionPresent: apiLanePresent,
      },
    ],
    defaultMethodId: item.defaultAccess === "api" ? "api" : "oauth",
    defaultAccess: true,
    selection: {
      supported: true,
      mode: item.codexAccountMode === "direct" ? "direct" : "pool",
      strategy: "quota",
      showAutoSwitch: true,
      methodId: "oauth",
    },
    activitySupported: true,
  };
}

function oauthAccess(name: string): ProviderAccessDescriptor {
  return {
    methods: [{
      id: "oauth",
      kind: "oauth",
      connectionId: name,
      supportsMultiple: true,
      connectionPresent: true,
    }],
    defaultMethodId: "oauth",
    activitySupported: true,
  };
}

function apiKeyAccess(name: string): ProviderAccessDescriptor {
  return {
    methods: [{
      id: "api-key",
      kind: "api-key",
      connectionId: name,
      supportsMultiple: true,
      poolSupported: true,
      connectionPresent: true,
    }],
    defaultMethodId: "api-key",
    activitySupported: true,
  };
}

function fallbackAccess(item: AccessPolicyItem, localProvider: boolean): ProviderAccessDescriptor {
  const mode = (item.authMode ?? "").toLowerCase();
  if (mode === "forward" || mode === "local" || localProvider) return noAccess();
  if (mode === "oauth") return oauthAccess(item.name);
  const hasKeyMaterial = item.hasApiKey === true;
  if (item.keyOptional === true && !hasKeyMaterial) return noAccess();
  const keyAuth = mode === "key" || hasKeyMaterial || mode === "";
  return keyAuth ? apiKeyAccess(item.name) : noAccess();
}

export function accessDescriptorPolicy(
  item: AccessPolicyItem,
  context: AccessPolicyContext,
): ProviderAccessDescriptor {
  if (item.access) {
    return context.accountProvider
      ? accountAccessOverlay(item, item.access, context.apiLanePresent)
      : item.access;
  }
  if (context.accountProvider) return defaultAccountAccess(item, context.apiLanePresent);
  return fallbackAccess(item, context.localProvider);
}
