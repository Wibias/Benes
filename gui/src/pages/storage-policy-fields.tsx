/** Benes dashboard client for the Go proxy (`internal/server`). */
import { NumberStepper } from "../components/NumberStepper";
import { clampNumberDraft } from "../clamp-draft";
import { formatBytes } from "../format-bytes";
import type { Locale, TFn } from "../i18n/shared";
import { ToastNotice } from "../ui";
import { runOutcomeView, type CleanupPolicy } from "./storage-cleanup-policy";

/** The editable drafts this form holds, as text because that is what a field value is. */
export type PolicyDrafts = {
  readonly thresholdGb: string;
  readonly targetMode: "percent" | "reduce";
  readonly percent: string;
  readonly reduceGb: string;
};

/** One policy number: its field, its unit, and the stepper pair that steps it. */
type NumberFieldSpec = {
  readonly id: string;
  readonly value: string;
  readonly min: number;
  readonly max?: number;
  readonly step: number;
  /** How far one stepper click moves the draft; also sets the printed precision. */
  readonly stepBy: number;
  readonly inputMode: "numeric" | "decimal";
  readonly unit: string;
  readonly ariaLabel: string;
  readonly incrementLabel: string;
  readonly decrementLabel: string;
  readonly disabled: boolean;
  readonly onValue: (value: string) => void;
  readonly onDirty: () => void;
  readonly onEditing: (editing: boolean) => void;
  readonly onSave: () => void;
};

/**
 * One policy number field.
 *
 * The commit gesture is shared deliberately: leaving the field or pressing Enter saves, and the
 * stepper is the pointer-only equivalent of typing a value, so every number in this form commits
 * the same way and none of them can leave an uncommitted edit behind.
 */
function PolicyNumberField({ spec }: { spec: NumberFieldSpec }) {
  const step = (delta: number) => {
    spec.onDirty();
    spec.onValue(clampNumberDraft(spec.value, delta, spec.min, spec.max ?? 10_000, spec.stepBy));
  };
  return (
    <span
      className="codex-auto-switch-input-wrap"
      onBlur={event => {
        if (event.currentTarget.contains(event.relatedTarget as Node | null)) return;
        spec.onEditing(false);
        spec.onSave();
      }}
    >
      <input
        id={spec.id}
        className="input mono codex-auto-switch-input"
        type="number"
        min={spec.min}
        max={spec.max}
        step={spec.step}
        inputMode={spec.inputMode}
        value={spec.value}
        disabled={spec.disabled}
        aria-label={spec.ariaLabel}
        onFocus={() => spec.onEditing(true)}
        onChange={event => {
          spec.onDirty();
          spec.onValue(event.target.value);
        }}
        onKeyDown={event => {
          if (event.nativeEvent.isComposing || spec.disabled) return;
          if (event.key !== "Enter") return;
          event.preventDefault();
          spec.onSave();
        }}
      />
      <span className="codex-auto-switch-unit" aria-hidden="true">{spec.unit}</span>
      <NumberStepper
        disabled={spec.disabled}
        incrementLabel={spec.incrementLabel}
        decrementLabel={spec.decrementLabel}
        onIncrement={() => step(spec.stepBy)}
        onDecrement={() => step(-spec.stepBy)}
      />
    </span>
  );
}

/** One radio choice of the target fieldset, with the number field it reveals. */
type TargetChoiceSpec = {
  readonly mode: "percent" | "reduce";
  readonly label: string;
  readonly checked: boolean;
  readonly disabled: boolean;
  readonly onChoose: () => void;
  readonly field: NumberFieldSpec;
};

/** The two ways a cleanup can pick what to remove; only the chosen one shows its number. */
function PolicyTargetFieldset({ t, choices }: { t: TFn; choices: TargetChoiceSpec[] }) {
  return (
    <fieldset className="field storage-policy-target">
      <legend className="field-label">{t("storage.policy.target")}</legend>
      {choices.map(choice => (
        <label key={choice.mode} className="storage-policy-target-row">
          <input
            type="radio"
            name="storage-policy-target"
            checked={choice.checked}
            disabled={choice.disabled}
            onChange={choice.onChoose}
          />
          <span className="storage-policy-target-label">{choice.label}</span>
          {choice.checked && <PolicyNumberField spec={choice.field} />}
        </label>
      ))}
    </fieldset>
  );
}

/** One labelled select of the policy form. */
type PolicySelectSpec = {
  readonly id: string;
  readonly label: string;
  readonly value: string;
  readonly disabled: boolean;
  readonly options: ReadonlyArray<{ readonly value: string; readonly label: string }>;
  readonly onChange: (value: string) => void;
};

