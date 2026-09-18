/** Benes dashboard client for the Go proxy (`internal/server`). */

export function SessionFact({ label, value }: { label: string; value: string | undefined | null }) {
  return (
    <div className="sessions-fact">
      <dt>{label}</dt>
      <dd>{value && value !== "" ? value : "\u2014"}</dd>
    </div>
  );
}
