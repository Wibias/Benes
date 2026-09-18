/** Benes-owned presentation for an empty provider row in the Models catalogue. */
import { createElement, type ReactNode } from "react";
import { navigateHash } from "../hash-routing";
import { IconInfo } from "../icons";
import type { TFn } from "../i18n/shared";
import type { ProviderDiscoverySummary } from "../models-groups";
import { discoveryFailureLabel } from "./models-shared";

export interface ProviderHintSurfaceInput {
  readonly t: TFn;
  readonly liveModels: boolean;
  readonly discovery?: ProviderDiscoverySummary;
  readonly showFailureBadge?: boolean;
  readonly onSync?: () => void;
  readonly syncing?: boolean;
}

type ProviderHintCopy = {
  readonly text: string;
  readonly failed: boolean;
};

function resolveProviderHintCopy(input: ProviderHintSurfaceInput): ProviderHintCopy {
  const failedDiscovery = input.liveModels && input.discovery?.status === "failed"
    ? input.discovery
    : undefined;

  if (failedDiscovery) {
    return {
      text: discoveryFailureLabel(input.t, failedDiscovery),
      failed: true,
    };
  }

  return {
    text: input.t(input.liveModels ? "models.emptyDiscovery" : "models.emptyDiscoveryDisabled"),
    failed: false,
  };
}

function actionNode(
  key: string,
  label: string,
  onClick: () => void,
  disabled = false,
): ReactNode {
  return createElement(
    "button",
    {
      key,
      type: "button",
      className: "link-btn",
      disabled,
      onClick,
    },
    label,
  );
}

function providerHintChildren(
  input: ProviderHintSurfaceInput,
  copy: ProviderHintCopy,
): ReactNode[] {
  const children: ReactNode[] = [];

  if (copy.failed && input.showFailureBadge !== false) {
    children.push(createElement(
      "span",
      { key: "failure", className: "badge badge-amber" },
      input.t("models.discoveryFailedBadge"),
    ));
    children.push(" ");
  }

  children.push(copy.text);

  if (input.liveModels && input.onSync) {
    children.push(" ");
    children.push(actionNode(
      "sync",
      input.t("prov.menu.syncModels"),
      input.onSync,
      input.syncing === true,
    ));
  }

  children.push(" ");
  children.push(actionNode(
    "providers",
    input.t("models.openProviderSettings"),
    () => navigateHash("providers"),
  ));

  return children;
}

export function ProviderHintSurface(input: ProviderHintSurfaceInput) {
  const copy = resolveProviderHintCopy(input);
  const body = createElement("span", null, ...providerHintChildren(input, copy));
  const icon = createElement(IconInfo, {
    width: 15,
    height: 15,
    "aria-hidden": true,
    style: { flexShrink: 0, marginTop: 2 },
  });

  return createElement(
    "div",
    {
      className: "row muted text-label leading-body",
      role: "status",
      style: { alignItems: "flex-start", gap: 8, padding: "6px 0" },
    },
    icon,
    body,
  );
}
