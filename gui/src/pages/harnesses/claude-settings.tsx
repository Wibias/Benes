import { useEffect, useId, useRef, useState, type ReactNode } from "react";
import { IconCopy } from "../../icons";
import { useT, type TFn } from "../../i18n/shared";
import { navigateHash } from "../../hash-routing";
import { modelLabel } from "../../model-display";
import { Select, Switch } from "../../ui";
import { describeRefusal } from "../integrations/refusal-copy";
import { readClaudeSettings, saveClaudeSettings } from "./claude-settings-io";
import {
  acceptSettingsWrite,
  catalogPickerOptions,
  claudeSettingsDirty,
  compactFieldValue,
  compactInputToOverride,
  manualSetupLines,
  pickerValue,
  type CatalogPickerOption,
  type ClaudeAuthMode,
  type ClaudeSettingsDraft,
} from "./claude-settings-state";

type ToastFn = (ok: boolean, text: string) => void;

export function ClaudeSettings({
  apiBase,
  onCopy,
  onToast,
}: {
  apiBase: string;
  onCopy: (value: string) => void;
  onToast: ToastFn;
}) {
  const t = useT();
  const [baseline, setBaseline] = useState<ClaudeSettingsDraft | null>(null);
  const [draft, setDraft] = useState<ClaudeSettingsDraft | null>(null);
  const [port, setPort] = useState(23100);
  const [modelsRaw, setModelsRaw] = useState<unknown>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [savingBase, setSavingBase] = useState<string | null>(null);
  const [formEpoch, setFormEpoch] = useState(0);
  const generation = useRef(0);
  const inflight = useRef<AbortController | null>(null);
  const saving = savingBase === apiBase;

  useEffect(() => {
    const ac = new AbortController();
    inflight.current = ac;
    const gen = generation.current + 1;
    generation.current = gen;
    void (async () => {
      try {
        const loaded = await readClaudeSettings(apiBase, ac.signal);
        if (!acceptSettingsWrite(generation.current, gen, ac.signal.aborted)) return;
        setBaseline(loaded.snapshot.draft);
        setDraft(loaded.snapshot.draft);
        setPort(loaded.snapshot.port);
        setModelsRaw(loaded.modelsRaw);
        setFormEpoch((n) => n + 1);
        setLoadError(null);
        setLoading(false);
      } catch (error) {
        if (!acceptSettingsWrite(generation.current, gen, ac.signal.aborted)) return;
        setBaseline(null);
        setDraft(null);
        setLoadError(describeRefusal(t, error, t("harnesses.claude.loadFailed")));
        setLoading(false);
      }
    })();
    return () => {
      generation.current += 1;
      ac.abort();
    };
  }, [apiBase, t]);

  const retry = () => {
    inflight.current?.abort();
    const ac = new AbortController();
    inflight.current = ac;
    const gen = generation.current + 1;
    generation.current = gen;
    setLoading(true);
    setLoadError(null);
    void (async () => {
      try {
        const loaded = await readClaudeSettings(apiBase, ac.signal);
        if (!acceptSettingsWrite(generation.current, gen, ac.signal.aborted)) return;
        setBaseline(loaded.snapshot.draft);
        setDraft(loaded.snapshot.draft);
        setPort(loaded.snapshot.port);
        setModelsRaw(loaded.modelsRaw);
        setLoadError(null);
        setLoading(false);
      } catch (error) {
        if (!acceptSettingsWrite(generation.current, gen, ac.signal.aborted)) return;
        setBaseline(null);
        setDraft(null);
        setLoadError(describeRefusal(t, error, t("harnesses.claude.loadFailed")));
        setLoading(false);
      }
    })();
  };

  const onSaved = async (nextBaseline: ClaudeSettingsDraft, nextDraft: ClaudeSettingsDraft) => {
    const gen = generation.current;
    const startedBase = apiBase;
    const signal = inflight.current?.signal;
    setSavingBase(startedBase);
    try {
      const snapshot = await saveClaudeSettings(startedBase, nextBaseline, nextDraft, signal);
      if (!acceptSettingsWrite(generation.current, gen, signal?.aborted === true)) return;
      setBaseline(snapshot.draft);
      setDraft(snapshot.draft);
      setPort(snapshot.port);
      setFormEpoch((n) => n + 1);
      onToast(true, t("harnesses.settingsSaved"));
    } catch (error) {
      if (!acceptSettingsWrite(generation.current, gen, signal?.aborted === true)) return;
      onToast(false, describeRefusal(t, error, t("harnesses.actionFailed")));
    } finally {
      setSavingBase((current) => current === startedBase ? null : current);
    }
  };

  if (loading && !draft) return <p className="page-sub">{t("common.loading")}</p>;
  if (loadError || !draft || !baseline) {
    return (
      <div className="harnesses-claude-status">
        <p className="page-sub">{loadError ?? t("harnesses.claude.loadFailed")}</p>
        <button type="button" className="btn btn-ghost btn-sm" onClick={retry}>{t("harnesses.claude.retry")}</button>
      </div>
    );
  }

  const extras = [draft.smallFastModel, draft.opus, draft.sonnet, draft.haiku, draft.fable];
  const catalog = catalogPickerOptions(modelsRaw, extras);
  return (
    <ClaudeSettingsForm
      key={formEpoch}
      baseline={baseline}
      draft={draft}
      port={port}
      catalog={catalog}
      saving={saving}
      onDraft={setDraft}
      onSave={(nextBaseline, nextDraft) => {
        if (saving) return;
        void onSaved(nextBaseline, nextDraft);
      }}
      onCopy={onCopy}
    />
  );
}

