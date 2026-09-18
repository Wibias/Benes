/**
 * The Lab's scenario catalog, as a profile's required-suite list offers it.
 *
 * The catalog is what the Lab *can* run: one row per suite, in one of the two evidence
 * layers Benes produces. A suite is identified by its layer together with its id, because
 * that pair — not the id alone — is what a profile selects, so two suites sharing an id in
 * different layers stay two distinct choices. The layers come from the vocabulary owner, so a
 * layer the schema adds is offered without a second list to keep in step.
 */
import {
  PRODUCT_EVIDENCE_LAYERS,
  type ProductEvidenceLayer,
} from "./evidence-vocabulary.ts";
import { isPlainObject } from "./lab-records.ts";

/** One suite a profile may require. */
export interface LabCatalogSuite {
  readonly suiteId: string;
  readonly evidenceLayer: ProductEvidenceLayer;
  /** The (layer, suite) identity the editor selects by. */
  readonly key: string;
}

/** The key one (layer, suite) pair is identified by. */
function suiteKey(evidenceLayer: ProductEvidenceLayer, suiteId: string): string {
  return `${evidenceLayer}:${suiteId}`;
}

function readSuite(row: unknown): LabCatalogSuite | null {
  if (!isPlainObject(row)) return null;
  const suiteId = typeof row.suiteId === "string" ? row.suiteId.trim() : "";
  if (suiteId === "") return null;
  const evidenceLayer = PRODUCT_EVIDENCE_LAYERS.find(layer => layer === row.evidenceLayer);
  if (evidenceLayer === undefined) return null;
  return { suiteId, evidenceLayer, key: suiteKey(evidenceLayer, suiteId) };
}

/**
 * Every suite the catalog names, deduplicated, in layer then suite-id order.
 *
 * `null` means the Lab answered with something that is not a catalog: the caller reports a
 * catalog it could not read rather than an empty catalog it never had.
 */
export function readLabCatalog(payload: unknown): LabCatalogSuite[] | null {
  if (!isPlainObject(payload) || !Array.isArray(payload.scenarios)) return null;
  const suites = new Map<string, LabCatalogSuite>();
  for (const row of payload.scenarios) {
    const suite = readSuite(row);
    if (suite !== null && !suites.has(suite.key)) suites.set(suite.key, suite);
  }
  return [...suites.values()].sort((left, right) => left.key.localeCompare(right.key));
}