function PolicySelect({ spec }: { spec: PolicySelectSpec }) {
  return (
    <label className="field" htmlFor={spec.id}>
      <span className="field-label">{spec.label}</span>
      <select
        id={spec.id}
        className="input"
        value={spec.value}
        disabled={spec.disabled}
        onChange={event => spec.onChange(event.target.value)}
      >
        {spec.options.map(option => (
          <option key={option.value} value={option.value}>{option.label}</option>
        ))}
      </select>
    </label>
  );
}

/** What the last run reported, in the board's own words. */
function lastRunOutcomeText(policy: CleanupPolicy, t: TFn, locale: Locale): string {
  const outcome = policy.job?.lastOutcome;
  if (outcome) {
    const view = runOutcomeView(outcome);
    if (view.kind === "status" || view.kind === "error") return t(view.key);
    if (view.mode === "permanent") {
      return t("storage.policy.lastRunDetail", {
        count: String(view.removed),
        size: formatBytes(view.freedBytes, locale),
      });
    }
    return t("storage.policy.lastRunCount", { count: String(view.removed) });
  }
  if (!policy.lastRun) return "";
  if (policy.mode === "permanent") {
    return t("storage.policy.lastRunDetail", {
      count: String(policy.lastRun.removed),
      size: formatBytes(policy.lastRun.freedBytes, locale),
    });
  }
  return t("storage.policy.lastRunCount", { count: String(policy.lastRun.removed) });
}

/** When the policy next runs, when it last ran, and what that run did. */
function PolicyRunMeta({
  policy, t, locale, formatWhen,
}: {
  policy: CleanupPolicy;
  t: TFn;
  locale: Locale;
  formatWhen: (ms: number | undefined) => string;
}) {
  const outcomeText = lastRunOutcomeText(policy, t, locale);
  const items = [
    { id: "next", label: t("storage.policy.nextRun"), value: formatWhen(policy.nextRun) },
    { id: "last", label: t("storage.policy.lastRun"), value: formatWhen(policy.lastRun?.at) },
    ...(outcomeText
      ? [{ id: "outcome", label: t("storage.policy.lastOutcome"), value: outcomeText }]
      : []),
  ];
  return (
    <div className="storage-policy-meta">
      {items.map(item => (
        <div key={item.id} className="storage-policy-meta-item">
          <span className="muted">{item.label}</span>
          <span className="storage-policy-meta-value">{item.value}</span>
        </div>
      ))}
    </div>
  );
}

/** Save and run-now, with whatever the last attempt reported. */
function PolicyRunActions({
  t, saving, running, error, status, savePolicy, runNow, onClearFeedback,
}: {
  t: TFn;
  saving: boolean;
  running: boolean;
  error: string | null;
  status: string | null;
  savePolicy: (patch?: Partial<CleanupPolicy>) => void;
  runNow: () => void;
  onClearFeedback: () => void;
}) {
  const feedback = error ?? status;
  return (
    <div className="storage-policy-actions">
      <button type="button" className="btn btn-ghost btn-sm" disabled={saving || running} onClick={() => void savePolicy()}>
        {t("storage.policy.save")}
      </button>
      <button type="button" className="btn btn-sm" disabled={saving || running} onClick={() => void runNow()}>
        {running ? t("storage.policy.running") : t("storage.policy.runNow")}
      </button>
      {feedback !== null && (
        <ToastNotice
          tone={error ? "err" : "ok"}
          dismissLabel={t("common.close")}
          onDismiss={onClearFeedback}
        >
          {feedback}
        </ToastNotice>
      )}
    </div>
  );
}

type PolicyFormProps = {
  policy: CleanupPolicy;
  t: TFn;
  locale: Locale;
  saving: boolean;
  running: boolean;
  drafts: PolicyDrafts;
  error: string | null;
  status: string | null;
  markDirty: () => void;
  setEditing: (editing: boolean) => void;
  onDrafts: (patch: Partial<PolicyDrafts>) => void;
  savePolicy: (patch?: Partial<CleanupPolicy>) => void;
  runNow: () => void;
  formatWhen: (ms: number | undefined) => string;
  clearFeedback: () => void;
};

