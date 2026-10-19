// Corpus differential: MilkDrop 2's parse (md2ref.exe) vs the vendored eel-wasm's
// parse (its own parse(), lifted out of butterchurn.min.js), on every block.
//
//   node canon.mjs <md2ref.exe> <bc.mjs copy> <blocks.jsonl> <out.json>
//
// Both trees are mapped onto one canonical form and evaluated by ONE evaluator
// (this file), so any value difference is a difference of parse alone, not of
// how an operator computes. Variables start at the same pseudo-random values
// in both; three ranges are tried, since `&` on values in (-1,1) truncates to 0
// and would hide most differences.
import fs from "node:fs";
import readline from "node:readline";
import { spawn } from "node:child_process";
import { pathToFileURL } from "node:url";

const [md2exe, bcPath, blocksFile, outFile] = process.argv.slice(2);
globalThis.window = globalThis;
const bc = await import(pathToFileURL(bcPath).href);

// ---- MilkDrop 2 pre-pass, as CState::StripLinefeedCharsAndComments -----------
// The Go reader joins a block's lines with "\n"; MilkDrop joins them with a
// control char, strips // and \\ comments to the end of the line, and drops the
// joins. /* */ is left for ns-eel2's own preprocessCode, as MilkDrop leaves it.
function md2Strip(src) {
  let out = "", comment = false;
  for (let i = 0; i < src.length; i++) {
    const c = src[i];
    if (comment) { if (c === "\n") comment = false; continue; }
    if ((c === "\\" && src[i + 1] === "\\") || (c === "/" && src[i + 1] === "/")) { comment = true; continue; }
    if (c !== "\n" && c !== "\r") out += c;
  }
  return out;
}

// ---- md2ref as a coprocess -------------------------------------------------
const proc = spawn(md2exe, [], { stdio: ["pipe", "pipe", "inherit"] });
const md2lines = readline.createInterface({ input: proc.stdout });
const md2queue = [];
let md2cur = [];
md2lines.on("line", (l) => {
  if (l === "END") { md2queue.shift()(md2cur); md2cur = []; } else md2cur.push(l);
});
function md2(src) {
  return new Promise((res) => { md2queue.push(res); proc.stdin.write(src.replace(/[\r\n]/g, " ") + "\n"); });
}