function ClaudeSettingsForm({
  baseline,
  draft,
  port,
  catalog,
  saving,
  onDraft,
  onSave,
  onCopy,
}: {
  baseline: ClaudeSettingsDraft;
  draft: ClaudeSettingsDraft;
  port: number;
  catalog: CatalogPickerOption[];
  saving: boolean;
  onDraft: (draft: ClaudeSettingsDraft) => void;
  onSave: (baseline: ClaudeSettingsDraft, draft: ClaudeSettingsDraft) => void;
  onCopy: (value: string) => void;
}) {
  const t = useT();
  const [compactText, setCompactText] = useState(() => compactFieldValue(draft.autoCompactWindow));
  const compactParsed = compactInputToOverride(compactText, draft.autoCompactWindow);
  const compactValid = compactParsed !== "invalid";
  const liveDraft = compactValid ? { ...draft, autoCompactWindow: compactParsed } : draft;
  const dirty = compactValid && claudeSettingsDirty(baseline, liveDraft);
  const modelOptions = [
    { value: "", label: t("harnesses.claude.helperUnset") },
    ...catalog.map((option) => ({ value: option.value, label: modelLabel(option.value) })),
  ];
  const patch = (partial: Partial<ClaudeSettingsDraft>) => onDraft({ ...draft, ...partial });

  return (
    <form
      className="harnesses-claude-settings"
      onSubmit={(event) => {
        event.preventDefault();
        if (!dirty || saving || !compactValid) return;
        onSave(baseline, liveDraft);
      }}
    >
      <AuthSection
        draft={draft}
        patch={patch}
        t={t}
        actions={(
          <button
            type="submit"
            className={`btn btn-sm harnesses-claude-save${dirty ? " btn-primary" : " btn-ghost"}`}
            disabled={!dirty || saving}
          >
            {t("harnesses.claude.save")}
          </button>
        )}
      />
      <ContextSection
        draft={liveDraft}
        compactText={compactText}
        compactValid={compactValid}
        patch={patch}
        onCompactText={setCompactText}
        t={t}
      />
      <AgentsSection draft={draft} patch={patch} t={t} />
      <RoutingSection draft={draft} catalog={catalog} modelOptions={modelOptions} patch={patch} t={t} />
      <LaunchSection draft={liveDraft} port={port} onCopy={onCopy} t={t} />
    </form>
  );
}

