/** Deterministic translation-key liveness analysis for the Benes dashboard. */
import { readdir, readFile, writeFile } from "node:fs/promises";
import { createRequire } from "node:module";
import path from "node:path";
import { fileURLToPath } from "node:url";

import {
  normalizeModulePath,
  reachableRuntimeModules,
  viteRuntimeEntryPoints,
  type SourceModule,
} from "./i18n-module-graph.ts";

const require = createRequire(import.meta.url);
const ts = require("typescript") as typeof import("typescript");

export type KeyStatus =
  | "LIVE_STATIC"
  | "LIVE_TYPED_REFERENCE"
  | "LIVE_CONSERVATIVE_LITERAL"
  | "LIVE_SPECIAL_CATALOG"
  | "SOURCE_ONLY_REFERENCE"
  | "UNUSED_PROVEN"
  | "UNRESOLVED_DYNAMIC";

export type KeyEvidence = {
  status: Exclude<KeyStatus, "UNUSED_PROVEN">;
  path: string;
  line: number;
  kind: string;
  detail?: string;
};

export type KeyRecord = {
  status: KeyStatus;
  evidence: KeyEvidence[];
};

export type LivenessReport = {
  keys: Record<string, KeyRecord>;
  unresolved: KeyEvidence[];
  unboundedCalls: KeyEvidence[];
  analysisErrors: string[];
};

export type AnalyzeOptions = {
  canonicalKeys: readonly string[];
  files: Array<{ path: string; source: string }>;
  catalogPaths?: readonly string[];
  entryPoints?: readonly string[];
  program?: ts.Program;
};

export const TRANSLATED_LOCALES = ["de", "fr", "ko", "zh", "zh-TW", "ru", "ja", "tr"] as const;

export const MEMORY_OBSERVABILITY_KEYS = [
  "dash.mem.title",
  "dash.mem.hint",
  "dash.mem.rss",
  "dash.mem.jsHeap",
  "dash.mem.jsHeapArena",
  "dash.mem.pressure",
  "dash.mem.pressureOf",
  "dash.mem.pressureUnknown",
  "dash.mem.jscHeap",
  "dash.mem.external",
  "dash.mem.arrayBuffers",
  "dash.mem.observed",
  "dash.mem.runtime",
  "dash.mem.growth",
  "dash.mem.perHour",
  "dash.mem.store",
  "dash.mem.storeHint",
  "dash.mem.storeEntries",
  "dash.mem.storeTotal",
  "dash.mem.storeLargest",
  "dash.mem.storeOldest",
  "dash.mem.threshold",
  "dash.mem.lastWarn",
  "dash.mem.never",
  "dash.mem.details",
  "dash.mem.unavailable",
  "dash.mem.inFlight",
  "dash.mem.restart",
  "dash.mem.restartConfirm",
  "dash.mem.draining",
  "dash.mem.reconnecting",
  "dash.mem.restartFailed",
  "dash.mem.restartNoSupervisor",
] as const;

const TKEY_FIELDS = new Set([
  "nameKey",
  "labelKey",
  "noteKey",
  "tkey",
  "messageKey",
  "errorKey",
  "hintKey",
  "copiedKey",
  "methodKey",
  "k",
]);

const TRANSLATE_CALLEES = new Set(["t", "catalogValue"]);
const PASS_THROUGH_FUNCTIONS = new Set(["Trans", "catalogValue"]);

function scriptKindFor(filePath: string): ts.ScriptKind {
  if (filePath.endsWith(".tsx") || filePath.endsWith(".jsx")) return ts.ScriptKind.TSX;
  return ts.ScriptKind.TS;
}

function lineOf(sourceFile: ts.SourceFile, position: number): number {
  return sourceFile.getLineAndCharacterOfPosition(position).line + 1;
}

function propertyName(node: ts.ObjectLiteralElementLike): string | null {
  if (!ts.isPropertyAssignment(node) && !ts.isShorthandPropertyAssignment(node)) return null;
  const name = node.name;
  if (ts.isIdentifier(name) || ts.isPrivateIdentifier(name)) return name.text;
  if (ts.isStringLiteral(name) || ts.isNoSubstitutionTemplateLiteral(name)) return name.text;
  if (ts.isNumericLiteral(name)) return name.text;
  return null;
}

function typeNameIsTKey(typeNode: ts.TypeNode | undefined): boolean {
  if (!typeNode) return false;
  if (ts.isTypeReferenceNode(typeNode) && ts.isIdentifier(typeNode.typeName)) {
    return typeNode.typeName.text === "TKey";
  }
  return false;
}

function isTKeyAssertion(node: ts.Node): boolean {
  if (ts.isAsExpression(node) || ts.isTypeAssertionExpression(node) || ts.isSatisfiesExpression(node)) {
    return typeNameIsTKey(node.type);
  }
  return false;
}

function enclosingCall(node: ts.Node): ts.CallExpression | null {
  let current: ts.Node | undefined = node.parent;
  let child: ts.Node = node;
  while (current) {
    if (ts.isCallExpression(current)) {
      if (current.expression === child || isNodeInside(current.expression, node)) return null;
      return current;
    }
    child = current;
    current = current.parent;
  }
  return null;
}

function translateKeyArgument(call: ts.CallExpression, name: string): ts.Expression | undefined {
  if (name === "t") return call.arguments[0];
  if (name === "catalogValue") return call.arguments[1];
  return undefined;
}

function inTranslateKeyPosition(node: ts.Node): string | null {
  const call = enclosingCall(node);
  if (!call) return null;
  const name = calleeName(call);
  if (!name || !TRANSLATE_CALLEES.has(name)) return null;
  const argument = translateKeyArgument(call, name);
  if (!argument || !isNodeInside(argument, node)) return null;
  return name;
}

function isNodeInside(root: ts.Node, target: ts.Node): boolean {
  let current: ts.Node | undefined = target;
  while (current) {
    if (current === root) return true;
    current = current.parent;
  }
  return false;
}

function calleeName(call: ts.CallExpression): string | null {
  const expr = call.expression;
  if (ts.isIdentifier(expr)) return expr.text;
  if (ts.isPropertyAccessExpression(expr) && ts.isIdentifier(expr.name)) return expr.name.text;
  return null;
}

function enclosingJsxAttributeName(node: ts.Node): string | null {
  let current: ts.Node | undefined = node.parent;
  while (current) {
    if (ts.isJsxAttribute(current)) {
      return ts.isIdentifier(current.name) ? current.name.text : null;
    }
    current = current.parent;
  }
  return null;
}

