/** Benes dashboard client for the Go proxy (`internal/server`). */
import type { TFn } from "../i18n/shared";

export function protocolOptionLabel(t: TFn, id: string): string {
  if (id === "responses") return t("sessions.protocol.responses");
  if (id === "chat_completions") return t("sessions.protocol.chat_completions");
  if (id === "anthropic_messages") return t("sessions.protocol.anthropic_messages");
  return id;
}
