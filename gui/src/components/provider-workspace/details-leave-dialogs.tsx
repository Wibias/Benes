import { UnsavedLeaveDialog } from "./provider-overlays";

type Tab = "overview" | "access" | "configuration";

export function DetailsLeaveDialogs({
  pendingTab,
  pendingBack,
  leaveSaving,
  onCancelTab,
  onDiscardTab,
  onSaveTab,
  onCancelBack,
  onDiscardBack,
  onSaveBack,
}: {
  pendingTab: Tab | null;
  pendingBack: boolean;
  leaveSaving: boolean;
  onCancelTab: () => void;
  onDiscardTab: () => void;
  onSaveTab: () => void;
  onCancelBack: () => void;
  onDiscardBack: () => void;
  onSaveBack: () => void;
}) {
  return (
    <>
      {pendingTab && (
        <UnsavedLeaveDialog
          saving={leaveSaving}
          onCancel={onCancelTab}
          onDiscard={onDiscardTab}
          onSave={onSaveTab}
        />
      )}
      {pendingBack && (
        <UnsavedLeaveDialog
          saving={leaveSaving}
          onCancel={onCancelBack}
          onDiscard={onDiscardBack}
          onSave={onSaveBack}
        />
      )}
    </>
  );
}
