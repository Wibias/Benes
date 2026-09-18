/** Flat v1 / Base / v2 agent-interface mode control for Sub-agents. */
import type { TFn, TKey } from "../../i18n/shared";
import type { MultiAgentMode } from "../../pages/subagents-delegation-contract.ts";

type ModeChip = {
  id: MultiAgentMode;
  labelKey: TKey;
};

const MODE_CHIPS: readonly ModeChip[] = [
  { id: "v1", labelKey: "models.v2Mode_v1" as TKey },
  { id: "default", labelKey: "models.v2Mode_default" as TKey },
  { id: "v2", labelKey: "models.v2Mode_v2" as TKey },
];

type AgentInterfaceProps = {
  t: TFn;
  mode: MultiAgentMode;
  busy: boolean;
  onChange: (mode: MultiAgentMode) => void;
};

function ModeChipButton(props: {
  chip: ModeChip;
  selected: boolean;
  busy: boolean;
  label: string;
  onPick: () => void;
}) {
  return (
    <button
      type="button"
      role="radio"
      aria-checked={props.selected}
      className={props.selected ? "subagents-mode-tab is-active" : "subagents-mode-tab"}
      disabled={props.busy}
      onClick={props.onPick}
    >
      {props.label}
    </button>
  );
}

export function SubagentsAgentInterface(props: AgentInterfaceProps) {
  const heading = props.t("sub.agentInterface");
  const modeLabel = props.t("sub.agentInterface.mode");
  return (
    <section className="subagents-agent-interface" aria-label={heading}>
      <h3 className="subagents-agent-interface-title">{heading}</h3>
      <div className="subagents-agent-interface-row">
        <span className="subagents-agent-interface-label">{modeLabel}</span>
        <div className="subagents-mode-tabs" role="radiogroup" aria-label={modeLabel}>
          {MODE_CHIPS.map((chip) => (
            <ModeChipButton
              key={chip.id}
              chip={chip}
              selected={props.mode === chip.id}
              busy={props.busy}
              label={props.t(chip.labelKey)}
              onPick={() => props.onChange(chip.id)}
            />
          ))}
        </div>
      </div>
    </section>
  );
}
