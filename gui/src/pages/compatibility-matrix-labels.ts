/**
 * Benes dashboard source. What the Compatibility surface calls the Lab's own vocabulary.
 *
 * The vocabulary itself is the accepted `lab/evidence-vocabulary` owner's: this module only
 * names it. A layer carries both of its names in one place — the prose name the detail pane
 * prints and the short column heading the matrix prints — so a layer cannot gain one without
 * the other. A verdict's name is derived from the value the owner publishes, because the
 * verdict keys follow the verdict and a second hand-written list could only drift from it.
 */
import type { TKey } from "../i18n/shared.ts";
import type { CompatibilityVerdict, EvidenceLayer } from "../lab/evidence-vocabulary.ts";

/** One evidence layer, as the board names it: in prose, and as a matrix column. */
export interface LayerText {
  readonly name: TKey;
  readonly column: TKey;
}

/** Every layer the schema can carry, named for both surfaces that print it. */
export const LAYER_TEXT: Record<EvidenceLayer, LayerText> = {
  protocol_conformance: { name: "lab.layer.protocol_conformance", column: "lab.col.protocol" },
  live_route_compatibility: { name: "lab.layer.live_route_compatibility", column: "lab.col.live" },
  task_effectiveness: { name: "lab.layer.task_effectiveness", column: "lab.col.task" },
};

/**
 * The catalog key naming one verdict.
 *
 * Every verdict the projection defines is answered here, and the type is what makes that a
 * promise rather than a habit: a verdict added to the vocabulary leaves this function without
 * a return for it, which the compiler rejects.
 */
export function verdictText(verdict: CompatibilityVerdict): TKey {
  switch (verdict) {
    case "UNKNOWN": return "lab.verdict.UNKNOWN";
    case "CLAIMED": return "lab.verdict.CLAIMED";
    case "PROBED": return "lab.verdict.PROBED";
    case "VERIFIED": return "lab.verdict.VERIFIED";
    case "DEGRADED": return "lab.verdict.DEGRADED";
    case "BLOCKED": return "lab.verdict.BLOCKED";
    case "UNSUPPORTED": return "lab.verdict.UNSUPPORTED";
  }
}
