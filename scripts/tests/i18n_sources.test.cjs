'use strict';
const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { collect } = require('../collect_i18n_keys.cjs');
const { spawnSync } = require('node:child_process');

function scan(source) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'tabmail-i18n-test-'));
  try {
    const file = path.join(root, 'web/features/view.tsx');
    fs.mkdirSync(path.dirname(file), { recursive: true });
    fs.writeFileSync(file, source);
    return collect(root, [file]);
  } finally { fs.rmSync(root, { recursive: true, force: true }); }
}
const keys = source => scan(source).keys.map(v => v.key);

test('literal catalog calls include single/double quote and static template', () => {
  assert.deepEqual(keys('t("alpha"); t(\'beta\'); t(`gamma`);'), ['alpha', 'beta', 'gamma']);
});
test('comments and string examples are not calls', () => {
  assert.deepEqual(keys('// t("fake")\n const example = \'t("fake")\'; /* t("fake") */ t("real");'), ['real']);
});
test('decoded escape sequences keep key identity', () => {
  assert.deepEqual(keys('t("a\\u002eb");'), ['a.b']);
});
test('computed calls reported rather than called checked', () => {
  const result = scan('t(`item.${n}`); t(key); t("ok");');
  assert.equal(result.dynamic_calls, 2);
  assert.deepEqual(result.keys.map(v => v.key), ['ok']);
});
test('useText is identified by local binding, not a file-wide bypass', () => {
  const result = scan('import {useText} from "@/components/company/common"; import {useI18n} from "@/lib/i18n"; function a(){ const t=useText(); t("你好", "Hello"); } function b(){ const {t}=useI18n(); t("missing.key"); }');
  assert.equal(result.inline_calls, 1);
  assert.deepEqual(result.keys.map(v => v.key), ['missing.key']);
});
test('aliased hooks and catalog function are checked', () => {
  assert.deepEqual(keys('import {useI18n as useWords} from "@/lib/i18n"; const {t:translate}=useWords(); translate("missing.key");'), ['missing.key']);
});
test('a fake useText import cannot disable checks', () => {
  assert.deepEqual(keys('import {useText} from "other-package"; const t=useText(); t("missing.key");'), ['missing.key']);
});
test('object t and nested TSX expressions are scanned', () => {
  assert.deepEqual(keys('const view=<div>{ctx.t("outer")}{t("inner")}</div>'), ['outer', 'inner']);
});
test('typed bilingual callbacks are distinct from catalog callbacks', () => {
  const result = scan('type Text=(zh:string,en:string)=>string; function a(t:Text){ t("你好","Hello"); } function b(t:(key:string,params?:Record<string,string>)=>string){ t("missing.key"); }');
  assert.equal(result.inline_calls, 1);
  assert.deepEqual(result.keys.map(v=>v.key), ['missing.key']);
});
test('syntax errors fail the scanner', () => {
  assert.throws(() => scan('const = ; t("key");'));
});
test('empty source list fails', () => {
  assert.throws(() => collect('/tmp', []));
});

for (const [label, key, expected] of [['valid source', 'ok', 0], ['missing key in features', 'feature.missing', 1]]) {
  test(`Python and AST integration: ${label}`, () => {
    const root = fs.mkdtempSync(path.join(os.tmpdir(), 'tabmail-i18n-integration-'));
    try {
      for (const dir of ['app','components','contexts','features','hooks','lib','locales'])
        fs.mkdirSync(path.join(root,'web',dir), {recursive:true});
      for (const locale of ['zh','en'])
        fs.writeFileSync(path.join(root,'web/locales',`${locale}.json`), JSON.stringify({ok:'OK'}));
      fs.writeFileSync(path.join(root,'web/features/view.tsx'), `t(${JSON.stringify(key)});`);
      const result = spawnSync('python3', ['-B', path.join(__dirname,'../check_i18n_keys.py'), '--root',root], {encoding:'utf8'});
      assert.equal(result.status, expected, result.stderr);
      assert.match(expected ? result.stderr : result.stdout, expected ? /feature\.missing.*web\/features\/view\.tsx:1/s : /i18n check PASS/);
    } finally { fs.rmSync(root,{recursive:true,force:true}); }
  });
}