function enclosingPropertyName(node: ts.Node): string | null {
  let current: ts.Node | undefined = node.parent;
  while (current) {
    if (ts.isPropertyAssignment(current)) return propertyName(current);
    current = current.parent;
  }
  return null;
}

function hasTKeyAssertionAncestor(node: ts.Node): boolean {
  let current: ts.Node | undefined = node.parent;
  while (current) {
    if (isTKeyAssertion(current)) return true;
    if (ts.isCallExpression(current) || ts.isJsxAttribute(current) || ts.isPropertyAssignment(current)) break;
    current = current.parent;
  }
  return false;
}

function enclosingFunction(node: ts.Node): ts.FunctionLikeDeclaration | null {
  let current: ts.Node | undefined = node.parent;
  while (current) {
    if (
      ts.isFunctionDeclaration(current) ||
      ts.isFunctionExpression(current) ||
      ts.isArrowFunction(current) ||
      ts.isMethodDeclaration(current)
    ) {
      return current;
    }
    current = current.parent;
  }
  return null;
}

function functionName(fn: ts.FunctionLikeDeclaration): string | null {
  if (fn.name && ts.isIdentifier(fn.name)) return fn.name.text;
  if (fn.parent && ts.isVariableDeclaration(fn.parent) && ts.isIdentifier(fn.parent.name)) {
    return fn.parent.name.text;
  }
  return null;
}

function isPassThroughTranslator(call: ts.CallExpression): boolean {
  const fn = enclosingFunction(call);
  if (!fn) return false;
  const name = functionName(fn);
  if (name && PASS_THROUGH_FUNCTIONS.has(name)) return true;
  if (name === "t") {
    const type = fn.parent && ts.isVariableDeclaration(fn.parent) ? fn.parent.type : undefined;
    if (typeNameIsTKey(type)) return true;
    if (type && ts.isTypeReferenceNode(type) && ts.isIdentifier(type.typeName) && type.typeName.text === "TFn") {
      return true;
    }
  }
  return false;
}

function evidence(
  status: KeyEvidence["status"],
  filePath: string,
  sourceFile: ts.SourceFile,
  node: ts.Node,
  kind: string,
  detail?: string,
): KeyEvidence {
  return { status, path: filePath, line: lineOf(sourceFile, node.getStart(sourceFile)), kind, detail };
}

function addEvidence(bucket: Map<string, KeyEvidence[]>, key: string, item: KeyEvidence): void {
  const list = bucket.get(key);
  if (list) list.push(item);
  else bucket.set(key, [item]);
}

function matchesTemplate(key: string, prefix: string, suffix: string, middles: readonly string[]): boolean {
  if (!key.startsWith(prefix) || !key.endsWith(suffix)) return false;
  if (key.length < prefix.length + suffix.length) return false;
  let cursor = prefix.length;
  const end = key.length - suffix.length;
  for (const middle of middles) {
    const index = key.indexOf(middle, cursor);
    if (index < 0 || index > end) return false;
    cursor = index + middle.length;
  }
  return cursor <= end;
}

function templatePieces(node: ts.TemplateExpression): { prefix: string; suffix: string; middles: string[] } {
  const prefix = node.head.text;
  const spans = node.templateSpans;
  const middles = spans.slice(0, -1).map((span) => span.literal.text);
  const suffix = spans.length > 0 ? spans[spans.length - 1]!.literal.text : "";
  return { prefix, suffix, middles };
}

function rank(status: KeyStatus): number {
  switch (status) {
    case "LIVE_STATIC":
      return 5;
    case "LIVE_TYPED_REFERENCE":
      return 4;
    case "LIVE_SPECIAL_CATALOG":
      return 3;
    case "LIVE_CONSERVATIVE_LITERAL":
      return 2;
    case "SOURCE_ONLY_REFERENCE":
      return 1;
    default:
      return 0;
  }
}

function finalizeStatus(evidenceList: KeyEvidence[]): KeyStatus {
  if (evidenceList.length === 0) return "UNUSED_PROVEN";
  const runtime = evidenceList.filter(
    (item) => item.status !== "UNRESOLVED_DYNAMIC" && item.status !== "SOURCE_ONLY_REFERENCE",
  );
  if (runtime.length > 0) {
    let status: KeyStatus = runtime[0]!.status;
    for (const item of runtime) {
      if (rank(item.status) > rank(status)) status = item.status;
    }
    return status;
  }
  if (evidenceList.some((item) => item.status === "UNRESOLVED_DYNAMIC")) return "UNRESOLVED_DYNAMIC";
  if (evidenceList.some((item) => item.status === "SOURCE_ONLY_REFERENCE")) return "SOURCE_ONLY_REFERENCE";
  return "UNUSED_PROVEN";
}

function unwrapExpression(expr: ts.Expression): ts.Expression {
  let current = expr;
  while (
    ts.isAsExpression(current) ||
    ts.isTypeAssertionExpression(current) ||
    ts.isSatisfiesExpression(current) ||
    ts.isParenthesizedExpression(current) ||
    ts.isNonNullExpression(current)
  ) {
    current = current.expression;
  }
  return current;
}

function stringLiteralText(expr: ts.Expression): string | null {
  const inner = unwrapExpression(expr);
  if (ts.isStringLiteral(inner) || ts.isNoSubstitutionTemplateLiteral(inner)) return inner.text;
  return null;
}

type ResolvedKeys =
  | { kind: "finite"; keys: string[]; via: string }
  | { kind: "template"; prefix: string; suffix: string; middles: string[] }
  | { kind: "unbounded"; via: string };

function uniqueKeys(keys: string[], canonical: ReadonlySet<string>): string[] {
  return [...new Set(keys.filter((key) => canonical.has(key)))];
}

function literalTypesFromType(type: ts.Type, checker: ts.TypeChecker, canonical: ReadonlySet<string>): string[] | null {
  const flags = type.flags;
  if (flags & ts.TypeFlags.StringLiteral) {
    const value = (type as ts.StringLiteralType).value;
    return canonical.has(value) ? [value] : [];
  }
  if (flags & ts.TypeFlags.Union) {
    const parts: string[] = [];
    for (const member of (type as ts.UnionType).types) {
      const got = literalTypesFromType(member, checker, canonical);
      if (got === null) return null;
      parts.push(...got);
    }
    const unique = uniqueKeys(parts, canonical);
    if (unique.length === canonical.size && canonical.size > 1) return null;
    return unique;
  }
  if (flags & ts.TypeFlags.EnumLiteral) {
    const value = checker.typeToString(type).replace(/^.*\./, "");
    return canonical.has(value) ? [value] : [];
  }
  if (flags & (ts.TypeFlags.Any | ts.TypeFlags.Unknown | ts.TypeFlags.String | ts.TypeFlags.Never)) {
    return null;
  }
  const symbol = type.aliasSymbol ?? type.symbol;
  if (symbol?.getName() === "TKey") return null;
  return [];
}