export function PolicyForm(props: PolicyFormProps) {
  const {
    policy, t, locale, saving, running, drafts, error, status,
    markDirty, setEditing, onDrafts, savePolicy, runNow, formatWhen, clearFeedback,
  } = props;
  const locked = saving || running;
  const save = () => { void savePolicy(); };
  const numberField = (
    id: string,
    value: string,
    extra: Partial<NumberFieldSpec> & Pick<NumberFieldSpec, "min" | "step" | "stepBy" | "inputMode" | "unit" | "ariaLabel" | "incrementLabel" | "decrementLabel">,
  ): NumberFieldSpec => ({
    id,
    value,
    disabled: locked,
    onValue: next => onDrafts(id === "storage-policy-threshold"
      ? { thresholdGb: next }
      : id === "storage-policy-percent" ? { percent: next } : { reduceGb: next }),
    onDirty: markDirty,
    onEditing: setEditing,
    onSave: save,
    ...extra,
  });

  const triggerField = numberField("storage-policy-threshold", drafts.thresholdGb, {
    min: 0,
    step: 0.1,
    stepBy: 0.1,
    inputMode: "decimal",
    unit: "GiB",
    ariaLabel: t("storage.policy.threshold"),
    incrementLabel: t("storage.policy.thresholdInc"),
    decrementLabel: t("storage.policy.thresholdDec"),
  });
  const targets: TargetChoiceSpec[] = [
    {
      mode: "percent",
      label: t("storage.policy.targetPercent"),
      checked: drafts.targetMode === "percent",
      disabled: locked,
      onChoose: () => {
        markDirty();
        onDrafts({ targetMode: "percent" });
      },
      field: numberField("storage-policy-percent", drafts.percent, {
        min: 1,
        max: 100,
        step: 1,
        stepBy: 1,
        inputMode: "numeric",
        unit: "%",
        ariaLabel: t("storage.policy.targetPercent"),
        incrementLabel: t("storage.policy.percentInc"),
        decrementLabel: t("storage.policy.percentDec"),
      }),
    },
    {
      mode: "reduce",
      label: t("storage.policy.targetReduce"),
      checked: drafts.targetMode === "reduce",
      disabled: locked,
      onChoose: () => {
        markDirty();
        onDrafts({ targetMode: "reduce" });
      },
      field: numberField("storage-policy-reduce", drafts.reduceGb, {
        min: 0,
        step: 0.1,
        stepBy: 0.1,
        inputMode: "decimal",
        unit: "GiB",
        ariaLabel: t("storage.policy.targetReduce"),
        incrementLabel: t("storage.policy.reduceInc"),
        decrementLabel: t("storage.policy.reduceDec"),
      }),
    },
  ];
  const selects: PolicySelectSpec[] = [
    {
      id: "storage-policy-schedule",
      label: t("storage.policy.schedule"),
      value: policy.schedule,
      disabled: locked,
      options: [
        { value: "manual", label: t("storage.policy.schedule.manual") },
        { value: "startup", label: t("storage.policy.schedule.startup") },
        { value: "daily", label: t("storage.policy.schedule.daily") },
        { value: "weekly", label: t("storage.policy.schedule.weekly") },
      ],
      onChange: value => { void savePolicy({ schedule: value as CleanupPolicy["schedule"] }); },
    },
    {
      id: "storage-policy-mode",
      label: t("storage.policy.mode"),
      value: policy.mode,
      disabled: locked,
      options: [
        { value: "quarantine", label: t("storage.policy.mode.quarantine") },
        { value: "permanent", label: t("storage.policy.mode.permanent") },
      ],
      onChange: value => { void savePolicy({ mode: value as CleanupPolicy["mode"] }); },
    },
  ];

  return (
    <section className="storage-cleanup-pane">
      <div className="storage-policy-enable">
        <div className="storage-policy-enable-row">
          <button
            type="button"
            className={policy.enabled ? "toggle on" : "toggle"}
            disabled={locked}
            aria-pressed={policy.enabled}
            aria-label={t("storage.policy.enabled")}
            title={t("storage.policy.enabledHint")}
            onClick={() => void savePolicy({ enabled: !policy.enabled })}
          >
            <span className="toggle-knob" />
          </button>
          <span>{t("storage.policy.enabled")}</span>
        </div>
      </div>

      <div className="storage-policy-fields">
        <div className="field storage-policy-trigger">
          <label className="field-label" htmlFor="storage-policy-threshold">
            {t("storage.policy.trigger")}
          </label>
          <div className="storage-policy-trigger-row">
            <span className="storage-policy-trigger-hint">{t("storage.policy.threshold")}</span>
            <PolicyNumberField spec={triggerField} />
          </div>
        </div>

        <PolicyTargetFieldset t={t} choices={targets} />

        <div className="storage-policy-selects">
          {selects.map(spec => <PolicySelect key={spec.id} spec={spec} />)}
        </div>
        {policy.mode === "permanent" && (
          <p className="err storage-policy-warn" role="status">{t("storage.policy.permanentWarn")}</p>
        )}
      </div>
      <PolicyRunMeta policy={policy} t={t} locale={locale} formatWhen={formatWhen} />
      <PolicyRunActions
        t={t}
        saving={saving}
        running={running}
        error={error}
        status={status}
        savePolicy={savePolicy}
        runNow={runNow}
        onClearFeedback={clearFeedback}
      />
    </section>
  );
}