// ---- canonical trees -------------------------------------------------------
// node := number | "name" (variable) | [op, ...args]
function sexpr(text) {
  let i = 0;
  const ws = () => { while (text[i] === " ") i++; };
  function go() {
    ws();
    if (text[i] === "(") {
      i++; ws();
      let j = i; while (text[j] !== " " && text[j] !== ")") j++;
      const head = text.slice(i, j); i = j;
      const args = [];
      for (;;) { ws(); if (text[i] === ")") { i++; break; } args.push(go()); }
      return [head, ...args];
    }
    let j = i; while (j < text.length && text[j] !== " " && text[j] !== ")") j++;
    const tok = text.slice(i, j); i = j;
    const n = Number(tok);
    return Number.isNaN(n) && tok !== "nan" ? tok : n;
  }
  return go();
}
const md2names = {
  "=": "=", _set: "=", seq: "seq", "+": "+", "-": "-", "*": "*", "/": "/", "&": "&", "|": "|", neg: "neg",
  _mod: "%", pow: "pow", _and: "&&", _or: "||", _not: "!", _equal: "==", _noteq: "!=", _below: "<",
  _above: ">", _beleq: "<=", _aboeq: ">=", _if: "if", _mem: "megabuf", _gmem: "gmegabuf",
  _addop: "+=", _subop: "-=", _mulop: "*=", _divop: "/=", _modop: "%=", _orop: "|=", _andop: "&=", _powop: "^=",
};
function fromMd2(t) {
  if (!Array.isArray(t)) return t;
  const [h, ...a] = t;
  if (h === "pos") return fromMd2(a[0]);
  const op = md2names[h] ?? "call:" + h;
  return flat([op, ...a.map(fromMd2)]);
}
function flat(t) { // nested seq, and exec2/exec3, are one sequence
  if (t[0] === "call:exec2" || t[0] === "call:exec3") t = ["seq", ...t.slice(1)];
  if (t[0] !== "seq") return t;
  const out = ["seq"];
  for (const x of t.slice(1)) Array.isArray(x) && x[0] === "seq" ? out.push(...x.slice(1)) : out.push(x);
  return out;
}
const ewCalls = { if: "if", bnot: "!", assign: "=", equal: "==", below: "<", above: ">", int: "call:floor", pow: "pow", megabuf: "megabuf", gmegabuf: "gmegabuf" };
const id16 = (s) => s.toLowerCase().slice(0, 16); // ns-eel.h:126 NSEEL_MAX_VARIABLE_NAMELEN
function fromEw(n) {
  switch (n.type) {
    case "NUMBER_LITERAL": return n.value;
    case "IDENTIFIER": return id16(n.value);
    case "EXPRESSION_BLOCK": return n.body.length === 1 ? fromEw(n.body[0]) : flat(["seq", ...n.body.map(fromEw)]);
    case "UNARY_EXPRESSION":
      if (n.operator === "+") return fromEw(n.value);
      return [n.operator === "-" ? "neg" : "!", fromEw(n.value)];
    case "BINARY_EXPRESSION": return [n.operator === "^" ? "pow" : n.operator, fromEw(n.left), fromEw(n.right)];
    case "LOGICAL_EXPRESSION": return [n.operator, fromEw(n.left), fromEw(n.right)];
    case "ASSIGNMENT_EXPRESSION": return [n.operator, fromEw(n.left), fromEw(n.right)];
    case "CALL_EXPRESSION": {
      const name = n.callee.value.toLowerCase();
      return flat([ewCalls[name] ?? "call:" + name, ...n.arguments.map(fromEw)]);
    }
  }
  throw new Error("unknown node " + n.type);
}
const show = (t) => (Array.isArray(t) ? `(${t.map(show).join(" ")})` : String(t));
const same = (a, b) => show(a) === show(b);

