import { Select } from "../ui";
import type { TFn } from "../i18n/shared";

export function NewModelsPolicyControl({
  policy,
  busy,
  t,
  onChange,
}: {
  policy: "on" | "off";
  busy: boolean;
  t: TFn;
  onChange: (policy: "on" | "off") => void;
}) {
  return (
    <Select
      value={policy}
      options={[
        { value: "off", label: t("models.newPolicyDefault", { policy: t("pws.disabledLabel") }) },
        { value: "on", label: t("models.newPolicyDefault", { policy: t("pws.enabledLabel") }) },
      ]}
      onChange={value => onChange(value === "off" ? "off" : "on")}
      disabled={busy}
      label={t("models.newPolicyLabel")}
    />
  );
}

export function NewArrivalNote({ text }: { text?: string }) {
  if (!text) return null;
  return <span className="muted mono text-label">{text}</span>;
}