function collectObjectLiteralStringValues(node: ts.ObjectLiteralExpression, canonical: ReadonlySet<string>): string[] {
  const keys: string[] = [];
  for (const property of node.properties) {
    if (!ts.isPropertyAssignment(property)) continue;
    const text = stringLiteralText(property.initializer);
    if (text && canonical.has(text)) keys.push(text);
  }
  return keys;
}

function isNestedFunctionLike(node: ts.Node): boolean {
  return (
    ts.isFunctionDeclaration(node) ||
    ts.isFunctionExpression(node) ||
    ts.isArrowFunction(node) ||
    ts.isMethodDeclaration(node) ||
    ts.isConstructorDeclaration(node) ||
    ts.isGetAccessorDeclaration(node) ||
    ts.isSetAccessorDeclaration(node)
  );
}

type FieldValues = Map<string, { keys: string[]; unbounded: boolean }>;

function isNonKeyExpression(expr: ts.Expression): boolean {
  const inner = unwrapExpression(expr);
  if (inner.kind === ts.SyntaxKind.NullKeyword) return true;
  if (inner.kind === ts.SyntaxKind.TrueKeyword || inner.kind === ts.SyntaxKind.FalseKeyword) return true;
  if (ts.isIdentifier(inner) && (inner.text === "undefined" || inner.text === "null")) return true;
  return false;
}

function collectReturnKeys(
  fn: ts.FunctionLikeDeclaration,
  checker: ts.TypeChecker,
  canonical: ReadonlySet<string>,
  depth: number,
  fieldValues: FieldValues,
): ResolvedKeys {
  if (!fn.body) return { kind: "unbounded", via: "fn-return" };
  const keys: string[] = [];
  let unbounded = false;

  const consider = (expr: ts.Expression): void => {
    if (isNonKeyExpression(expr)) return;
    const type = checker.getTypeAtLocation(expr);
    if (type.flags & ts.TypeFlags.Never) return;
    const resolved = resolveExpression(expr, checker, canonical, depth + 1, fieldValues);
    if (resolved.kind === "unbounded" || resolved.kind === "template") {
      unbounded = true;
      return;
    }
    keys.push(...resolved.keys);
  };

  if (!ts.isBlock(fn.body)) {
    consider(fn.body);
    if (unbounded) return { kind: "unbounded", via: "fn-return" };
    return { kind: "finite", keys: uniqueKeys(keys, canonical), via: "fn-return" };
  }

  const visit = (node: ts.Node): void => {
    if (node !== fn.body && isNestedFunctionLike(node)) return;
    if (ts.isReturnStatement(node) && node.expression) consider(node.expression);
    ts.forEachChild(node, visit);
  };
  visit(fn.body);
  if (unbounded) return { kind: "unbounded", via: "fn-return" };
  return { kind: "finite", keys: uniqueKeys(keys, canonical), via: "fn-return" };
}

function aliasedSymbol(checker: ts.TypeChecker, symbol: ts.Symbol | undefined): ts.Symbol | undefined {
  if (!symbol) return undefined;
  return symbol.flags & ts.SymbolFlags.Alias ? checker.getAliasedSymbol(symbol) : symbol;
}

function collectNewMapValues(expr: ts.Expression, canonical: ReadonlySet<string>): string[] | null {
  const inner = unwrapExpression(expr);
  if (!ts.isNewExpression(inner)) return null;
  const ctor = unwrapExpression(inner.expression);
  if (!ts.isIdentifier(ctor) || ctor.text !== "Map") return null;
  const arg = inner.arguments?.[0];
  if (!arg || !ts.isArrayLiteralExpression(arg)) return null;
  const keys: string[] = [];
  for (const element of arg.elements) {
    if (!ts.isArrayLiteralExpression(element) || element.elements.length < 2) return null;
    const text = stringLiteralText(element.elements[1]!);
    if (text && canonical.has(text)) keys.push(text);
    else return null;
  }
  return keys;
}

function resolveConstMapValues(
  expr: ts.Expression,
  checker: ts.TypeChecker,
  canonical: ReadonlySet<string>,
): string[] | null {
  const inner = unwrapExpression(expr);
  if (ts.isObjectLiteralExpression(inner)) {
    const keys = collectObjectLiteralStringValues(inner, canonical);
    return keys.length > 0 ? keys : [];
  }
  const fromNew = collectNewMapValues(inner, canonical);
  if (fromNew) return fromNew;
  const symbol = aliasedSymbol(checker, checker.getSymbolAtLocation(inner));
  const decl = symbol?.valueDeclaration;
  if (decl && ts.isVariableDeclaration(decl) && decl.initializer) {
    if (ts.isObjectLiteralExpression(decl.initializer)) {
      const keys = collectObjectLiteralStringValues(decl.initializer, canonical);
      return keys.length > 0 ? keys : [];
    }
    return collectNewMapValues(decl.initializer, canonical);
  }
  return null;
}

function resolveFromSymbol(
  symbol: ts.Symbol,
  checker: ts.TypeChecker,
  canonical: ReadonlySet<string>,
  depth: number,
  fieldValues: FieldValues,
): ResolvedKeys | null {
  const declarations = symbol.getDeclarations() ?? [];
  const keys: string[] = [];
  for (const decl of declarations) {
    if (ts.isVariableDeclaration(decl) && decl.initializer) {
      const resolved = resolveExpression(decl.initializer, checker, canonical, depth + 1, fieldValues);
      if (!resolved) continue;
      if (resolved.kind === "unbounded" || resolved.kind === "template") return resolved;
      keys.push(...resolved.keys);
    }
    if (ts.isFunctionLike(decl)) {
      const returned = collectReturnKeys(decl, checker, canonical, depth, fieldValues);
      if (returned.kind === "unbounded" || returned.kind === "template") return returned;
      keys.push(...returned.keys);
    }
    if (ts.isPropertyAssignment(decl)) {
      const text = stringLiteralText(decl.initializer);
      if (text && canonical.has(text)) keys.push(text);
    }
  }
  if (keys.length > 0) return { kind: "finite", keys: uniqueKeys(keys, canonical), via: "symbol" };
  return null;
}

