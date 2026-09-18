/** Benes dashboard client for the Go proxy (`internal/server`). */
import { Component, type ComponentProps, type ReactNode } from "react";

interface ErrorBoundaryProps {
  title: string;
  message: string;
  detailsLabel: string;
  reloadLabel: string;
  /** Localized page name; the fallback headline prefixes it to the failure title. */
  pageName: string;
  children: ReactNode;
}

interface BoundaryState {
  /** Message of the thrown value, or `null` while the subtree renders. */
  failure: string | null;
}

/** Anything a component throws is rendered as text, so non-Errors are stringified. */
function failureText(thrown: unknown): string {
  return thrown instanceof Error ? thrown.message : String(thrown);
}

/**
 * The fallback's fixed chrome, one attribute bag per block. Keeping the inline
 * numbers here leaves the markup below a template that carries only copy.
 */
const PANEL_ATTRIBUTES = {
  className: "card",
  role: "alert",
  style: { maxWidth: 720, padding: "var(--space-6)" },
} as const satisfies ComponentProps<"section">;

const HEADLINE_ATTRIBUTES = {
  style: { margin: "0 0 var(--space-2)", fontSize: "var(--text-title)" },
} as const satisfies ComponentProps<"h2">;

const SUMMARY_ATTRIBUTES = {
  className: "muted",
  style: { margin: "0 0 var(--space-4)" },
} as const satisfies ComponentProps<"p">;

const DETAILS_ATTRIBUTES = {
  style: { margin: "0 0 var(--space-5)", overflowWrap: "anywhere" },
} as const satisfies ComponentProps<"p">;

interface FailureReportProps {
  headline: string;
  summary: string;
  detailsLabel: string;
  details: string;
  actionLabel: string;
  onReload: () => void;
}

/** The fallback: what went wrong, plus the single action that retries the subtree. */
function FailureReport({ headline, summary, detailsLabel, details, actionLabel, onReload }: FailureReportProps) {
  return (
    <section {...PANEL_ATTRIBUTES}>
      <h2 {...HEADLINE_ATTRIBUTES}>{headline}</h2>
      <p {...SUMMARY_ATTRIBUTES}>{summary}</p>
      <p {...DETAILS_ATTRIBUTES}>
        <strong>{detailsLabel}:</strong> {details}
      </p>
      <button type="button" className="btn btn-primary" onClick={onReload}>
        {actionLabel}
      </button>
    </section>
  );
}

/**
 * Page-level failure containment.
 *
 * The boundary keeps only the thrown text; the report and its recovery action are
 * one presentational component, so clearing the state remounts the failed subtree
 * exactly as a fresh `key` on the boundary would.
 */
export default class ErrorBoundary extends Component<ErrorBoundaryProps, BoundaryState> {
  state: BoundaryState = { failure: null };

  static getDerivedStateFromError(thrown: unknown): BoundaryState {
    return { failure: failureText(thrown) };
  }

  private readonly clearFailure = (): void => {
    this.setState({ failure: null });
  };

  render(): ReactNode {
    const { failure } = this.state;
    if (failure === null) return this.props.children;

    const { pageName, title, message, detailsLabel, reloadLabel } = this.props;
    return (
      <FailureReport
        headline={`${pageName}: ${title}`}
        summary={message}
        detailsLabel={detailsLabel}
        details={failure}
        actionLabel={reloadLabel}
        onReload={this.clearFailure}
      />
    );
  }
}