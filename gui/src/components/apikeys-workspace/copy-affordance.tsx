/**
 * Benes dashboard source. Copy affordances for the API workspace.
 *
 * Both surfaces here copy a value the reader can already see: a data-plane URL
 * on the Endpoints tab, and a curl example on the Examples tab. The clipboard
 * protocol itself is not re-implemented — `useCopyFeedback` owns the attempt
 * sequence, the honest `copied`/`unavailable` outcome, and the expiry, so a
 * denied or non-secure clipboard never reads as success.
 *
 * The hover/focus hint is a fixed-position portal because both surfaces sit
 * inside columns that clip overflow; it is described to assistive technology
 * through `aria-describedby` rather than the native `title` attribute.
 */
import {
  useCallback,
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { createPortal } from "react-dom";
import { IconCopy } from "../../icons";
import { useI18n, type TKey } from "../../i18n/shared";
import { useCopyFeedback } from "../use-copy-feedback";

type CopySubject = "url" | "example";

/** Hint and success strings per surface; the CLI example reads its own verbs. */
const COPY_COPY: Record<CopySubject, { hint: TKey; copied: TKey }> = {
  url: { hint: "api.copyUrlHint", copied: "api.urlCopied" },
  example: { hint: "api.copyExampleHint", copied: "api.exampleCopied" },
};

/** Gap between the trigger and the bubble, in CSS pixels. */
const TIP_GAP_PX = 8;
const TIP_MIN_TOP_PX = 8;

/**
 * True when the click landed on the surface's own scrollbar.
 *
 * Clicking a scrollbar is a scroll gesture, not a copy gesture. The target is
 * the scrolled element (the `<pre>`), not the button wrapper, so the wrapper's
 * own box says nothing about where the pointer was.
 */
function hitScrollbar(
  target: EventTarget | null,
  currentTarget: EventTarget & Element,
  point: { x: number; y: number },
): boolean {
  if (!(target instanceof HTMLElement) || target === currentTarget) return false;
  const box = target.getBoundingClientRect();
  if (target.scrollHeight > target.clientHeight + 1 && point.x >= box.right - 16) return true;
  return target.scrollWidth > target.clientWidth + 1 && point.y >= box.bottom - 16;
}

function CopyAffordance({
  text,
  subject,
  className,
  children,
  label,
}: {
  text: string;
  subject: CopySubject;
  className: string;
  children: ReactNode;
  /** Accessible name for an icon-only trigger. */
  label?: string;
}) {
  const { t } = useI18n();
  const feedback = useCopyFeedback<string>();
  const tipId = useId();
  const triggerRef = useRef<HTMLButtonElement | null>(null);
  const [hovered, setHovered] = useState(false);
  const [tipTop, setTipTop] = useState<number | null>(null);
  const copied = feedback.outcomeFor(text) === "copied";
  const showing = hovered || copied;

  useLayoutEffect(() => {
    if (!showing) return undefined;
    const reposition = () => {
      const trigger = triggerRef.current;
      if (!trigger) return;
      setTipTop(Math.max(TIP_MIN_TOP_PX, trigger.getBoundingClientRect().top - TIP_GAP_PX));
    };
    // Position before paint, then follow the viewport while the hint is up.
    reposition();
    window.addEventListener("scroll", reposition, true);
    window.addEventListener("resize", reposition);
    return () => {
      window.removeEventListener("scroll", reposition, true);
      window.removeEventListener("resize", reposition);
    };
  }, [showing]);

  const copy = useCallback(
    (point: { x: number; y: number }, target: EventTarget | null, currentTarget: EventTarget & Element) => {
      // A drag-select inside the code surface ends in a click; copying then
      // would replace what the reader just selected.
      if (window.getSelection()?.toString()) return;
      if (hitScrollbar(target, currentTarget, point)) return;
      feedback.copy(text, text);
    },
    [feedback, text],
  );

  const bubble = showing && tipTop !== null
    ? createPortal(
      <span
        id={tipId}
        className="benes-tooltip-bubble api-copy-tip-fixed"
        role="tooltip"
        style={{ top: tipTop }}
      >
        {t(copied ? COPY_COPY[subject].copied : COPY_COPY[subject].hint)}
      </span>,
      document.body,
    )
    : null;

  return (
    <>
      <button
        ref={triggerRef}
        type="button"
        className={`benes-tooltip ${className}`}
        aria-label={label ?? t(COPY_COPY[subject].hint)}
        aria-describedby={showing ? tipId : undefined}
        onMouseEnter={() => setHovered(true)}
        onMouseLeave={() => setHovered(false)}
        onFocus={() => setHovered(true)}
        onBlur={() => setHovered(false)}
        onKeyDown={event => {
          if (event.key === "Escape") setHovered(false);
        }}
        onClick={event => copy({ x: event.clientX, y: event.clientY }, event.target, event.currentTarget)}
      >
        {children}
      </button>
      {bubble}
    </>
  );
}

/** Icon-only copy control for one endpoint row on the Endpoints tab. */
export function EndpointCopyIcon({ url }: { url: string }) {
  return (
    <CopyAffordance text={url} subject="url" className="awi-endpoint-copy">
      <IconCopy aria-hidden="true" />
    </CopyAffordance>
  );
}

/** The curl example itself is the button; the visible sample is selectable. */
export function ExampleCopyBlock({ text }: { text: string }) {
  return (
    <CopyAffordance text={text} subject="example" className="api-example-copy-btn">
      <code className="api-code api-example-pre">{text}</code>
    </CopyAffordance>
  );
}