function collectCallSiteArgumentKeys(
  param: ts.ParameterDeclaration,
  fn: ts.FunctionLikeDeclaration,
  checker: ts.TypeChecker,
  canonical: ReadonlySet<string>,
  depth: number,
  fieldValues: FieldValues,
  propName?: string,
): ResolvedKeys | null {
  const fnName = functionName(fn);
  const paramName = propName ?? (ts.isIdentifier(param.name) ? param.name.text : null);
  if (!fnName) return null;
  const index = fn.parameters.indexOf(param);
  const sourceFile = fn.getSourceFile();
  const keys: string[] = [];
  let unbounded = false;
  let found = false;

  const consider = (expr: ts.Expression): void => {
    found = true;
    if (isNonKeyExpression(expr)) return;
    const resolved = resolveExpression(expr, checker, canonical, depth + 1, fieldValues);
    if (resolved.kind === "unbounded" || resolved.kind === "template") unbounded = true;
    else keys.push(...resolved.keys);
  };

  const visit = (node: ts.Node): void => {
    if (node === fn) return;
    if (ts.isCallExpression(node)) {
      const callee = unwrapExpression(node.expression);
      const name = ts.isIdentifier(callee)
        ? callee.text
        : ts.isPropertyAccessExpression(callee)
          ? callee.name.text
          : null;
      if (name === fnName && node.arguments[index]) consider(node.arguments[index]!);
    }
    if ((ts.isJsxOpeningElement(node) || ts.isJsxSelfClosingElement(node)) && paramName) {
      const tag = node.tagName;
      if (ts.isIdentifier(tag) && tag.text === fnName) {
        for (const attr of node.attributes.properties) {
          if (!ts.isJsxAttribute(attr) || !ts.isIdentifier(attr.name) || attr.name.text !== paramName) continue;
          const init = attr.initializer;
          if (!init) continue;
          const expr = ts.isJsxExpression(init) ? init.expression : ts.isStringLiteral(init) ? init : undefined;
          if (expr) consider(expr);
        }
      }
    }
    ts.forEachChild(node, visit);
  };
  visit(sourceFile);
  if (!found) return null;
  if (unbounded) return { kind: "unbounded", via: "call-site" };
  return { kind: "finite", keys: uniqueKeys(keys, canonical), via: "call-site" };
}

function collectDestructuredPropKeys(
  id: ts.Identifier,
  checker: ts.TypeChecker,
  canonical: ReadonlySet<string>,
  depth: number,
  fieldValues: FieldValues,
): ResolvedKeys | null {
  if (!ts.isBindingElement(id.parent) || id.parent.name !== id) return null;
  const fn = enclosingFunction(id);
  if (!fn) return null;
  const param = fn.parameters.find((item) => {
    let current: ts.Node | undefined = id.parent;
    while (current && current !== fn) {
      if (current === item) return true;
      current = current.parent;
    }
    return false;
  });
  if (!param) return null;
  return collectCallSiteArgumentKeys(param, fn, checker, canonical, depth, fieldValues, id.text);
}

function mapCallbackReceiver(param: ts.ParameterDeclaration): ts.Expression | null {
  const fn = param.parent;
  if (!ts.isArrowFunction(fn) && !ts.isFunctionExpression(fn)) return null;
  let parent: ts.Node = fn.parent;
  if (ts.isParenthesizedExpression(parent)) parent = parent.parent;
  if (!ts.isCallExpression(parent)) return null;
  const isArg = parent.arguments.some((arg) => unwrapExpression(arg) === fn);
  if (!isArg) return null;
  const callee = parent.expression;
  if (ts.isPropertyAccessExpression(callee) && callee.name.text === "map") return callee.expression;
  return null;
}

function mergeResolved(
  parts: Array<ResolvedKeys | null>,
  via: string,
): ResolvedKeys {
  const keys: string[] = [];
  for (const part of parts) {
    if (!part || part.kind === "unbounded") return { kind: "unbounded", via };
    if (part.kind === "template") return part;
    keys.push(...part.keys);
  }
  return { kind: "finite", keys: [...new Set(keys)], via };
}

