import { useCallback, useRef, useState } from "react";
import {
  accountsFocusAdjustment,
  clampedDetailTab,
  pendingLeaveBack,
  pendingLeaveTab,
  type DetailTab,
} from "../../provider-workspace/detail-tabs";

export function useDetailTabs(showAccess: boolean, scopedAccountsFocusToken: number, onBack?: () => void) {
  const [tab, setTab] = useState<DetailTab>("overview");
  const [configDirty, setConfigDirty] = useState(false);
  const [pendingTab, setPendingTab] = useState<DetailTab | null>(null);
  const [pendingBack, setPendingBack] = useState(false);
  const [leaveSaving, setLeaveSaving] = useState(false);
  const [configEpoch, setConfigEpoch] = useState(0);
  const [seenAccountsFocusToken, setSeenAccountsFocusToken] = useState(0);
  const saveConfigRef = useRef<(() => Promise<boolean>) | null>(null);
  const registerConfigSave = useCallback((fn: (() => Promise<boolean>) | null) => {
    saveConfigRef.current = fn;
  }, []);

  const clamped = clampedDetailTab(showAccess, tab);
  if (clamped !== tab) setTab(clamped);

  // Adjust related state when accountsFocusToken changes during render (not in an
  // effect) so the Access tab is selected without a one-frame stale paint.
  // https://react.dev/learn/you-might-not-need-an-effect#adjusting-some-state-when-a-prop-changes
  const focus = accountsFocusAdjustment(scopedAccountsFocusToken, seenAccountsFocusToken);
  if (focus) {
    setSeenAccountsFocusToken(focus.seen);
    if (focus.tab) setTab(focus.tab);
  }

  const switchTab = (next: DetailTab) => {
    const decision = pendingLeaveTab(next, configDirty);
    if ("pending" in decision) {
      setPendingTab(decision.pending);
      return;
    }
    setTab(decision.tab);
  };

  const requestBack = () => {
    if (!onBack) return;
    if (pendingLeaveBack(configDirty) === "pending") {
      setPendingBack(true);
      return;
    }
    onBack();
  };

  const finishLeave = async (onSuccess: () => void, onFail: () => void) => {
    setLeaveSaving(true);
    try {
      const ok = await saveConfigRef.current?.() ?? true;
      if (!ok) {
        onFail();
        return;
      }
      setConfigDirty(false);
      onSuccess();
    } finally {
      setLeaveSaving(false);
    }
  };

  const discardAndSwitch = (next: DetailTab | null) => {
    setConfigEpoch(n => n + 1);
    setConfigDirty(false);
    setPendingTab(null);
    if (next) setTab(next);
  };

  const discardAndLeave = () => {
    setConfigEpoch(n => n + 1);
    setConfigDirty(false);
    setPendingBack(false);
    onBack?.();
  };

  return {
    tab,
    setTab,
    configDirty,
    setConfigDirty,
    pendingTab,
    setPendingTab,
    pendingBack,
    setPendingBack,
    leaveSaving,
    configEpoch,
    registerConfigSave,
    switchTab,
    requestBack,
    finishLeave,
    discardAndSwitch,
    discardAndLeave,
  };
}
