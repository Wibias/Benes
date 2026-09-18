import type { HarnessId } from "./types";

/**
 * The Harnesses mark registry: one row per harness identity.
 *
 * A row states the asset that renders the harness and whether that asset is a dark or
 * multi-colour glyph the dark theme repaints. Keeping both facts on the row is what stops the
 * monochrome list from drifting away from the marks it describes. Third-party marks live in
 * `/provider-icons` and Benes' own glyphs in `/harness-icons`; the split is real (a harness
 * with a third-party mark keeps that brand), so the path is stated per row rather than derived.
 */
export type HarnessIcon = {
  /** Public path of the mark. */
  readonly src: string;
  /** The mark is repainted for the dark theme instead of being shown as authored. */
  readonly mono?: true;
};

export const HARNESS_ICON: Record<HarnessId, HarnessIcon> = {
  "claude-desktop": { src: "/provider-icons/claude-color.svg" },
  claude: { src: "/provider-icons/claude-color.svg" },
  codex: { src: "/provider-icons/openai.svg" },
  dsh: { src: "/harness-icons/dsh.svg" },
  opencode: { src: "/provider-icons/opencode.svg" },
  pi: { src: "/harness-icons/pi.svg", mono: true },
  prime: { src: "/harness-icons/prime.svg", mono: true },
  omp: { src: "/harness-icons/omp.svg" },
  hermes: { src: "/harness-icons/hermes.svg" },
  openclaw: { src: "/harness-icons/openclaw.svg" },
  kimi: { src: "/provider-icons/kimi-color.svg", mono: true },
  gajae: { src: "/harness-icons/gajae.svg" },
  grok: { src: "/provider-icons/grok.svg", mono: true },
  mcode: { src: "/harness-icons/mcode.svg" },
};
