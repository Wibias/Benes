import { useEffect, useState } from "react";
import { consumedOneTimeSecret, oneTimeSecretForDialog } from "../api-access/one-time-secret";
import { readApiTab, type ApiTab } from "./api-tab";

export function useApiTab(): ApiTab {
  const [tab, setTab] = useState<ApiTab>(readApiTab);
  useEffect(() => {
    const sync = () => setTab(readApiTab());
    window.addEventListener("hashchange", sync);
    window.addEventListener("popstate", sync);
    return () => {
      window.removeEventListener("hashchange", sync);
      window.removeEventListener("popstate", sync);
    };
  }, []);
  return tab;
}

export function createKeyDialogState(
  liveSecret: string | null,
  createOpen: boolean,
  consumedSecret: string | null,
): string | null {
  return oneTimeSecretForDialog(
    liveSecret,
    createOpen,
    liveSecret !== null && liveSecret === consumedSecret,
  );
}

export function nextConsumedSecret(dialogSecret: string | null, consumedSecret: string | null): string | null {
  return consumedOneTimeSecret(dialogSecret, consumedSecret);
}

export function noop(): void {}

export function readyDialogFields(ready: {
  newName: string;
  creating: boolean;
  copied: boolean;
  onNewNameChange: (value: string) => void;
  onCreate: () => void;
  onCopyKey: () => void;
} | null): {
  newName: string;
  creating: boolean;
  copied: boolean;
  onNewNameChange: (value: string) => void;
  onCreate: () => void;
  onCopyKey: () => void;
} {
  if (!ready) {
    return {
      newName: "",
      creating: false,
      copied: false,
      onNewNameChange: noop,
      onCreate: noop,
      onCopyKey: noop,
    };
  }
  return {
    newName: ready.newName,
    creating: ready.creating,
    copied: ready.copied,
    onNewNameChange: ready.onNewNameChange,
    onCreate: ready.onCreate,
    onCopyKey: ready.onCopyKey,
  };
}

export function shouldRenderApiReady(showSkeleton: boolean, failedCold: boolean, hasReady: boolean): boolean {
  return hasReady && !showSkeleton && !failedCold;
}