function AuthSection({
  draft,
  patch,
  t,
  actions,
}: {
  draft: ClaudeSettingsDraft;
  patch: (partial: Partial<ClaudeSettingsDraft>) => void;
  t: TFn;
  actions?: ReactNode;
}) {
  return (
    <section className="harnesses-claude-section">
      <div className="harnesses-claude-section-head">
        <h4>{t("harnesses.claude.auth.title")}</h4>
        {actions}
      </div>
      <SettingsRow
        label={t("harnesses.claude.auth.mode")}
        hint={t("harnesses.claude.auth.modeHint")}
        control={(
          <Select
            value={draft.authMode}
            options={[
              { value: "auto", label: t("harnesses.claude.auth.auto") },
              { value: "subscription", label: t("harnesses.claude.auth.subscription") },
              { value: "proxy", label: t("harnesses.claude.auth.proxy") },
            ]}
            onChange={(value) => patch({ authMode: value as ClaudeAuthMode })}
            label={t("harnesses.claude.auth.mode")}
            chevron="down"
            align="left"
          />
        )}
      />
    </section>
  );
}

function ContextSection({
  draft,
  compactText,
  compactValid,
  patch,
  onCompactText,
  t,
}: {
  draft: ClaudeSettingsDraft;
  compactText: string;
  compactValid: boolean;
  patch: (partial: Partial<ClaudeSettingsDraft>) => void;
  onCompactText: (value: string) => void;
  t: TFn;
}) {
  return (
    <section className="harnesses-claude-section">
      <h4>{t("harnesses.claude.context.title")}</h4>
      <SettingsRow
        label={t("harnesses.claude.context.auto")}
        hint={t("harnesses.claude.context.autoHint")}
        control={(
          <Switch
            on={draft.autoContext}
            onClick={() => patch({ autoContext: !draft.autoContext })}
            label={t("harnesses.claude.context.auto")}
          />
        )}
      />
      <SettingsRow
        label={t("harnesses.claude.context.compact")}
        hint={t("harnesses.claude.context.compactHint")}
        control={(
          <CompactField
            text={compactText}
            inherited={draft.autoCompactWindow == null}
            invalid={!compactValid}
            onText={onCompactText}
            onReset={() => {
              patch({ autoCompactWindow: null });
              onCompactText(compactFieldValue(null));
            }}
          />
        )}
      />
    </section>
  );
}

function AgentsSection({
  draft,
  patch,
  t,
}: {
  draft: ClaudeSettingsDraft;
  patch: (partial: Partial<ClaudeSettingsDraft>) => void;
  t: TFn;
}) {
  return (
    <section className="harnesses-claude-section">
      <h4>{t("harnesses.claude.agents.title")}</h4>
      <SettingsRow
        label={t("harnesses.claude.agents.inject")}
        hint={t("harnesses.claude.agents.injectHint")}
        extra={(
          <button type="button" className="providers-link" onClick={() => navigateHash("subagents")}>
            {t("harnesses.claude.agents.manage")}
          </button>
        )}
        control={(
          <Switch
            on={draft.injectAgents}
            onClick={() => patch({ injectAgents: !draft.injectAgents })}
            label={t("harnesses.claude.agents.inject")}
          />
        )}
      />
    </section>
  );
}

