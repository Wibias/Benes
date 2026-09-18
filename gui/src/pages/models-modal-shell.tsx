/** Benes dashboard client for the Go proxy (`internal/server`). */
import type { ReactNode } from "react";

export function ModelsModalShell({
  open,
  label,
  onClose,
  closeDisabled,
  children,
}: {
  open: boolean;
  label: string;
  onClose: () => void;
  closeDisabled?: boolean;
  children: ReactNode;
}) {
  const close = () => {
    if (!closeDisabled) onClose();
  };
  return (
    <div
      className="modal-overlay models-modal-overlay"
      data-open={open ? "true" : "false"}
      role="dialog"
      aria-modal={open}
      aria-label={label}
      onClick={close}
      onKeyDown={event => {
        if (event.key === "Escape") close();
      }}
      {...(!open ? { inert: true } : {})}
    >
      <div className="modal-card models-modal-card" onClick={event => event.stopPropagation()}>
        {children}
      </div>
    </div>
  );
}