function resolveExpression(
  expr: ts.Expression,
  checker: ts.TypeChecker,
  canonical: ReadonlySet<string>,
  depth: number,
  fieldValues: FieldValues = new Map(),
): ResolvedKeys {
  if (depth > 8) return { kind: "unbounded", via: "depth" };
  const inner = unwrapExpression(expr);
  if (isNonKeyExpression(inner)) return { kind: "finite", keys: [], via: "non-key" };
  const literal = stringLiteralText(inner);
  if (literal) {
    return canonical.has(literal)
      ? { kind: "finite", keys: [literal], via: "literal" }
      : { kind: "finite", keys: [], via: "literal" };
  }
  if (ts.isTemplateExpression(inner)) {
    const pieces = templatePieces(inner);
    return { kind: "template", ...pieces };
  }
  if (ts.isConditionalExpression(inner)) {
    return mergeResolved(
      [
        resolveExpression(inner.whenTrue, checker, canonical, depth + 1, fieldValues),
        resolveExpression(inner.whenFalse, checker, canonical, depth + 1, fieldValues),
      ],
      "conditional",
    );
  }
  if (
    ts.isBinaryExpression(inner) &&
    (inner.operatorToken.kind === ts.SyntaxKind.BarBarToken || inner.operatorToken.kind === ts.SyntaxKind.QuestionQuestionToken)
  ) {
    return mergeResolved(
      [
        resolveExpression(inner.left, checker, canonical, depth + 1, fieldValues),
        resolveExpression(inner.right, checker, canonical, depth + 1, fieldValues),
      ],
      "binary",
    );
  }
  if (ts.isArrayLiteralExpression(inner)) {
    const parts = inner.elements.map((element) =>
      ts.isSpreadElement(element)
        ? ({ kind: "unbounded", via: "spread" } as ResolvedKeys)
        : resolveExpression(element, checker, canonical, depth + 1, fieldValues),
    );
    return mergeResolved(parts, "array");
  }
  if (ts.isObjectLiteralExpression(inner)) {
    const keys = collectObjectLiteralStringValues(inner, canonical);
    if (keys.length > 0) return { kind: "finite", keys, via: "object" };
  }
  if (ts.isCallExpression(inner)) {
    const callee = unwrapExpression(inner.expression);
    if (ts.isPropertyAccessExpression(callee) && (callee.name.text === "get" || callee.name.text === "at")) {
      const mapKeys = resolveConstMapValues(callee.expression, checker, canonical);
      if (mapKeys && mapKeys.length > 0) return { kind: "finite", keys: mapKeys, via: "map-values" };
    }
    const symbol = aliasedSymbol(checker, checker.getSymbolAtLocation(callee));
    for (const decl of symbol?.getDeclarations() ?? []) {
      if (ts.isFunctionLike(decl) && decl.body) {
        return collectReturnKeys(decl, checker, canonical, depth, fieldValues);
      }
    }
  }
  if (ts.isElementAccessExpression(inner)) {
    const mapKeys = resolveConstMapValues(inner.expression, checker, canonical);
    if (mapKeys && mapKeys.length > 0) return { kind: "finite", keys: mapKeys, via: "map-values" };
  }
  if (ts.isIdentifier(inner) || ts.isPropertyAccessExpression(inner) || ts.isElementAccessExpression(inner)) {
    const symbolLocation = ts.isIdentifier(inner)
      ? inner
      : ts.isPropertyAccessExpression(inner)
        ? inner.name
        : undefined;
    const rawSymbol = symbolLocation ? checker.getSymbolAtLocation(symbolLocation) : undefined;
    const symbol = rawSymbol && (rawSymbol.flags & ts.SymbolFlags.Alias)
      ? checker.getAliasedSymbol(rawSymbol)
      : rawSymbol;
    if (symbol) {
      const fromSymbol = resolveFromSymbol(symbol, checker, canonical, depth + 1, fieldValues);
      if (fromSymbol) return fromSymbol;
      const valueDecl = symbol.valueDeclaration;
      if (valueDecl && ts.isVariableDeclaration(valueDecl) && valueDecl.initializer) {
        return resolveExpression(valueDecl.initializer, checker, canonical, depth + 1, fieldValues);
      }
    }
    if (ts.isIdentifier(inner)) {
      const fn = enclosingFunction(inner);
      const param = fn?.parameters.find((item) => ts.isIdentifier(item.name) && item.name.text === inner.text);
      if (param) {
        const receiver = mapCallbackReceiver(param);
        if (receiver) return resolveExpression(receiver, checker, canonical, depth + 1, fieldValues);
        const fromCalls = collectCallSiteArgumentKeys(param, fn!, checker, canonical, depth, fieldValues);
        if (fromCalls) return fromCalls;
      }
      const binding = symbol?.valueDeclaration && ts.isBindingElement(symbol.valueDeclaration)
        ? symbol.valueDeclaration
        : ts.isBindingElement(inner.parent)
          ? inner.parent
          : undefined;
      if (binding && ts.isIdentifier(binding.name)) {
        const fromDestructure = collectDestructuredPropKeys(binding.name, checker, canonical, depth, fieldValues);
        if (fromDestructure) return fromDestructure;
      }
    }
    if (ts.isPropertyAccessExpression(inner)) {
      const field = fieldValues.get(inner.name.text);
      if (field?.unbounded) return { kind: "unbounded", via: `prop:${inner.name.text}` };
      if (field && field.keys.length > 0) return { kind: "finite", keys: uniqueKeys(field.keys, canonical), via: `prop:${inner.name.text}` };
    }
    const type = checker.getTypeAtLocation(inner);
    const fromType = literalTypesFromType(type, checker, canonical);
    if (fromType && fromType.length > 0) return { kind: "finite", keys: fromType, via: "type" };
    return { kind: "unbounded", via: checker.typeToString(type) };
  }
  const type = checker.getTypeAtLocation(inner);
  const fromType = literalTypesFromType(type, checker, canonical);
  if (fromType && fromType.length > 0) return { kind: "finite", keys: fromType, via: "type" };
  return { kind: "unbounded", via: checker.typeToString(type) };
}

function createFixtureProgram(files: SourceModule[]): ts.Program {
  const map = new Map(files.map((file) => [normalizeModulePath(file.path), file.source]));
  const options: ts.CompilerOptions = {
    target: ts.ScriptTarget.ES2023,
    module: ts.ModuleKind.ESNext,
    moduleResolution: ts.ModuleResolutionKind.Bundler,
    jsx: ts.JsxEmit.ReactJSX,
    noEmit: true,
    skipLibCheck: true,
    noLib: true,
    allowImportingTsExtensions: true,
    isolatedModules: true,
    strict: false,
  };
  const host: ts.CompilerHost = {
    getSourceFile: (fileName, languageVersion) => {
      const source = map.get(normalizeModulePath(fileName));
      if (source === undefined) return undefined;
      return ts.createSourceFile(fileName, source, languageVersion, true, scriptKindFor(fileName));
    },
    getDefaultLibFileName: () => "lib.d.ts",
    writeFile: () => {},
    getCurrentDirectory: () => "",
    getCanonicalFileName: (fileName) => normalizeModulePath(fileName),
    useCaseSensitiveFileNames: () => true,
    getNewLine: () => "\n",
    fileExists: (fileName) => map.has(normalizeModulePath(fileName)),
    readFile: (fileName) => map.get(normalizeModulePath(fileName)),
    directoryExists: () => true,
    getDirectories: () => [],
  };
  return ts.createProgram([...map.keys()], options, host);
}

function isTypePositionLiteral(node: ts.Node): boolean {
  let current: ts.Node | undefined = node.parent;
  while (current) {
    if (ts.isLiteralTypeNode(current) || ts.isTypeAliasDeclaration(current) || ts.isTypeNode(current)) return true;
    if (ts.isImportDeclaration(current) || ts.isExportDeclaration(current)) return false;
    if (ts.isVariableDeclaration(current) || ts.isFunctionLike(current) || ts.isCallExpression(current)) return false;
    current = current.parent;
  }
  return false;
}

function variableTypeMentionsTKey(node: ts.Node): boolean {
  let current: ts.Node | undefined = node.parent;
  while (current) {
    if (ts.isVariableDeclaration(current) && current.type) return current.type.getText().includes("TKey");
    if (ts.isPropertyDeclaration(current) && current.type) return current.type.getText().includes("TKey");
    current = current.parent;
  }
  return false;
}

