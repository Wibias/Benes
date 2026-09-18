/** Runtime module reachability for translation-key liveness. */
import { createRequire } from "node:module";
import path from "node:path";

const require = createRequire(import.meta.url);
const ts = require("typescript") as typeof import("typescript");

export type SourceModule = {
  path: string;
  source: string;
};

const SOURCE_EXTENSIONS = [".tsx", ".ts", ".mts", ".jsx", ".js", ".mjs"];

export function normalizeModulePath(value: string): string {
  return value.replace(/\\/g, "/").replace(/^\.\//, "");
}

function scriptKindFor(filePath: string): ts.ScriptKind {
  if (filePath.endsWith(".tsx") || filePath.endsWith(".jsx")) return ts.ScriptKind.TSX;
  return ts.ScriptKind.TS;
}

function stripQuery(spec: string): string {
  const cut = spec.search(/[?#]/);
  return cut < 0 ? spec : spec.slice(0, cut);
}

function candidateFiles(fromFile: string, spec: string): string[] {
  const cleaned = stripQuery(spec);
  if (cleaned.startsWith("http:") || cleaned.startsWith("https:")) return [];
  if (!cleaned.startsWith(".") && !cleaned.startsWith("/")) return [];
  const dir = path.posix.dirname(normalizeModulePath(fromFile));
  const joined = path.posix.normalize(`${dir}/${cleaned}`);
  const withoutExt = joined.replace(/\.(?:tsx|ts|mts|jsx|js|mjs)$/, "");
  const out: string[] = [];
  const push = (value: string) => {
    if (!out.includes(value)) out.push(value);
  };
  push(joined);
  for (const ext of SOURCE_EXTENSIONS) {
    push(`${withoutExt}${ext}`);
    push(`${joined}${ext}`);
    push(`${joined}/index${ext}`);
    push(`${withoutExt}/index${ext}`);
  }
  return out;
}

function isTypeOnlyNamedBinding(binding: ts.NamedImportBindings | undefined): boolean {
  if (!binding) return false;
  if (ts.isNamespaceImport(binding)) return false;
  if (!ts.isNamedImports(binding)) return false;
  return binding.elements.length > 0 && binding.elements.every((el) => el.isTypeOnly);
}

function isRuntimeImport(node: ts.ImportDeclaration): boolean {
  const clause = node.importClause;
  if (!clause) return true;
  if (clause.isTypeOnly) return false;
  if (clause.name) return true;
  if (clause.namedBindings && ts.isNamespaceImport(clause.namedBindings)) return true;
  if (isTypeOnlyNamedBinding(clause.namedBindings)) return false;
  return true;
}

function isRuntimeExport(node: ts.ExportDeclaration): boolean {
  if (node.isTypeOnly) return false;
  if (!node.exportClause) return true;
  if (ts.isNamespaceExport(node.exportClause)) return true;
  if (!ts.isNamedExports(node.exportClause)) return true;
  if (node.exportClause.elements.length === 0) return true;
  return node.exportClause.elements.some((element) => !element.isTypeOnly);
}

function lineOf(sourceFile: ts.SourceFile, node: ts.Node): number {
  return sourceFile.getLineAndCharacterOfPosition(node.getStart(sourceFile)).line + 1;
}

function unwrapImportArg(expr: ts.Expression): ts.Expression {
  let current = expr;
  while (ts.isParenthesizedExpression(current)) current = current.expression;
  return current;
}

function finiteImportSpecs(expr: ts.Expression): string[] | null {
  const inner = unwrapImportArg(expr);
  if (ts.isStringLiteral(inner) || ts.isNoSubstitutionTemplateLiteral(inner)) return [inner.text];
  if (ts.isConditionalExpression(inner)) {
    const left = finiteImportSpecs(inner.whenTrue);
    const right = finiteImportSpecs(inner.whenFalse);
    if (!left || !right) return null;
    return [...new Set([...left, ...right])];
  }
  if (
    ts.isBinaryExpression(inner) &&
    (inner.operatorToken.kind === ts.SyntaxKind.BarBarToken || inner.operatorToken.kind === ts.SyntaxKind.QuestionQuestionToken)
  ) {
    const left = finiteImportSpecs(inner.left);
    const right = finiteImportSpecs(inner.right);
    if (!left || !right) return null;
    return [...new Set([...left, ...right])];
  }
  return null;
}

function isImportMetaGlob(node: ts.Node): boolean {
  if (!ts.isPropertyAccessExpression(node)) return false;
  const name = node.name.text;
  if (name !== "glob" && name !== "globEager") return false;
  const meta = node.expression;
  return ts.isMetaProperty(meta) && meta.keywordToken === ts.SyntaxKind.ImportKeyword && meta.name.text === "meta";
}

export type RuntimeLoad =
  | { kind: "spec"; spec: string; line: number }
  | { kind: "unsupported"; reason: string; detail: string; line: number };

function collectRuntimeLoads(sourceFile: ts.SourceFile): RuntimeLoad[] {
  const loads: RuntimeLoad[] = [];
  const visit = (node: ts.Node): void => {
    if (ts.isImportDeclaration(node) && node.moduleSpecifier && ts.isStringLiteral(node.moduleSpecifier)) {
      if (isRuntimeImport(node)) {
        loads.push({ kind: "spec", spec: node.moduleSpecifier.text, line: lineOf(sourceFile, node) });
      }
    } else if (ts.isExportDeclaration(node) && node.moduleSpecifier && ts.isStringLiteral(node.moduleSpecifier)) {
      if (isRuntimeExport(node)) {
        loads.push({ kind: "spec", spec: node.moduleSpecifier.text, line: lineOf(sourceFile, node) });
      }
    } else if (ts.isCallExpression(node) && node.expression.kind === ts.SyntaxKind.ImportKeyword) {
      const arg = node.arguments[0];
      const line = lineOf(sourceFile, node);
      if (!arg) {
        loads.push({ kind: "unsupported", reason: "unsupported-dynamic-import", detail: "import()", line });
      } else {
        const specs = finiteImportSpecs(arg);
        if (specs) {
          for (const spec of specs) loads.push({ kind: "spec", spec, line });
        } else {
          loads.push({
            kind: "unsupported",
            reason: "unsupported-dynamic-import",
            detail: arg.getText(sourceFile).replace(/\s+/g, " ").trim(),
            line,
          });
        }
      }
    } else if (isImportMetaGlob(node)) {
      loads.push({
        kind: "unsupported",
        reason: "unsupported-runtime-loader",
        detail: node.getText(sourceFile).replace(/\s+/g, " ").trim(),
        line: lineOf(sourceFile, node),
      });
    }
    ts.forEachChild(node, visit);
  };
  visit(sourceFile);
  return loads;
}

export function resolveRuntimeDependency(
  fromFile: string,
  spec: string,
  files: ReadonlyMap<string, string>,
): string | null {
  for (const candidate of candidateFiles(fromFile, spec)) {
    const normalized = normalizeModulePath(candidate);
    if (files.has(normalized)) return normalized;
  }
  return null;
}

export type UnresolvedSpec = {
  from: string;
  spec: string;
  line: number;
};

export type UnsupportedLoad = {
  from: string;
  line: number;
  reason: string;
  detail: string;
};

export type ReachabilityResult = {
  reachable: Set<string>;
  unresolvedSpecs: UnresolvedSpec[];
  unsupportedLoads: UnsupportedLoad[];
};

export function reachableRuntimeModules(
  files: Array<SourceModule> | ReadonlyMap<string, string>,
  entryPoints: readonly string[],
): ReachabilityResult {
  const map = files instanceof Map
    ? files
    : new Map(files.map((file) => [normalizeModulePath(file.path), file.source]));
  const reachable = new Set<string>();
  const unresolvedSpecs: UnresolvedSpec[] = [];
  const unsupportedLoads: UnsupportedLoad[] = [];
  const queue = entryPoints.map(normalizeModulePath).filter((entry) => map.has(entry));

  while (queue.length > 0) {
    const current = queue.pop()!;
    if (reachable.has(current)) continue;
    reachable.add(current);
    const source = map.get(current);
    if (source === undefined) continue;
    const sourceFile = ts.createSourceFile(current, source, ts.ScriptTarget.Latest, true, scriptKindFor(current));
    for (const load of collectRuntimeLoads(sourceFile)) {
      if (load.kind === "unsupported") {
        unsupportedLoads.push({ from: current, line: load.line, reason: load.reason, detail: load.detail });
        continue;
      }
      if (load.spec.endsWith(".css") || load.spec.endsWith(".svg") || load.spec.endsWith(".json")) continue;
      const resolved = resolveRuntimeDependency(current, load.spec, map);
      if (!resolved) {
        if (load.spec.startsWith(".")) unresolvedSpecs.push({ from: current, spec: load.spec, line: load.line });
        continue;
      }
      if (!reachable.has(resolved)) queue.push(resolved);
    }
  }

  return { reachable, unresolvedSpecs, unsupportedLoads };
}

export function viteRuntimeEntryPoints(): string[] {
  return ["src/main.tsx"];
}
