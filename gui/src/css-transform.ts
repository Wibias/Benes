/** CSS transform strings. Kept out of pages/components so i18n lint does not treat units as copy. */

export function cssPx(value: number): string {
  return `${value}px`;
}

export function cssTranslate3d(x: number, y: number): string {
  return `translate3d(${x}px, ${y}px, 0)`;
}

export function cssTranslateY(y: number): string {
  return `translateY(${y}px)`;
}

export function cssTransformTransition(): string {
  return "transform var(--subagents-reorder-move, 300ms cubic-bezier(0.77, 0, 0.175, 1))";
}
