#!/usr/bin/env node
"use strict";
// Source-only client inventory. Never evaluates application modules or performs
// HTTP calls. Unresolved transport forwarding is explicit, not silently omitted.
const fs = require("node:fs");
const path = require("node:path");
const ts = require("../web/node_modules/typescript");
const ROOT = path.resolve(__dirname, "..");
function scan(root = ROOT) {
  const files = [];
  function walk(dir) {
    for (const item of fs.readdirSync(dir, { withFileTypes: true })) {
      if (["node_modules", ".next", ".git", "out"].includes(item.name)) continue;
      const p = path.join(dir, item.name);
      if (item.isDirectory()) walk(p);
      else if (/\.tsx?$/.test(p) && !/\.(test|spec|r5audit)\.tsx?$/.test(p) && !/\.d\.ts$/.test(p)) files.push(p);
    }
  }
  walk(path.join(root, "web"));
  const program = ts.createProgram(files, { noResolve: true, noLib: true, jsx: ts.JsxEmit.Preserve });
  const checker = program.getTypeChecker();
  const rows = [];
  function symbolic(n, seen = new Set()) {
    if (!n) return "";
    if (ts.isStringLiteral(n) || ts.isNoSubstitutionTemplateLiteral(n)) return n.text;
    if (ts.isParenthesizedExpression(n) || ts.isAsExpression(n)) return symbolic(n.expression, seen);
    if (ts.isCallExpression(n)) {
      if (n.expression.getText() === "workPath") return "/mailboxes/{id}";
      if (n.expression.getText() === "encodeURIComponent") return "{id}";
      if (n.expression.getText() === "getBaseUrl") return "";
    }
    if (ts.isTemplateExpression(n)) return n.head.text + n.templateSpans.map(s => symbolic(s.expression, seen) + s.literal.text).join("");
    if (ts.isBinaryExpression(n) && n.operatorToken.kind === ts.SyntaxKind.PlusToken) return symbolic(n.left, seen) + symbolic(n.right, seen);
    if (ts.isIdentifier(n)) {
      const sym = checker.getSymbolAtLocation(n);
      const decl = sym?.valueDeclaration;
      if (decl && ts.isVariableDeclaration(decl) && decl.initializer && !seen.has(decl)) {
        const next = new Set(seen); next.add(decl); return symbolic(decl.initializer, next);
      }
    }
    return "{dynamic}";
  }
  function methods(options) {
    if (!options) return ["GET"];
    if (!ts.isObjectLiteralExpression(options)) return ["DYNAMIC"];
    const m = options.properties.find(p => ts.isPropertyAssignment(p) && p.name.getText().replace(/["']/g, "") === "method");
    if (!m) return ["GET"];
    if (ts.isConditionalExpression(m.initializer)) return [...new Set([symbolic(m.initializer.whenTrue), symbolic(m.initializer.whenFalse)])];
    const value = symbolic(m.initializer); return /^[A-Z]+$/.test(value) ? [value] : ["DYNAMIC"];
  }
  for (const name of files.sort()) {
    const sf = program.getSourceFile(name);
    if (sf.parseDiagnostics.length) throw new Error(`syntax errors in ${name}`);
    function visit(n) {
      if (ts.isCallExpression(n) && ts.isIdentifier(n.expression) && ["request", "company", "downloadCompanyFile", "streamEvents", "fetch"].includes(n.expression.text)) {
        const callee = n.expression.text;
        const input = n.arguments[0];
        const variants = input && ts.isConditionalExpression(input) ? [input.whenTrue, input.whenFalse] : [input];
        let enclosing = n.parent;
        while (enclosing && !ts.isFunctionDeclaration(enclosing) && !ts.isVariableDeclaration(enclosing)) enclosing = enclosing.parent;
        const owner = enclosing?.name?.getText() || "inline";
        const ms = ["downloadCompanyFile", "streamEvents"].includes(callee) ? ["GET"] : methods(n.arguments[1]);
        const options = n.arguments[1];
        const methodNode = options && ts.isObjectLiteralExpression(options) ? options.properties.find(p => ts.isPropertyAssignment(p) && p.name.getText().replace(/["']/g, "") === "method")?.initializer : null;
        const paired = input && ts.isConditionalExpression(input) && methodNode && ts.isConditionalExpression(methodNode) && input.condition.getText() === methodNode.condition.getText();
        variants.forEach((variant, branch) => {
          const target = (["company", "downloadCompanyFile"].includes(callee) ? "/api/v1/company" : "") + symbolic(variant);
          const source = path.relative(root, name).split(path.sep).join("/");
          const forwarding = (source === "web/lib/api/base.ts" && !target.startsWith("/api/v1")) || (source === "web/lib/company.ts" && callee === "request" && target === "/api/v1/company{dynamic}");
          rows.push({ source, line: sf.getLineAndCharacterOfPosition(n.getStart()).line + 1, branch, callee, owner, expression: variant?.getText() || "", path: target, methods: paired && variants.length === ms.length ? [ms[branch]] : ms, forwarding });
        });
      }
      ts.forEachChild(n, visit);
    }
    visit(sf);
  }
  if (!rows.length) throw new Error("empty API client scan");
  return rows;
}
function validate(actual, documented, routes) {
  if (!actual.length || actual.length !== documented.length) throw new Error("client inventory count drift");
  const normalize = value => value.split("?")[0].replace(/\{[^{}]*\}/g, "{}");
  const routeMap = new Map(routes.map(r => [`${r.method} ${normalize(r.path)}`, `${r.method} ${r.path}`]));
  actual.forEach((row, index) => {
    const stored = documented[index];
    for (const key of Object.keys(row)) if (JSON.stringify(row[key]) !== JSON.stringify(stored[key])) throw new Error(`client source drift: ${row.source}:${row.line} ${key}`);
    const matched = row.forwarding ? [] : row.methods.map(method => {
      const route = routeMap.get(`${method} ${normalize(row.path)}`);
      if (!route) throw new Error(`unregistered/unresolved client call: ${row.source}:${row.line}`);
      return route;
    });
    if (JSON.stringify(matched) !== JSON.stringify(stored.routes)) throw new Error(`client route mapping drift: ${row.source}:${row.line}`);
  });
}
if (require.main === module) {
  const rows = scan();
  if (process.argv[2] === "--check") {
    const read = name => JSON.parse(fs.readFileSync(path.join(ROOT, "docs/company-mail/evidence", name), "utf8"));
    validate(rows, read("R5-CLIENT-CALLS.json"), read("R5-API-MATRIX.json"));
    console.log(`API client inventory PASS: ${rows.length} branches, ${rows.filter(x => x.forwarding).length} explicit transport forwarders`);
  } else process.stdout.write(JSON.stringify(rows, null, 2) + "\n");
}
module.exports = { scan, validate };
