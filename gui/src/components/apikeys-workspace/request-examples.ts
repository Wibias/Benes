/**
 * Benes dashboard source. The curl samples on the Examples tab.
 *
 * Kept as a pure builder so the secret-safety rule is checkable without
 * rendering: every sample authenticates with a literal placeholder, and no
 * caller ever passes a live or newly-created key in. A test asserts the
 * placeholder survives even while the page is holding a one-time secret.
 */
import type { TFn, TKey } from "../../i18n/shared";
import type { ApiEndpointInfo } from "../../api-access/endpoints";

/**
 * What every sample sends as its credential. A reader replaces it with their
 * own key; the dashboard never substitutes one it happens to be holding.
 */
export const EXAMPLE_KEY_PLACEHOLDER = "benes_YOUR_KEY_HERE";

const MODELS = {
  chat: "gpt-5.4",
  responses: "gpt-5.4",
  messages: "claude-sonnet-4-6",
} as const;

const MESSAGES_MAX_TOKENS = 64;

export interface RequestExample {
  id: "chat" | "responses" | "messages";
  titleKey: TKey;
  text: string;
}

/**
 * Sample assembly.
 *
 * These fragments are protocol, not copy, so they are composed from named
 * pieces instead of one prose template: a reader can see the shape of the
 * request without parsing a multi-line literal, and the resolved text stays
 * byte-stable per locale.
 */
const OPEN_BRACE = "'{";
const CLOSE_BRACE = "  }'";
const INDENT = "    ";
const HEADER_INDENT = "  ";
const CONTINUATION = " \\";
const JSON_CREDENTIAL = '-H "x-benes-api-key: ' + EXAMPLE_KEY_PLACEHOLDER + '"';
const JSON_CONTENT_TYPE = '-H "Content-Type: application/json"';
const USER_TURN_PREFIX = '"messages": [{"role": "user", "content": ';
const USER_TURN_SUFFIX = "}]";

function jsonBody(members: string[]): string {
  const body = members.map(member => INDENT + member).join(",\n");
  return OPEN_BRACE + "\n" + body + "\n" + CLOSE_BRACE;
}

function curl(url: string, payload: string): string {
  return [
    "curl " + url + CONTINUATION,
    HEADER_INDENT + JSON_CREDENTIAL + CONTINUATION,
    HEADER_INDENT + JSON_CONTENT_TYPE + CONTINUATION,
    HEADER_INDENT + "-d " + payload,
  ].join("\n");
}

function modelMember(model: string): string {
  return '"model": "' + model + '"';
}

/**
 * The enabled examples, in the order the tab prints them.
 *
 * `includeMessages` mirrors whether the Messages surface is reachable: the
 * listener only accepts `/v1/messages` while the Claude harness is enabled, so
 * the sample is withheld rather than shown as a request that would be refused.
 */
export function buildRequestExamples(options: {
  endpoints: ApiEndpointInfo;
  /** Already JSON-quoted, so the sample stays valid whatever the locale says. */
  sampleInput: string;
  includeMessages: boolean;
}): RequestExample[] {
  const { endpoints, sampleInput, includeMessages } = options;
  const userTurn = USER_TURN_PREFIX + sampleInput + USER_TURN_SUFFIX;
  const examples: RequestExample[] = [
    {
      id: "chat",
      titleKey: "api.usageChatTitle",
      text: curl(endpoints.chatCompletions, jsonBody([modelMember(MODELS.chat), userTurn])),
    },
    {
      id: "responses",
      titleKey: "api.usageResponsesTitle",
      text: curl(endpoints.responses, jsonBody([modelMember(MODELS.responses), '"input": ' + sampleInput])),
    },
  ];
  if (includeMessages) {
    examples.push({
      id: "messages",
      titleKey: "api.usageMessagesTitle",
      text: curl(endpoints.messages, jsonBody([
        modelMember(MODELS.messages),
        '"max_tokens": ' + MESSAGES_MAX_TOKENS,
        userTurn,
      ])),
    });
  }
  return examples;
}

/** One sample as the Examples tab prints it: a resolved heading and the bytes. */
export interface RequestExampleView {
  readonly id: RequestExample["id"];
  readonly title: string;
  readonly text: string;
}

export interface ExamplesPanelView {
  readonly title: string;
  readonly intro: string;
  readonly examples: readonly RequestExampleView[];
}

/**
 * The Examples tab's own model: section copy, and each sample with its heading
 * already resolved.
 *
 * The panel renders this and nothing else, so the tab never reaches for the
 * one-time key and never decides for itself which surfaces exist. The localized
 * sample input is quoted here, which is why the heading is resolved alongside
 * it rather than in the component.
 */
export function deriveExamplesPanel(
  endpoints: ApiEndpointInfo,
  includeMessages: boolean,
  t: TFn,
): ExamplesPanelView {
  const examples = buildRequestExamples({
    endpoints,
    sampleInput: JSON.stringify(t("api.usageSampleInput")),
    includeMessages,
  });
  return {
    title: t("api.section.examples"),
    intro: t("api.examplesIntro"),
    examples: examples.map(example => ({
      id: example.id,
      title: t(example.titleKey),
      text: example.text,
    })),
  };
}