function classifyNonCallLiteral(
  node: ts.StringLiteral | ts.NoSubstitutionTemplateLiteral,
  filePath: string,
  sourceFile: ts.SourceFile,
  checker: ts.TypeChecker | null,
  canonical: ReadonlySet<string>,
): KeyEvidence | null {
  if (!canonical.has(node.text)) return null;
  if (isTypePositionLiteral(node)) return null;
  if (inTranslateKeyPosition(node)) return null;
  const attr = enclosingJsxAttributeName(node);
  if (attr === "k") return null;
  const field = enclosingPropertyName(node);
  if (field && TKEY_FIELDS.has(field)) {
    return evidence("LIVE_TYPED_REFERENCE", filePath, sourceFile, node, `field:${field}`);
  }
  if (hasTKeyAssertionAncestor(node)) {
    return evidence("LIVE_TYPED_REFERENCE", filePath, sourceFile, node, "as-tkey");
  }
  if (variableTypeMentionsTKey(node)) {
    return evidence("LIVE_TYPED_REFERENCE", filePath, sourceFile, node, "tkey-container");
  }
  if (checker) {
    const contextual = checker.getContextualType(node);
    if (contextual) {
      const literals = literalTypesFromType(contextual, checker, canonical);
      if (literals && literals.includes(node.text) && (typeLooksLikeTKey(contextual, checker) || literals.length < canonical.size)) {
        if (typeLooksLikeTKey(contextual, checker) || (contextual.aliasSymbol?.getName() === "TKey")) {
          return evidence("LIVE_TYPED_REFERENCE", filePath, sourceFile, node, "contextual-tkey");
        }
      }
      if (typeLooksLikeTKey(contextual, checker)) {
        return evidence("LIVE_TYPED_REFERENCE", filePath, sourceFile, node, "contextual-tkey");
      }
    }
  }
  return evidence("LIVE_CONSERVATIVE_LITERAL", filePath, sourceFile, node, "string-literal");
}

function isExpandedTKeyUnion(type: ts.Type, canonical: ReadonlySet<string>): boolean {
  if (!(type.flags & ts.TypeFlags.Union) || canonical.size < 8) return false;
  let literals = 0;
  for (const member of (type as ts.UnionType).types) {
    if (member.flags & ts.TypeFlags.StringLiteral) literals += 1;
  }
  return literals >= Math.min(canonical.size, 32) && literals >= canonical.size * 0.5;
}

function typeLooksLikeTKey(type: ts.Type, checker: ts.TypeChecker): boolean {
  if (type.flags & (ts.TypeFlags.Undefined | ts.TypeFlags.Null | ts.TypeFlags.Void)) return false;
  if (type.aliasSymbol?.getName() === "TKey" || type.symbol?.getName() === "TKey") return true;
  if (type.flags & ts.TypeFlags.Union) {
    return (type as ts.UnionType).types.some((member) => typeLooksLikeTKey(member, checker));
  }
  const text = checker.typeToString(type);
  return text === "TKey" || text.startsWith("TKey |") || text.endsWith("| TKey");
}

function applyResolved(
  resolved: ResolvedKeys,
  canonical: readonly string[],
  canonicalSet: ReadonlySet<string>,
  filePath: string,
  sourceFile: ts.SourceFile,
  node: ts.Node,
  byKey: Map<string, KeyEvidence[]>,
  unresolved: KeyEvidence[],
  unboundedCalls: KeyEvidence[],
): void {
  if (resolved.kind === "finite") {
    const item = evidence("LIVE_STATIC", filePath, sourceFile, node, "resolved-keys", resolved.via);
    for (const key of resolved.keys) addEvidence(byKey, key, item);
    return;
  }
  if (resolved.kind === "template") {
    const item = evidence(
      "UNRESOLVED_DYNAMIC",
      filePath,
      sourceFile,
      node,
      "dynamic-template",
      `${resolved.prefix}\${…}${resolved.suffix}`,
    );
    unresolved.push(item);
    if (resolved.prefix.length + resolved.suffix.length + resolved.middles.join("").length === 0) {
      unboundedCalls.push(item);
      return;
    }
    for (const key of canonical) {
      if (matchesTemplate(key, resolved.prefix, resolved.suffix, resolved.middles)) addEvidence(byKey, key, item);
    }
    return;
  }
  unboundedCalls.push(evidence("UNRESOLVED_DYNAMIC", filePath, sourceFile, node, "unbounded-call", resolved.via));
}

function rememberField(fieldValues: FieldValues, name: string, key: string | null, typedNonLiteral: boolean): void {
  const entry = fieldValues.get(name) ?? { keys: [], unbounded: false };
  if (key) entry.keys.push(key);
  if (typedNonLiteral) entry.unbounded = true;
  fieldValues.set(name, entry);
}

function collectTypedFieldValues(
  sourceFile: ts.SourceFile,
  checker: ts.TypeChecker,
  canonical: ReadonlySet<string>,
  fieldValues: FieldValues,
): void {
  const visit = (node: ts.Node): void => {
    if (ts.isPropertyAssignment(node)) {
      const name = propertyName(node);
      if (name) {
        const text = stringLiteralText(node.initializer);
        const contextual = checker.getContextualType(node.initializer);
        const typed = TKEY_FIELDS.has(name)
          || hasTKeyAssertionAncestor(node.initializer)
          || variableTypeMentionsTKey(node)
          || !!(contextual && typeLooksLikeTKey(contextual, checker));
        if (text && canonical.has(text)) rememberField(fieldValues, name, text, false);
        else if (typed) {
          const resolved = resolveExpression(node.initializer, checker, canonical, 0, fieldValues);
          if (resolved.kind === "finite") {
            for (const key of resolved.keys) rememberField(fieldValues, name, key, false);
          } else {
            rememberField(fieldValues, name, null, true);
          }
        }
      }
    }
    ts.forEachChild(node, visit);
  };
  visit(sourceFile);
}

