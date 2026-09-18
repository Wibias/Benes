/** Benes dashboard client for the Go proxy (`internal/server`). */
import type { Literal, Node, Property, TemplateElement } from "estree";
import type { JSXAttribute, JSXText } from "estree-jsx";
import { isBrandOrModelLiteral, isTechnicalLiteral } from "./i18n-allowlist.ts";
import {
  isInsideNonUiContext,
  isInsideTrans,
  isTransProp,
  propertyKeyName,
} from "./i18n-non-ui-context.ts";
import { formatHardcodedSnippet, i18nLocaleFileHint } from "./i18n-locales.ts";

type LocalRuleContext = {
  report(descriptor: {
    node: Node;
    messageId: "uiString" | "dataCopy";
    data?: Record<string, string>;
  }): void;
};

type LocalRuleListener = {
  JSXText?(node: JSXText): void;
  JSXAttribute?(node: JSXAttribute): void;
  Literal?(node: Literal): void;
  TemplateElement?(node: TemplateElement): void;
  Property?(node: Property): void;
};

type LocalRuleModule = {
  meta: {
    type: "problem";
    docs: {
      description: string;
    };
    schema: readonly unknown[];
    messages: Record<string, string>;
  };
  create(context: LocalRuleContext): LocalRuleListener;
};

const LITERAL_PATTERN =
  /[A-Za-zÀ-ÖØ-öø-ÿ\u0100-\u024F\u1E00-\u1EFF\u0400-\u04FF\u3040-\u309F\u30A0-\u30FF\u4E00-\u9FFF\uAC00-\uD7AF]/u;

const HTML_CHARACTER_REFERENCE_PATTERN =
  /&(?:#\d+|#x[0-9A-Fa-f]+|[A-Za-z][A-Za-z0-9]+);/g;

const UI_ATTRS = new Set([
  "title",
  "placeholder",
  "aria-label",
  "aria-description",
  "aria-roledescription",
  "alt",
]);

const DATA_COPY_KEYS = new Set([
  "label",
  "title",
  "description",
  "placeholder",
  "message",
  "reason",
  "text",
  "summary",
  "subtitle",
  "helper",
  "empty",
  "hint",
]);

function isAllowedLiteral(value: string): boolean {
  const trimmed = value.trim();
  if (!trimmed) return true;
  if (!LITERAL_PATTERN.test(trimmed)) return true;
  if (isTechnicalLiteral(trimmed)) return true;
  if (isBrandOrModelLiteral(trimmed)) return true;
  return false;
}

function reportLiteral(
  context: LocalRuleContext,
  node: Node,
  value: string,
  messageId: "uiString" | "dataCopy",
  comparisonValue = value,
) {
  if (isAllowedLiteral(comparisonValue)) return;
  context.report({
    node,
    messageId,
    data: {
      snippet: formatHardcodedSnippet(value),
      locales: i18nLocaleFileHint(),
    },
  });
}

const noHardcodedUiStrings: LocalRuleModule = {
  meta: {
    type: "problem",
    docs: {
      description: "Disallow hardcoded user-facing UI strings; use src/i18n keys via useT/t/Trans.",
    },
    schema: [],
    messages: {
      uiString:
        'Hardcoded UI text: "{{snippet}}". Add a key to {{locales}} and render with t() or <Trans />. Company names and model ids are the only allowed literals.',
    },
  },
  create(context) {
    return {
      JSXText(node: JSXText) {
        if (isInsideNonUiContext(node)) return;
        const value = node.value.replace(/\s+/g, " ").trim();
        if (!value) return;

        const comparisonValue = value.replace(HTML_CHARACTER_REFERENCE_PATTERN, "");
        reportLiteral(context, node, value, "uiString", comparisonValue);
      },
      JSXAttribute(node: JSXAttribute) {
        if (node.name.type !== "JSXIdentifier") return;
        if (!UI_ATTRS.has(node.name.name)) return;
        const valueNode = node.value;
        if (!valueNode) return;
        if (valueNode.type === "Literal" && typeof valueNode.value === "string") {
          reportLiteral(context, valueNode, valueNode.value, "uiString");
          return;
        }
        if (valueNode.type === "JSXExpressionContainer") {
          const expr = valueNode.expression;
          if (expr.type === "Literal" && typeof expr.value === "string") {
            reportLiteral(context, expr, expr.value, "uiString");
          }
        }
      },
      Literal(node) {
        if (typeof node.value !== "string") return;
        const parent = (node as { parent?: Node }).parent;
        if (!parent) return;
        if (parent.type === "ImportDeclaration") return;
        if (isInsideTrans(node) || isTransProp(node) || isInsideNonUiContext(node)) return;
        if (parent.type === "JSXAttribute") return;
        if (parent.type === "JSXExpressionContainer") return;
        if (parent.type === "Property") {
          const key = propertyKeyName((parent as Property).key);
          if (key && DATA_COPY_KEYS.has(key)) {
            reportLiteral(context, node, node.value, "uiString");
          }
        }
      },
      TemplateElement(node: TemplateElement) {
        if (isInsideNonUiContext(node)) return;
        const raw = node.value.raw.replace(/\s+/g, " ").trim();
        if (!raw) return;
        reportLiteral(context, node, raw, "uiString");
      },
    };
  },
};

const noHardcodedDataCopy: LocalRuleModule = {
  meta: {
    type: "problem",
    docs: {
      description: "Disallow hardcoded label/title/description fields in data helpers.",
    },
    schema: [],
    messages: {
      dataCopy:
        'Hardcoded user-facing copy: "{{snippet}}". Use i18n keys (e.g. labelKey) in {{locales}} and resolve with t() at render time.',
    },
  },
  create(context) {
    return {
      Property(node: Property) {
        const key = propertyKeyName(node.key);
        if (!key || !DATA_COPY_KEYS.has(key)) return;
        const value = node.value;
        if (value.type === "Literal" && typeof value.value === "string") {
          reportLiteral(context, value, value.value, "dataCopy");
        }
      },
    };
  },
};

export default {
  rules: {
    "no-hardcoded-ui-strings": noHardcodedUiStrings,
    "no-hardcoded-data-copy": noHardcodedDataCopy,
  },
};
