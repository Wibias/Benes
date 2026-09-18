/**
 * Benes dashboard source. The generated-config preview for one export client.
 *
 * The pane renders the view `./client-config-resource` resolved, including the
 * transfer triple its actions move. That is what keeps the preview and the
 * copied bytes identical by construction: both come from the listener's own
 * filename, media type, and generated text, so neither can be rebuilt from
 * `config` — which would emit JSON for the clients that read TOML.
 *
 * Both sections are the same shape — a heading, an optional note beside it, and
 * a body — so they are rendered through one local section rather than twice by
 * hand, and the action table is the view's, so the head cannot grow a control
 * the view does not describe.
 */
import type { ReactNode } from "react";
import type { ClientConfigActionId, ClientConfigDetailView } from "./client-config-resource.ts";

export function ClientConfigDetail({
  view,
  onAction,
}: {
  view: ClientConfigDetailView;
  onAction: (id: ClientConfigActionId) => void;
}) {
  return (
    <div className="awi-detail">
      <div className="awi-detail-head">
        <h3 className="awi-detail-title">{view.title}</h3>
        <span className="awi-detail-actions">
          {view.actions.map(action => (
            <button
              key={action.id}
              type="button"
              className="providers-link providers-link--plain"
              onClick={() => onAction(action.id)}
            >
              {action.label}
            </button>
          ))}
        </span>
      </div>
      <PaneSection heading={view.overviewTitle}>
        <dl className="awi-kv">
          {view.overview.map(row => (
            <div className="awi-kv-row" key={row.label}>
              <dt>{row.label}</dt>
              <dd>{row.code ? <code>{row.value}</code> : row.value}</dd>
            </div>
          ))}
        </dl>
        {view.notes.map(note => <p className="muted small" key={note}>{note}</p>)}
      </PaneSection>
      <PaneSection heading={view.preview.title} note={view.preview.note}>
        <pre
          className="api-code api-example-pre awi-generated"
          tabIndex={0}
          role="group"
          aria-label={view.preview.label}
        >{view.preview.text}</pre>
      </PaneSection>
    </div>
  );
}

/** One section of the pane: its heading, an optional note beside it, its body. */
function PaneSection({
  heading,
  note,
  children,
}: {
  heading: string;
  note?: string;
  children: ReactNode;
}) {
  return (
    <div className="awi-section">
      {note === undefined ? (
        <h4 className="awi-section-title">{heading}</h4>
      ) : (
        <div className="awi-section-head">
          <h4 className="awi-section-title">{heading}</h4>
          <span className="muted small">{note}</span>
        </div>
      )}
      {children}
    </div>
  );
}