function RoutingSection({
  draft,
  catalog,
  modelOptions,
  patch,
  t,
}: {
  draft: ClaudeSettingsDraft;
  catalog: CatalogPickerOption[];
  modelOptions: { value: string; label: ReactNode }[];
  patch: (partial: Partial<ClaudeSettingsDraft>) => void;
  t: TFn;
}) {
  return (
    <section className="harnesses-claude-section">
      <h4>{t("harnesses.claude.routing.title")}</h4>
      <p className="harnesses-claude-lede">{t("harnesses.claude.routing.hint")}</p>
      <SettingsRow
        label={t("harnesses.claude.helper")}
        hint={t("harnesses.claude.helperHint")}
        className="harnesses-claude-row--helper"
        control={(
          <Select
            value={pickerValue(draft.smallFastModel, catalog)}
            options={modelOptions}
            onChange={(value) => patch({ smallFastModel: value })}
            label={t("harnesses.claude.helper")}
            chevron="down"
            align="left"
          />
        )}
      />
      <p className="harnesses-claude-families">{t("harnesses.claude.families")}</p>
      {([
        ["opus", t("harnesses.claude.family.opus")],
        ["sonnet", t("harnesses.claude.family.sonnet")],
        ["haiku", t("harnesses.claude.family.haiku")],
        ["fable", t("harnesses.claude.family.fable")],
      ] as const).map(([key, label]) => (
        <SettingsRow
          key={key}
          label={label}
          dense
          control={(
            <Select
              value={pickerValue(draft[key], catalog)}
              options={modelOptions}
              onChange={(value) => patch({ [key]: value })}
              label={label}
              chevron="down"
              align="left"
            />
          )}
        />
      ))}
    </section>
  );
}

function LaunchSection({
  draft,
  port,
  onCopy,
  t,
}: {
  draft: ClaudeSettingsDraft;
  port: number;
  onCopy: (value: string) => void;
  t: TFn;
}) {
  return (
    <section className="harnesses-claude-section">
      <h4>{t("harnesses.claude.launch.title")}</h4>
      <div className="harnesses-claude-launch-head">
        <p className="harnesses-claude-lede">{t("harnesses.claude.launch.hint")}</p>
      </div>
      <div className="harnesses-claude-command">
        <code>benes claude</code>
        <button type="button" className="btn btn-ghost btn-sm" onClick={() => onCopy("benes claude")}>
          <IconCopy />
          {t("harnesses.copy")}
        </button>
      </div>
      <details className="harnesses-claude-manual">
        <summary>{t("harnesses.claude.launch.manual")}</summary>
        <pre><code>{manualSetupLines(draft, port).join("\n")}</code></pre>
      </details>
    </section>
  );
}

function SettingsRow({
  label,
  hint,
  extra,
  control,
  dense,
  className,
}: {
  label: string;
  hint?: string;
  extra?: ReactNode;
  control: ReactNode;
  dense?: boolean;
  className?: string;
}) {
  const hintId = useId();
  return (
    <div className={`harnesses-claude-row${dense ? " harnesses-claude-row--family" : ""}${className ? ` ${className}` : ""}`}>
      <div className="harnesses-claude-copy">
        <div className="harnesses-claude-label">{label}</div>
        {hint && <p id={hintId} className="harnesses-claude-hint">{hint}</p>}
        {extra}
      </div>
      <div className="harnesses-claude-control">{control}</div>
    </div>
  );
}

function CompactField({
  text,
  inherited,
  invalid,
  onText,
  onReset,
}: {
  text: string;
  inherited: boolean;
  invalid: boolean;
  onText: (value: string) => void;
  onReset: () => void;
}) {
  const t = useT();
  const statusId = useId();
  return (
    <div className="harnesses-claude-compact">
      <input
        className="input"
        inputMode="numeric"
        value={text}
        aria-invalid={invalid}
        aria-describedby={statusId}
        onChange={(event) => onText(event.target.value)}
      />
      {invalid ? (
        <p id={statusId} className="harnesses-claude-compact-error">{t("harnesses.claude.context.compactInvalid")}</p>
      ) : inherited ? (
        <span id={statusId} className="harnesses-claude-compact-hint">{t("harnesses.claude.context.compactDefault")}</span>
      ) : (
        <button type="button" id={statusId} className="providers-link providers-link--plain" onClick={onReset}>
          {t("harnesses.claude.context.compactReset")}
        </button>
      )}
    </div>
  );
}
