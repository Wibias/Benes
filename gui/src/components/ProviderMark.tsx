/** Decorative provider identity mark. Surrounding controls own the accessible name. */
import { providerIconKnockout, providerIconSrc } from "../provider-icons";

export type ProviderMarkProps = {
  name: string;
  adapter?: string;
  baseUrl?: string;
  className?: string;
};

type AssetMark = {
  kind: "asset";
  src: string;
  invertDark: boolean;
  plate?: { color: string; radius?: string };
};

type LetterMark = {
  kind: "letter";
  glyph: string;
  background: string;
  color: string;
};

const LETTER_INK: ReadonlyArray<{ background: string; color: string }> = [
  { background: "#1b4d5c", color: "#e7f4f8" },
  { background: "#3a2a58", color: "#f1e9ff" },
  { background: "#4a2f1c", color: "#f8eadc" },
  { background: "#1f4a38", color: "#def5e8" },
  { background: "#4a2030", color: "#fde8ef" },
  { background: "#2c3b5c", color: "#e6edfb" },
  { background: "#3f3a12", color: "#f7f3d4" },
  { background: "#2d3438", color: "#edf1f3" },
];

function letterIndex(name: string): number {
  let hash = 2166136261;
  const key = name.trim().toLowerCase();
  for (let i = 0; i < key.length; i += 1) {
    hash ^= key.charCodeAt(i);
    hash = Math.imul(hash, 16777619);
  }
  return (hash >>> 0) % LETTER_INK.length;
}

function letterGlyph(name: string): string {
  const trimmed = name.trim();
  if (!trimmed) return "?";
  return trimmed.slice(0, 1).toUpperCase();
}

function resolveMark(name: string, adapter?: string, baseUrl?: string): AssetMark | LetterMark {
  const src = providerIconSrc(name, { adapter, baseUrl });
  if (src) {
    const knockout = providerIconKnockout(name);
    return {
      kind: "asset",
      src,
      invertDark: Boolean(knockout?.invertDark),
      plate: knockout?.lightPlate
        ? { color: knockout.lightPlate, radius: knockout.lightPlateRadius }
        : undefined,
    };
  }
  const ink = LETTER_INK[letterIndex(name)]!;
  return { kind: "letter", glyph: letterGlyph(name), background: ink.background, color: ink.color };
}

export function ProviderMark({ name, adapter, baseUrl, className }: ProviderMarkProps) {
  const mark = resolveMark(name, adapter, baseUrl);
  if (mark.kind === "asset") {
    const imgClass = [
      mark.invertDark ? "provider-icon-img--invert-dark" : "",
      mark.plate ? "provider-icon-img--light-plate" : "",
    ].filter(Boolean).join(" ");
    const style = mark.plate
      ? {
          ["--provider-icon-light-plate" as string]: mark.plate.color,
          ...(mark.plate.radius
            ? { ["--provider-icon-light-plate-radius" as string]: mark.plate.radius }
            : {}),
        }
      : undefined;
    return (
      <span className={className} aria-hidden="true">
        <img src={mark.src} alt="" className={imgClass || undefined} style={style} />
      </span>
    );
  }
  return (
    <span className={className} aria-hidden="true">
      <span className="provider-icon-fallback" style={{ background: mark.background, color: mark.color }}>
        {mark.glyph}
      </span>
    </span>
  );
}
