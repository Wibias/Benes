/** Canonical Proactive delegation text mirrored from codex-rs (multi_agent_mode_instructions.rs). */

export const ULTRA_MODE_PRESET =
  "Proactive multi-agent delegation is active. Any earlier instruction requiring an explicit user request before spawning sub-agents no longer applies. Delegate independent sub-tasks to sub-agents whenever parallel work would materially improve speed or quality - do not serialize work that can run concurrently. Each sub-agent runs in its own context and can use all available tools; prefer spawning specialists over doing everything yourself. This mode remains active until a later multi-agent mode developer message changes it.";
