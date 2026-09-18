/** Ancestor contexts where a string literal is machine chrome, not UI copy. */
import type { Node, Property } from "estree";
import type { JSXAttribute, JSXElement } from "estree-jsx";

const CODE_SAMPLE_TAGS = new Set(["pre", "code", "samp", "kbd"]);

const NON_UI_JSX_ATTRS = new Set([
  "className",
  "class",
  "style",
  "id",
  "key",
  "htmlFor",
  "for",
  "type",
  "name",
  "href",
  "src",
  "srcSet",
  "rel",
  "target",
  "method",
  "tabIndex",
  "role",
  "width",
  "height",
  "viewBox",
  "fill",
  "stroke",
  "d",
  "path",
  "xmlns",
]);

/** Visible aria copy still goes through the UI-string rule. */
const UI_ARIA_ATTR_NAMES = new Set([
  "aria-label",
  "aria-description",
  "aria-roledescription",
]);

const NON_UI_OBJECT_KEYS = new Set([
  "gridTemplateColumns",
  "gridTemplateRows",
  "gridColumn",
  "gridRow",
  "gridArea",
  "background",
  "backgroundImage",
  "transform",
  "transition",
  "animation",
]);

const NON_UI_CALLEES = new Set([
  "fetch",
  "encodeURIComponent",
  "decodeURIComponent",
  "JSON.stringify",
  "String",
  "URL",
]);

type ParentedNode = Node & { parent?: Node };

export function isCodeSampleTag(tag: string | null): boolean {
  return tag !== null && CODE_SAMPLE_TAGS.has(tag);
}

export function isNonUiJsxAttributeName(name: string): boolean {
  if (NON_UI_JSX_ATTRS.has(name)) return true;
  if (name.startsWith("data-")) return true;
  if (name === "title") return false;
  if (name.startsWith("aria-")) return !UI_ARIA_ATTR_NAMES.has(name);
  return false;
}

export function isNonUiCssPropertyName(key: string): boolean {
  return NON_UI_OBJECT_KEYS.has(key);
}

export function isNonUiCalleeIdentifier(name: string): boolean {
  return NON_UI_CALLEES.has(name);
}

function jsxElementName(node: JSXElement): string | null {
  const name = node.openingElement.name;
  if (name.type === "JSXIdentifier") return name.name;
  return null;
}

export function propertyKeyName(key: Property["key"]): string | null {
  if (key.type === "Identifier") return key.name;
  if (key.type === "Literal" && typeof key.value === "string") return key.value;
  return null;
}

export function isInsideTrans(node: Node): boolean {
  let current: Node | undefined = node;
  while (current) {
    if (current.type === "JSXElement") {
      const opening = (current as JSXElement).openingElement;
      const name = opening.name;
      if (name.type === "JSXIdentifier" && name.name === "Trans") return true;
    }
    current = (current as ParentedNode).parent;
  }
  return false;
}

export function isTransProp(node: Node): boolean {
  const parent = (node as ParentedNode).parent;
  if (!parent || parent.type !== "JSXAttribute") return false;
  const attr = parent as JSXAttribute;
  if (attr.name.type !== "JSXIdentifier") return false;
  return attr.name.name === "k" || attr.name.name === "cmd";
}

export function isInsideTCall(node: Node): boolean {
  let current: Node | undefined = node;
  while (current) {
    if (current.type === "CallExpression") {
      const callee = (current as { callee: Node }).callee;
      if (callee.type === "Identifier" && callee.name === "t") return true;
    }
    current = (current as ParentedNode).parent;
  }
  return false;
}

function matchesNonUiAncestor(current: Node): boolean {
  if (current.type === "JSXElement") {
    return isCodeSampleTag(jsxElementName(current as JSXElement));
  }
  if (current.type === "JSXAttribute") {
    const attr = current as JSXAttribute;
    if (attr.name.type !== "JSXIdentifier") return false;
    return isNonUiJsxAttributeName(attr.name.name);
  }
  if (current.type === "Property") {
    const key = propertyKeyName((current as Property).key);
    return key !== null && isNonUiCssPropertyName(key);
  }
  if (current.type === "CallExpression") {
    const callee = (current as { callee: Node }).callee;
    return callee.type === "Identifier" && isNonUiCalleeIdentifier(callee.name);
  }
  return false;
}

/**
 * True when the node is inside non-UI context: t()/Trans, code/pre samples,
 * style/url attrs, fetch URLs, etc.
 */
export function isInsideNonUiContext(node: Node): boolean {
  if (isInsideTCall(node) || isInsideTrans(node)) return true;
  let current: Node | undefined = node;
  while (current) {
    if (matchesNonUiAncestor(current)) return true;
    current = (current as ParentedNode).parent;
  }
  return false;
}