// ---- one evaluator for both -------------------------------------------------
const CLOSE = 0.00001;
const tr = (x) => { try { return BigInt.asIntN(64, BigInt(Math.trunc(x))); } catch { return 0n; } };
function hashName(s) { let h = 7; for (const c of s) h = (h * 31 + c.charCodeAt(0)) % 1000; return h / 1000; }
function evaluate(stmts, seedVals) {
  const env = new Map(Object.entries(seedVals));
  const mem = new Map();
  let budget = 20000;
  const get = (v) => { if (!env.has(v)) env.set(v, seedVal(v)); return env.get(v); };
  const set = (lhs, val) => {
    if (typeof lhs === "string") env.set(lhs, val);
    else if (Array.isArray(lhs) && (lhs[0] === "megabuf" || lhs[0] === "gmegabuf")) mem.set(lhs[0] + Math.floor(ev(lhs[1]) + CLOSE), val);
    return val;
  };
  function ev(t) {
    if (--budget < 0) throw new Error("budget");
    if (typeof t === "number") return t;
    if (typeof t === "string") return get(t);
    const [op, ...a] = t;
    const A = () => ev(a[0]), B = () => ev(a[1]);
    switch (op) {
      case "=": return set(a[0], B());
      case "+=": case "-=": case "*=": case "/=": case "%=": case "|=": case "&=": case "^=": {
        const cur = ev(a[0]); const r = B();
        return set(a[0], ev([op === "^=" ? "pow" : op[0], cur, r]));
      }
      case "seq": { let r = 0; for (const x of a) r = ev(x); return r; }
      case "+": { const x = A(); return x + B(); }
      case "-": { const x = A(); return x - B(); }
      case "*": { const x = A(); return x * B(); }
      case "/": { const x = A(); return x / B(); }
      case "%": { const x = A(), y = B(); const d = Number(tr(Math.abs(y))) >>> 0, n = Number(tr(Math.abs(x))) >>> 0; return d ? n % d : 0; }
      case "pow": { const x = A(); return Math.pow(x, B()); }
      case "&": { const x = A(); return Number(tr(x) & tr(B())); }
      case "|": { const x = A(); return Number(tr(x) | tr(B())); }
      case "neg": return -A();
      case "!": return Math.abs(A()) > CLOSE ? 0 : 1;
      case "&&": return Math.abs(A()) > CLOSE && Math.abs(B()) > CLOSE ? 1 : 0;
      case "||": return Math.abs(A()) > CLOSE || Math.abs(B()) > CLOSE ? 1 : 0;
      case "==": { const x = A(); return Math.abs(x - B()) < CLOSE ? 1 : 0; }
      case "!=": { const x = A(); return Math.abs(x - B()) < CLOSE ? 0 : 1; }
      case "<": { const x = A(); return x < B() ? 1 : 0; }
      case ">": { const x = A(); return x > B() ? 1 : 0; }
      case "<=": { const x = A(); return x <= B() ? 1 : 0; }
      case ">=": { const x = A(); return x >= B() ? 1 : 0; }
      case "if": return Math.abs(A()) > CLOSE ? ev(a[1]) : ev(a[2]);
      case "megabuf": case "gmegabuf": return mem.get(op + Math.floor(A() + CLOSE)) ?? 0;
      case "call:loop": { const n = Math.min(Math.max(0, Math.floor(A())), 16); let r = 0; for (let i = 0; i < n; i++) r = ev(a[1]); return r; }
      case "call:while": { let i = 0; while (i++ < 16 && Math.abs(A()) > CLOSE); return 0; }
    }
    // every other function: a fixed, name-dependent mix of its arguments
    const args = a.map(ev);
    const k = hashName(op);
    switch (op) {
      case "call:sin": return Math.sin(args[0]);
      case "call:cos": return Math.cos(args[0]);
      case "call:abs": return Math.abs(args[0]);
      case "call:sqr": return args[0] * args[0];
      case "call:sqrt": return Math.sqrt(Math.abs(args[0]));
      case "call:min": return Math.min(args[0], args[1]);
      case "call:max": return Math.max(args[0], args[1]);
      case "call:floor": return Math.floor(args[0]);
      case "call:sign": return Math.sign(args[0]);
      case "call:rand": return Math.abs(Math.floor(args[0] * 0.37)) || 0;
    }
    return args.reduce((s, x, i) => s + x * (k + i + 1), k);
  }
  const results = stmts.map((s) => ev(s));
  return { results, env };
}
let SEED_RANGE = [0, 3];
function seedVal(name) {
  let h = 2166136261;
  for (const c of name) h = Math.imul(h ^ c.charCodeAt(0), 16777619);
  const u = ((h >>> 0) % 100000) / 100000;
  return SEED_RANGE[0] + u * (SEED_RANGE[1] - SEED_RANGE[0]);
}
const close = (x, y) => (Number.isNaN(x) && Number.isNaN(y)) || x === y || Math.abs(x - y) <= 1e-9 * Math.max(1, Math.abs(x), Math.abs(y));
function valueDiffers(ta, tb) {
  for (const range of [[0, 3], [-10, 10], [0, 1000]]) {
    SEED_RANGE = range;
    let ra, rb;
    try { ra = evaluate(ta, {}); rb = evaluate(tb, {}); } catch { continue; }
    for (let i = 0; i < ra.results.length; i++) if (!close(ra.results[i], rb.results[i])) return true;
    for (const [k, v] of ra.env) if (!close(v, rb.env.get(k))) return true;
  }
  return false;
}

// where two trees first part: the operator pair at the topmost differing node
function firstSplit(a, b) {
  if (!Array.isArray(a) || !Array.isArray(b) || a[0] !== b[0] || a.length !== b.length) {
    const h = (t) => (Array.isArray(t) ? t[0] : typeof t === "number" ? "num" : "var");
    return { md2: h(a), ew: h(b), md2t: show(a).slice(0, 200), ewt: show(b).slice(0, 200) };
  }
  for (let i = 1; i < a.length; i++) if (!same(a[i], b[i])) return firstSplit(a[i], b[i]);
  return null;
}