function visitTranslationFile(options: {
  filePath: string;
  sourceFile: ts.SourceFile;
  checker: ts.TypeChecker;
  canonical: readonly string[];
  canonicalSet: ReadonlySet<string>;
  fieldValues: FieldValues;
  byKey: Map<string, KeyEvidence[]>;
  unresolved: KeyEvidence[];
  unboundedCalls: KeyEvidence[];
  sourceOnly: boolean;
}): void {
  const {
    filePath,
    sourceFile,
    checker,
    canonical,
    canonicalSet,
    fieldValues,
    byKey,
    unresolved,
    unboundedCalls,
    sourceOnly,
  } = options;

  const keepFinite = (keys: string[], node: ts.Node, kind: string, detail?: string): void => {
    const status = sourceOnly ? "SOURCE_ONLY_REFERENCE" : "LIVE_STATIC";
    const item = evidence(status, filePath, sourceFile, node, kind, detail);
    for (const key of keys) addEvidence(byKey, key, item);
  };

  const visit = (node: ts.Node): void => {
    if (ts.isCallExpression(node)) {
      const name = calleeName(node);
      if (name && TRANSLATE_CALLEES.has(name) && !isPassThroughTranslator(node)) {
        const argument = translateKeyArgument(node, name);
        if (!argument) {
          if (!sourceOnly) {
            unboundedCalls.push(evidence("UNRESOLVED_DYNAMIC", filePath, sourceFile, node, "unbounded-call", "missing-key-arg"));
          }
        } else {
          const resolved = resolveExpression(argument, checker, canonicalSet, 0, fieldValues);
          if (sourceOnly) {
            if (resolved.kind === "finite") keepFinite(resolved.keys, argument, "source-only", resolved.via);
          } else if (resolved.kind === "finite" && resolved.via === "literal") {
            keepFinite(resolved.keys, argument, name === "t" ? "t-literal" : "catalogValue-literal");
          } else {
            applyResolved(resolved, canonical, canonicalSet, filePath, sourceFile, argument, byKey, unresolved, unboundedCalls);
          }
        }
      }
    }
    if (ts.isJsxAttribute(node) && ts.isIdentifier(node.name) && node.name.text === "k" && node.initializer) {
      const init = node.initializer;
      const expr = ts.isJsxExpression(init) ? init.expression : ts.isStringLiteral(init) ? init : undefined;
      if (expr && (ts.isStringLiteral(expr) || ts.isNoSubstitutionTemplateLiteral(expr)) && canonicalSet.has(expr.text)) {
        keepFinite([expr.text], expr, "trans-k");
      } else if (expr && !sourceOnly) {
        const resolved = resolveExpression(expr, checker, canonicalSet, 0, fieldValues);
        applyResolved(resolved, canonical, canonicalSet, filePath, sourceFile, expr, byKey, unresolved, unboundedCalls);
      }
    }
    if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) {
      const item = classifyNonCallLiteral(node, filePath, sourceFile, checker, canonicalSet);
      if (item) {
        if (sourceOnly) {
          if (item.status === "LIVE_TYPED_REFERENCE" || item.status === "LIVE_STATIC") {
            addEvidence(byKey, node.text, { ...item, status: "SOURCE_ONLY_REFERENCE", kind: "source-only" });
          }
        } else {
          addEvidence(byKey, node.text, item);
        }
      }
    }
    ts.forEachChild(node, visit);
  };
  visit(sourceFile);
}

export function isCatalogPath(filePath: string): boolean {
  const normalized = normalizeModulePath(filePath);
  if (/(^|\/)i18n\/en(?:-base)?\.(?:ts|mts)$/.test(normalized)) return true;
  if (/(?:^|\/)i18n\/(?:locale-overrides|lab|lab-translations|log-guard|routing-compatibility)\.ts$/.test(normalized)) return true;
  return TRANSLATED_LOCALES.some((locale) => normalized.endsWith(`/i18n/${locale}.ts`));
}

export function analyzeKeyLiveness(options: AnalyzeOptions): LivenessReport {
  const canonical = [...new Set(options.canonicalKeys)];
  const canonicalSet = new Set(canonical);
  const catalogPathSet = new Set((options.catalogPaths ?? []).map(normalizeModulePath));
  const modules: SourceModule[] = options.files.map((file) => ({
    path: normalizeModulePath(file.path),
    source: file.source,
  }));
  const analysisErrors: string[] = [];
  const byKey = new Map<string, KeyEvidence[]>();
  const unresolved: KeyEvidence[] = [];
  const unboundedCalls: KeyEvidence[] = [];

  let reachable: Set<string>;
  if (options.entryPoints && options.entryPoints.length > 0) {
    const graph = reachableRuntimeModules(modules, options.entryPoints);
    reachable = graph.reachable;
    for (const miss of graph.unresolvedSpecs) {
      analysisErrors.push(`unresolved runtime import ${miss.spec} from ${miss.from}:${miss.line}`);
    }
    for (const load of graph.unsupportedLoads) {
      analysisErrors.push(
        `unsupported runtime module load in ${load.from}:${load.line} (${load.reason}: ${load.detail})`,
      );
    }
    if (reachable.size === 0) analysisErrors.push("runtime entry points resolved to no modules");
  } else {
    reachable = new Set(modules.map((file) => file.path).filter((filePath) => !catalogPathSet.has(filePath) && !isCatalogPath(filePath)));
  }

  let program: ts.Program;
  try {
    program = options.program ?? createFixtureProgram(modules);
  } catch (error) {
    analysisErrors.push(`typescript program failed: ${error instanceof Error ? error.message : String(error)}`);
    const keys: Record<string, KeyRecord> = {};
    for (const key of canonical) keys[key] = { status: "UNUSED_PROVEN", evidence: [] };
    return { keys, unresolved, unboundedCalls, analysisErrors };
  }
  const checker = program.getTypeChecker();
  const programFiles = new Map<string, ts.SourceFile>();
  for (const sourceFile of program.getSourceFiles()) {
    programFiles.set(normalizeModulePath(sourceFile.fileName), sourceFile);
  }

  const fieldValues: FieldValues = new Map();
  const sourceFileFor = (file: SourceModule): ts.SourceFile =>
    programFiles.get(file.path)
    ?? [...programFiles.entries()].find(([name]) => name.endsWith(`/${file.path}`) || name.endsWith(file.path))?.[1]
    ?? ts.createSourceFile(file.path, file.source, ts.ScriptTarget.Latest, true, scriptKindFor(file.path));

  for (const file of modules) {
    if (catalogPathSet.has(file.path) || isCatalogPath(file.path)) continue;
    if (!reachable.has(file.path)) continue;
    collectTypedFieldValues(sourceFileFor(file), checker, canonicalSet, fieldValues);
  }

  for (const file of modules) {
    if (catalogPathSet.has(file.path) || isCatalogPath(file.path)) continue;
    const sourceOnly = !reachable.has(file.path);
    const sourceFile = sourceFileFor(file);
    visitTranslationFile({
      filePath: file.path,
      sourceFile,
      checker,
      canonical,
      canonicalSet,
      fieldValues,
      byKey,
      unresolved,
      unboundedCalls,
      sourceOnly,
    });
  }

  const keys: Record<string, KeyRecord> = {};
  for (const key of canonical) {
    const evidenceList = byKey.get(key) ?? [];
    keys[key] = { status: finalizeStatus(evidenceList), evidence: evidenceList };
  }
  return { keys, unresolved, unboundedCalls, analysisErrors };
}

export function statusOf(report: LivenessReport, key: string): KeyStatus {
  return report.keys[key]?.status ?? "UNUSED_PROVEN";
}

export function extractCatalogKeys(source: string): Set<string> {
  const sourceFile = ts.createSourceFile("catalog.ts", source, ts.ScriptTarget.Latest, true, ts.ScriptKind.TS);
  const keys = new Set<string>();
  const visit = (node: ts.Node): void => {
    if (ts.isObjectLiteralExpression(node)) {
      for (const property of node.properties) {
        const name = propertyName(property);
        if (name) keys.add(name);
      }
    }
    ts.forEachChild(node, visit);
  };
  visit(sourceFile);
  return keys;
}

