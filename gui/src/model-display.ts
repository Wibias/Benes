/** Benes dashboard client for the Go proxy (`internal/server`). */
/**
 * Model rows carry a distinguishing glyph so the 5.6 trio stays readable in dropdowns,
 * tables, and badges. Lucide-style inline SVG only — no emoji, and no glyph for a model
 * the map does not know.
 */
import { createElement, type CSSProperties, type ReactNode } from "react";
import { IconGlobe, IconMoon, IconSun } from "./icons";

type ModelIcon = typeof IconSun;

const GLYPH_PX = 14;

/** Slugs with a glyph; a provider-prefixed id also matches on its leaf segment. */
const MODEL_ICONS: ReadonlyArray<readonly [string, ModelIcon]> = [
  ["gpt-5.6-sol", IconSun],
  ["gpt-5.6-terra", IconGlobe],
  ["gpt-5.6-luna", IconMoon],
];

function iconFor(slug: string): ModelIcon | null {
  const leaf = slug.slice(slug.lastIndexOf("/") + 1);
  const entry = MODEL_ICONS.find(([candidate]) => candidate === slug || candidate === leaf);
  return entry ? entry[1] : null;
}

function glyphProps(): { style: CSSProperties; "aria-hidden": true } {
  return {
    style: { width: GLYPH_PX, height: GLYPH_PX, flexShrink: 0, verticalAlign: "text-bottom" },
    "aria-hidden": true,
  };
}

/** A model name with its glyph prefix, or the plain slug when the model has none. */
export function modelLabel(slug: string): ReactNode {
  const Icon = iconFor(slug);
  if (!Icon) return slug;
  return createElement("span", { className: "model-label" }, createElement(Icon, glyphProps()), slug);
}