// ---- run -------------------------------------------------------------------
const stats = { blocks: 0, empty: 0, md2err: 0, ewerr: 0, bothParsed: 0, treeDiff: 0, valueDiff: 0 };
const presets = { all: new Set(), treeDiff: new Set(), valueDiff: new Set() };
const pairs = new Map(); // "md2op|ewop" -> {stmts, presets:Set, example}
const errs = [];
const rl = readline.createInterface({ input: fs.createReadStream(blocksFile) });
for await (const line of rl) {
  if (!line) continue;
  const b = JSON.parse(line);
  presets.all.add(b.file);
  const src = md2Strip(b.src);
  if (!src.trim()) { stats.empty++; continue; }
  stats.blocks++;
  let ew;
  try { ew = bc.__parse(src); } catch (e) { stats.ewerr++; continue; }
  const out = await md2(src);
  if (out.some((l) => l.startsWith("ERR"))) { stats.md2err++; if (errs.length < 40) errs.push({ file: b.file.split(/[\\/]/).pop(), block: b.block, err: out.find((l) => l.startsWith("ERR")).slice(0, 160) }); continue; }
  stats.bothParsed++;
  const ta = out.filter((l) => l.startsWith("T ")).map((l) => fromMd2(sexpr(l.slice(2))));
  const tb = ew.body.map(fromEw);
  let differs = ta.length !== tb.length;
  const splits = [];
  for (let i = 0; i < Math.max(ta.length, tb.length); i++) {
    if (ta[i] === undefined || tb[i] === undefined) { differs = true; continue; }
    if (!same(ta[i], tb[i])) { differs = true; splits.push({ i, s: firstSplit(ta[i], tb[i]) }); }
  }
  if (!differs) continue;
  stats.treeDiff++;
  presets.treeDiff.add(b.file);
  const vd = ta.length === tb.length && valueDiffers(ta, tb);
  if (vd) { stats.valueDiff++; presets.valueDiff.add(b.file); }
  for (const { i, s } of splits) {
    const k = `${s.md2} | ${s.ew}`;
    let e = pairs.get(k);
    if (!e) pairs.set(k, (e = { stmts: 0, valueStmts: 0, presets: new Set(), valuePresets: new Set(), examples: [] }));
    e.stmts++;
    e.presets.add(b.file);
    const stmtVd = valueDiffers([ta[i]], [tb[i]]);
    if (stmtVd) { e.valueStmts++; e.valuePresets.add(b.file); }
    if ((stmtVd ? e.examples.filter(x=>x.value).length < 4 : e.examples.filter(x=>!x.value).length < 2)) e.examples.push({ file: b.file.split(/[\\/]/).pop(), block: b.block, md2: s.md2t, ew: s.ewt, value: stmtVd });
  }
}
proc.stdin.end();
const report = {
  stats, presets: { all: presets.all.size, treeDiff: presets.treeDiff.size, valueDiff: presets.valueDiff.size },
  pairs: [...pairs].sort((x, y) => y[1].presets.size - x[1].presets.size).map(([k, e]) => ({ split: k, stmts: e.stmts, valueStmts: e.valueStmts, presets: e.presets.size, valuePresets: e.valuePresets.size, valuePresetList: [...e.valuePresets], examples: e.examples })),
  md2errSample: errs,
  valueDiffPresets: [...presets.valueDiff].map((f) => f.split(/[\\/]presets[\\/]/).pop()).sort(),
};
fs.writeFileSync(outFile, JSON.stringify(report, null, 1));
console.log(JSON.stringify({ stats: report.stats, presets: report.presets }, null, 1));
for (const p of report.pairs) console.log(String(p.presets).padStart(5), String(p.valuePresets).padStart(5), p.split);
