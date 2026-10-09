#!/usr/bin/env node
'use strict';
// Use the project's installed compiler, never download or evaluate app code.
const fs = require('node:fs');
const path = require('node:path');
const ts = require('../web/node_modules/typescript');

function collect(root, files) {
  if (!Array.isArray(files) || !files.length) throw new Error('source list is empty');
  const program = ts.createProgram(files, { noResolve: true, noLib: true, jsx: ts.JsxEmit.Preserve });
  const checker = program.getTypeChecker();
  const ignored = new Set();
  const aliases = new Set();
  const sources = files.map(file => {
    const source = program.getSourceFile(file);
    if (!source) throw new Error(`source missing: ${file}`);
    const errors = program.getSyntacticDiagnostics(source);
    if (errors.length) throw new Error(`${file}: ${ts.flattenDiagnosticMessageText(errors[0].messageText, '\n')}`);
    return source;
  });
  const symbol = node => checker.getSymbolAtLocation(node);
  function importKind(node) {
    const sym = symbol(node);
    for (const declaration of sym?.declarations || []) {
      if (!ts.isImportSpecifier(declaration)) continue;
      const imported = (declaration.propertyName || declaration.name).text;
      const module = declaration.parent.parent.parent.moduleSpecifier.text;
      const resolved = module.startsWith('@/') ? path.join(root, 'web', module.slice(2))
        : path.resolve(path.dirname(declaration.getSourceFile().fileName), module);
      if (imported === 'useText' && resolved === path.join(root, 'web/components/company/common')) return 'inline';
      if (imported === 'useI18n' && resolved === path.join(root, 'web/lib/i18n')) return 'catalog';
    }
    // The inline helper is also called in its own defining module.
    if (node.text === 'useText' && path.resolve(node.getSourceFile().fileName) === path.join(root, 'web/components/company/common.tsx')) return 'inline';
    return null;
  }
  for (const source of sources) {
    function bindings(node) {
      if (ts.isVariableDeclaration(node) && node.initializer && ts.isCallExpression(node.initializer)) {
        const kind = importKind(node.initializer.expression);
        if (kind === 'inline' && ts.isIdentifier(node.name)) ignored.add(symbol(node.name));
        if (kind === 'catalog' && ts.isObjectBindingPattern(node.name)) {
          for (const entry of node.name.elements) {
            if ((entry.propertyName || entry.name).text === 't') aliases.add(symbol(entry.name));
          }
        }
      }
      ts.forEachChild(node, bindings);
    }
    bindings(source);
  }
  ignored.delete(undefined);
  aliases.delete(undefined);
  // Inline translators can also arrive through an explicitly typed callback,
  // e.g. sendPolicyLabel(t: (zh: string, en: string) => string). Do not suppress
  // catalog callbacks whose second argument is an interpolation object.
  function inlineCallback(callee) {
    const declarations = symbol(callee)?.declarations || [];
    if (!declarations.some(ts.isParameter)) return false;
    const signatures = checker.getTypeAtLocation(callee).getCallSignatures();
    if (signatures.length !== 1 || signatures[0].parameters.length !== 2) return false;
    return signatures[0].parameters.every(parameter => {
      const declaration = parameter.valueDeclaration;
      return declaration && !declaration.questionToken && !declaration.dotDotDotToken &&
        (checker.getTypeOfSymbolAtLocation(parameter, callee).flags & ts.TypeFlags.String) !== 0;
    });
  }
  const result = { files: sources.length, keys: [], dynamic_calls: 0, inline_calls: 0 };
  for (const source of sources) {
    function calls(node) {
      if (ts.isCallExpression(node)) {
        const callee = node.expression;
        if (ignored.has(symbol(callee)) || inlineCallback(callee)) {
          result.inline_calls++;
        } else if ((ts.isIdentifier(callee) && (callee.text === 't' || aliases.has(symbol(callee))))
                   || (ts.isPropertyAccessExpression(callee) && callee.name.text === 't')) {
          const first = node.arguments[0];
          if (first && (ts.isStringLiteral(first) || ts.isNoSubstitutionTemplateLiteral(first))) {
            const line = source.getLineAndCharacterOfPosition(first.getStart(source)).line + 1;
            result.keys.push({ key: first.text, location: `${path.relative(root, source.fileName)}:${line}` });
          } else result.dynamic_calls++;
        }
      }
      ts.forEachChild(node, calls);
    }
    calls(source);
  }
  return result;
}

module.exports = { collect };
if (require.main === module) {
  try {
    const request = JSON.parse(fs.readFileSync(0, 'utf8'));
    process.stdout.write(JSON.stringify(collect(path.resolve(request.root), request.files)) + '\n');
  } catch (error) {
    process.stderr.write(`i18n source scan failed: ${error.message}\n`);
    process.exitCode = 1;
  }
}