export function extractTopLevelExportKeys(source: string, exportName: string): Set<string> {
  const sourceFile = ts.createSourceFile("catalog.ts", source, ts.ScriptTarget.Latest, true, ts.ScriptKind.TS);
  const keys = new Set<string>();
  const visit = (node: ts.Node): void => {
    if (ts.isVariableDeclaration(node) && ts.isIdentifier(node.name) && node.name.text === exportName && node.initializer) {
      const initializer = unwrapExpression(node.initializer);
      if (ts.isObjectLiteralExpression(initializer)) {
        for (const property of initializer.properties) {
          const name = propertyName(property);
          if (name) keys.add(name);
        }
      }
      return;
    }
    ts.forEachChild(node, visit);
  };
  visit(sourceFile);
  return keys;
}

export type LocaleParityFinding = {
  locale: string;
  missing: string[];
  extra: string[];
};

export function localeParity(
  canonical: ReadonlySet<string>,
  locales: Record<string, ReadonlySet<string>>,
): LocaleParityFinding[] {
  const findings: LocaleParityFinding[] = [];
  for (const [locale, keys] of Object.entries(locales)) {
    const missing = [...canonical].filter((key) => !keys.has(key)).sort();
    const extra = [...keys].filter((key) => !canonical.has(key)).sort();
    if (missing.length > 0 || extra.length > 0) findings.push({ locale, missing, extra });
  }
  return findings;
}

function isSpace(ch: string | undefined): boolean {
  return ch === " " || ch === "\t";
}

function propertyLineRange(source: string, start: number, end: number): { from: number; to: number } {
  let from = start;
  while (from > 0 && source[from - 1] !== "\n") from -= 1;
  let to = end;
  while (isSpace(source[to])) to += 1;
  if (source[to] === ",") to += 1;
  while (isSpace(source[to])) to += 1;
  if (source[to] === "\r") to += 1;
  if (source[to] === "\n") to += 1;
  return { from, to };
}

export function removeCatalogKeysFromSource(source: string, drop: ReadonlySet<string>): string {
  if (drop.size === 0) return source;
  const sourceFile = ts.createSourceFile("catalog.ts", source, ts.ScriptTarget.Latest, true, ts.ScriptKind.TS);
  const ranges: Array<{ from: number; to: number }> = [];
  const visit = (node: ts.Node): void => {
    if (ts.isPropertyAssignment(node) || ts.isShorthandPropertyAssignment(node)) {
      const name = propertyName(node);
      if (name && drop.has(name)) {
        ranges.push(propertyLineRange(source, node.getStart(sourceFile), node.getEnd()));
      }
    }
    ts.forEachChild(node, visit);
  };
  visit(sourceFile);
  ranges.sort((a, b) => b.from - a.from);
  let next = source;
  for (const range of ranges) next = next.slice(0, range.from) + next.slice(range.to);
  return next;
}

export async function listSourceFiles(root: string): Promise<string[]> {
  const files: string[] = [];
  const walk = async (directory: string): Promise<void> => {
    for (const entry of await readdir(directory, { withFileTypes: true })) {
      const full = path.join(directory, entry.name);
      if (entry.isDirectory()) {
        if (entry.name === "node_modules" || entry.name === "dist") continue;
        await walk(full);
        continue;
      }
      if (/\.(?:ts|tsx|mts)$/.test(entry.name)) files.push(full);
    }
  };
  await walk(root);
  return files.sort();
}

export function unusedKeys(report: LivenessReport): string[] {
  return Object.entries(report.keys)
    .filter(([, record]) => record.status === "UNUSED_PROVEN")
    .map(([key]) => key)
    .sort();
}

export function sourceOnlyKeys(report: LivenessReport): string[] {
  return Object.entries(report.keys)
    .filter(([, record]) => record.status === "SOURCE_ONLY_REFERENCE")
    .map(([key]) => key)
    .sort();
}

export function guiRootFromScript(scriptUrl: string = import.meta.url): string {
  return path.resolve(path.dirname(fileURLToPath(scriptUrl)), "..");
}

export function createGuiProgram(guiRoot: string): ts.Program {
  const configPath = ts.findConfigFile(guiRoot, ts.sys.fileExists, "tsconfig.app.json");
  if (!configPath) throw new Error(`tsconfig.app.json not found in ${guiRoot}`);
  const configFile = ts.readConfigFile(configPath, ts.sys.readFile);
  if (configFile.error) throw new Error(ts.flattenDiagnosticMessageText(configFile.error.messageText, "\n"));
  const parsed = ts.parseJsonConfigFileContent(configFile.config, ts.sys, guiRoot);
  return ts.createProgram({ rootNames: parsed.fileNames, options: parsed.options });
}

export async function analyzeGuiCatalog(guiRoot: string, canonicalKeys: readonly string[]): Promise<LivenessReport> {
  const srcRoot = path.join(guiRoot, "src");
  const files = await listSourceFiles(srcRoot);
  const loaded = await Promise.all(
    files.map(async (filePath) => ({
      path: path.relative(guiRoot, filePath).replace(/\\/g, "/"),
      source: await readFile(filePath, "utf8"),
    })),
  );
  return analyzeKeyLiveness({
    canonicalKeys,
    files: loaded,
    entryPoints: viteRuntimeEntryPoints(),
    program: createGuiProgram(guiRoot),
  });
}

export function catalogFileRelPaths(): string[] {
  return [
    "src/i18n/en-base.mts",
    "src/i18n/en.ts",
    "src/i18n/locale-overrides.ts",
    "src/i18n/lab.ts",
  ];
}

export async function pruneUnusedCatalogKeys(guiRoot: string, unused: readonly string[]): Promise<string[]> {
  const drop = new Set(unused);
  const changed: string[] = [];
  for (const relative of catalogFileRelPaths()) {
    const full = path.join(guiRoot, relative);
    const source = await readFile(full, "utf8");
    const next = removeCatalogKeysFromSource(source, drop);
    if (next !== source) {
      await writeFile(full, next, "utf8");
      changed.push(relative);
    }
  }
  return changed;
}

export function livenessGateFailures(report: LivenessReport): string[] {
  const failures: string[] = [];
  failures.push(...report.analysisErrors);
  if (report.unboundedCalls.length > 0) {
    failures.push(
      `unbounded translation calls: ${report.unboundedCalls
        .map((item) => `${item.path}:${item.line} (${item.detail ?? item.kind})`)
        .join("; ")}`,
    );
  }
  const unused = unusedKeys(report);
  if (unused.length > 0) failures.push(`unused canonical keys: ${unused.length}`);
  return failures;
}
