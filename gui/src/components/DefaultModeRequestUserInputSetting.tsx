/**
 * Benes dashboard client for the Go proxy (`internal/server`).
 * Codex Auth card for Codex's own `default_mode_request_user_input` flag.
 *
 * Three collaborators, one job each: `useCodexRequestUserInputFlag` owns transport, polling and
 * the optimistic flip, `codex-feature-flags` owns what a response *means* (including the success
 * and failure sentences), and the request-user-input sections render each phase of the card.
 * Reads and writes go to the management API, which flips the flag in `$CODEX_HOME/config.toml`
 * through the official `codex features` CLI.
 */

import { useCodexRequestUserInputFlag } from "../hooks/useCodexRequestUserInputFlag";
import {
  RequestUserInputControls,
  RequestUserInputCopy,
  RequestUserInputFeedbackLine,
} from "./codex-request-user-input-sections";

export function DefaultModeRequestUserInputSetting({ apiBase }: { apiBase: string }) {
  const card = useCodexRequestUserInputFlag(apiBase);

  return (
    <div className="card card-row codex-request-user-input-card" aria-busy={card.busy || undefined}>
      <RequestUserInputCopy loadError={card.loadError} />
      <RequestUserInputControls
        enabled={card.enabled}
        locked={card.locked}
        showRetry={card.loadError}
        onRetry={card.refresh}
        onToggle={card.toggle}
      />
      {card.feedback && <RequestUserInputFeedbackLine feedback={card.feedback} />}
    </div>
  );
}

export default DefaultModeRequestUserInputSetting;
