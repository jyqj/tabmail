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
  function symbolic(n, seen = new Set(), bindings = new Map()) {
    if (!n) return "";
    if (ts.isIdentifier(n) && bindings.has(checker.getSymbolAtLocation(n))) return bindings.get(checker.getSymbolAtLocation(n));
    if (ts.isStringLiteral(n) || ts.isNoSubstitutionTemplateLiteral(n)) return n.text;
    if (ts.isParenthesizedExpression(n) || ts.isAsExpression(n)) return symbolic(n.expression, seen, bindings);
    if (ts.isCallExpression(n)) {
      if (n.expression.getText() === "workPath") return "/mailboxes/{id}";
      if (n.expression.getText() === "encodeURIComponent") return "{id}";
      if (n.expression.getText() === "getBaseUrl") return "";
    }
    if (ts.isTemplateExpression(n)) return n.head.text + n.templateSpans.map(s => symbolic(s.expression, seen, bindings) + s.literal.text).join("");
    if (ts.isBinaryExpression(n) && n.operatorToken.kind === ts.SyntaxKind.PlusToken) return symbolic(n.left, seen, bindings) + symbolic(n.right, seen, bindings);
    if (ts.isIdentifier(n)) {
      const sym = checker.getSymbolAtLocation(n);
      const decl = sym?.valueDeclaration;
      if (decl && ts.isVariableDeclaration(decl) && decl.initializer && !seen.has(decl)) {
        const next = new Set(seen); next.add(decl); return symbolic(decl.initializer, next, bindings);
      }
    }
    return "{dynamic}";
  }
  // Expand finite string parameters only for private, directly called helpers.
  // An exported/escaped helper or any unknown argument stays unresolved.
  function parameterMayBeWritten(fn, symbol) {
    function containsParameter(n) {
      if (ts.isIdentifier(n) && (checker.getSymbolAtLocation(n) === symbol ||
          (ts.isShorthandPropertyAssignment(n.parent) && checker.getShorthandAssignmentValueSymbol(n.parent) === symbol))) return true;
      return !!ts.forEachChild(n, containsParameter);
    }
    function visit(n) {
      // Inspect the entire body, including nested closures and unreachable code;
      // this is an immutability check, not a control-flow proof.
      if (ts.isBinaryExpression(n) && n.operatorToken.kind >= ts.SyntaxKind.FirstAssignment &&
          n.operatorToken.kind <= ts.SyntaxKind.LastAssignment && containsParameter(n.left)) return true;
      if ((ts.isPrefixUnaryExpression(n) || ts.isPostfixUnaryExpression(n)) &&
          [ts.SyntaxKind.PlusPlusToken, ts.SyntaxKind.MinusMinusToken].includes(n.operator) && containsParameter(n.operand)) return true;
      if ((ts.isForInStatement(n) || ts.isForOfStatement(n)) && containsParameter(n.initializer)) return true;
      // Direct eval/with prevent proving that lexical parameters are immutable.
      if (ts.isWithStatement(n) || (ts.isCallExpression(n) && ts.isIdentifier(n.expression) && n.expression.text === "eval")) return true;
      return !!ts.forEachChild(n, visit);
    }
    return !!fn.body && visit(fn.body);
  }
  function pathBindings(input, sf) {
    let bindings = [new Map()];
    const parameters = new Set();
    function find(n) {
      if (ts.isIdentifier(n)) {
        const symbol = checker.getSymbolAtLocation(n);
        const decl = symbol?.valueDeclaration;
        if (decl && ts.isParameter(decl) && decl.initializer && ts.isStringLiteral(decl.initializer)) parameters.add(symbol);
      }
      ts.forEachChild(n, find);
    }
    if (input) find(input);
    for (const symbol of parameters) {
      const parameter = symbol.valueDeclaration, fn = parameter.parent;
      if (!ts.isFunctionDeclaration(fn) || !fn.name || fn.modifiers?.some(m => [ts.SyntaxKind.ExportKeyword, ts.SyntaxKind.DefaultKeyword].includes(m.kind))) continue;
      if (parameterMayBeWritten(fn, symbol)) continue;
      const fnSymbol = checker.getSymbolAtLocation(fn.name), index = fn.parameters.indexOf(parameter);
      const values = new Set();
      let unresolved = false;
      function references(n) {
        if (ts.isIdentifier(n) && n !== fn.name && checker.getSymbolAtLocation(n) === fnSymbol) {
          const call = n.parent;
          if (!ts.isCallExpression(call) || call.expression !== n || call.arguments.some(ts.isSpreadElement)) unresolved = true;
          else {
            const arg = call.arguments[index];
            const value = symbolic(arg || parameter.initializer);
            if (value.includes("{dynamic}") || value.includes("{id}")) unresolved = true;
            else values.add(value);
          }
        }
        ts.forEachChild(n, references);
      }
      references(sf);
      if (!unresolved && values.size) bindings = bindings.flatMap(binding => [...values].map(value => new Map([...binding, [symbol, value]])));
    }
    return bindings;
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
        let branch = 0;
        variants.forEach((variant, variantIndex) => pathBindings(variant, sf).forEach(bindings => {
          const target = (["company", "downloadCompanyFile"].includes(callee) ? "/api/v1/company" : "") + symbolic(variant, new Set(), bindings);
          const source = path.relative(root, name).split(path.sep).join("/");
          const forwarding = (source === "web/lib/api/base.ts" && !target.startsWith("/api/v1")) || (source === "web/lib/company.ts" && callee === "request" && target === "/api/v1/company{dynamic}");
          rows.push({ source, line: sf.getLineAndCharacterOfPosition(n.getStart()).line + 1, branch: branch++, callee, owner, expression: variant?.getText() || "", path: target, methods: paired && variants.length === ms.length ? [ms[variantIndex]] : ms, forwarding });
        }));
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
      if (!route) throw new Error(`unregistered/unresolved client call: ${row.source}:${row.line} branch ${row.branch}: ${method} ${row.path}`);
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
