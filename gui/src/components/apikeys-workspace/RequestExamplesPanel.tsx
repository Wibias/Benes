/**
 * Benes dashboard source. The Examples tab of the API workspace.
 *
 * The tab renders `deriveExamplesPanel`'s model: the section copy, and each
 * sample with its heading already resolved. Nothing here reads the one-time key
 * — the model is built from the accepted sample builder, which authenticates
 * with a literal placeholder — and nothing here decides which API surfaces
 * exist.
 */
import { useT } from "../../i18n/shared";
import type { ApiEndpointInfo } from "../../api-access/endpoints";
import { deriveExamplesPanel } from "./request-examples";
import { ExampleCopyBlock } from "./copy-affordance";

export interface RequestExamplesPanelProps {
  readonly endpoints: ApiEndpointInfo;
  /** The Messages surface only exists while the Claude harness is enabled. */
  readonly claudeCodeEnabled: boolean;
}

export function RequestExamplesPanel(props: RequestExamplesPanelProps) {
  const t = useT();
  const panel = deriveExamplesPanel(props.endpoints, props.claudeCodeEnabled, t);

  return (
    <section className="awi-usage-panel">
      <ExamplesHead title={panel.title} intro={panel.intro} />
      <div className="awi-usage-panel-body">
        {panel.examples.map(example => (
          <ExampleRow key={example.id} heading={example.title} sample={example.text} />
        ))}
      </div>
    </section>
  );
}

/** Section heading. The inner block keeps the title above its subtext. */
function ExamplesHead({ title, intro }: { title: string; intro: string }) {
  return (
    <div className="api-panel-head">
      <div>
        <h3 className="awi-board-title">{title}</h3>
        <p className="muted small">{intro}</p>
      </div>
    </div>
  );
}

/** One sample: its heading, and the copy affordance over the exact bytes. */
function ExampleRow({ heading, sample }: { heading: string; sample: string }) {
  return (
    <div className="awi-usage-example">
      <h4 className="awi-usage-example-title">{heading}</h4>
      <ExampleCopyBlock text={sample} />
    </div>
  );
}
