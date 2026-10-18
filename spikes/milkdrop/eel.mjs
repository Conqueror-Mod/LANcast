// SPIKE — throwaway. An interpreter for MilkDrop's EEL2 subset.
//
// Text -> tokens -> AST -> a tree of JavaScript closures. No string ever
// becomes code: there is no eval, no new Function, no Function constructor
// reached by any path. The closures take butterchurn's variable object `a`
// and return it, which is the shape butterchurn's loadPreset accepts when
// `preset.init_eqs` is already a function (butterchurn.js, loadPreset).
//
// Semantics mirror butterchurn's converter (milkdrop-eel-parser) rather than
// ns-eel2 itself, because the point is to be a drop-in for what it emits:
//   if(c,a,b)    lazy, c taken when |c| > 1e-5
//   loop(n, e)   n evaluated once, floored, capped at 1048576
//   while(e)     do-while, runs while |e| > 1e-5, capped at 1048576
//   megabuf(i)   a.megabuf[floor(i)], also an lvalue; gmegabuf likewise
//   rand(x)      integer in [0, floor(x)) — the converter emits randint
//   / and %      zero divisor gives 0
//   == !=        epsilon 1e-5; < > <= >= exact

export class EelError extends Error {
  constructor(msg, pos) {
    super(msg);
    this.pos = pos;
  }
}

const EPS = 0.00001;
const LOOP_CAP = 1048576;

// ---- tokens --------------------------------------------------------------------

const PUNCT = [
  "+=", "-=", "*=", "/=", "%=", "^=", "|=", "&=",
  "==", "!=", "<=", ">=", "&&", "||", "<<", ">>",
  "+", "-", "*", "/", "%", "^", "|", "&", "~", "!", "<", ">", "=", "?", ":", ";", ",", "(", ")", "[", "]",
];

export function tokenize(src) {
  const out = [];
  let i = 0;
  const n = src.length;
  while (i < n) {
    const c = src[i];
    if (c === " " || c === "\t" || c === "\r" || c === "\n") { i++; continue; }
    if (c === "/" && src[i + 1] === "/") { while (i < n && src[i] !== "\n") i++; continue; }
    if (c === "/" && src[i + 1] === "*") {
      const end = src.indexOf("*/", i + 2);
      i = end < 0 ? n : end + 2;
      continue;
    }
    // numbers: 1  1.  .5  1e-3  $x1F  $pi $e $phi
    if ((c >= "0" && c <= "9") || (c === "." && src[i + 1] >= "0" && src[i + 1] <= "9")) {
      const m = /^(\d+\.?\d*|\.\d+)([eE][+-]?\d+)?/.exec(src.slice(i));
      out.push({ t: "num", v: parseFloat(m[0]), pos: i });
      i += m[0].length;
      continue;
    }
    if (c === "$") {
      const rest = src.slice(i);
      let m;
      if ((m = /^\$x[0-9a-fA-F]+/.exec(rest))) { out.push({ t: "num", v: parseInt(m[0].slice(2), 16), pos: i }); i += m[0].length; continue; }
      if ((m = /^\$(pi|e|phi)\b/i.exec(rest))) {
        const k = m[1].toLowerCase();
        out.push({ t: "num", v: k === "pi" ? Math.PI : k === "e" ? Math.E : 1.61803398874989, pos: i });
        i += m[0].length;
        continue;
      }
      if ((m = /^\$'(.)'/.exec(rest))) { out.push({ t: "num", v: m[1].charCodeAt(0), pos: i }); i += m[0].length; continue; }
      throw new EelError(`unknown $ constant`, i);
    }
    if (/[A-Za-z_]/.test(c)) {
      const m = /^[A-Za-z_][A-Za-z0-9_.]*/.exec(src.slice(i));
      out.push({ t: "id", v: m[0].toLowerCase(), pos: i });
      i += m[0].length;
      continue;
    }
    let p = null;
    for (const q of PUNCT) if (src.startsWith(q, i)) { p = q; break; }
    if (!p) throw new EelError(`unexpected character ${JSON.stringify(c)}`, i);
    out.push({ t: "op", v: p, pos: i });
    i += p.length;
  }
  out.push({ t: "eof", pos: n });
  return out;
}

// ---- parser (precedence climbing) --------------------------------------------------

// Binary operators, loosest first. EEL2's documented order: unary binds
// tighter than ^, then * / %, then + -, then | & ~, comparisons, && ||.
const BIN = [
  ["||"],
  ["&&"],
  ["==", "!=", "<", ">", "<=", ">="],
  ["|", "&", "~"],
  ["+", "-"],
  ["*", "/", "%", "<<", ">>"],
  ["^"],
];
const ASSIGN = new Set(["=", "+=", "-=", "*=", "/=", "%=", "^=", "|=", "&="]);

export function parse(src) {
  const toks = tokenize(src);
  let k = 0;
  const peek = () => toks[k];
  const isOp = (v) => toks[k].t === "op" && toks[k].v === v;
  const eat = (v) => {
    if (!isOp(v)) throw new EelError(`expected ${v}, found ${toks[k].v ?? toks[k].t}`, toks[k].pos);
    k++;
  };

  // seq := expr (';' expr)* [';']   — the value of a sequence is its last expression
  function seq(stop) {
    const items = [];
    for (;;) {
      while (isOp(";")) k++;
      if (peek().t === "eof" || (stop && isOp(stop))) break;
      items.push(assign());
      if (isOp(";")) continue;
      if (peek().t === "eof" || (stop && isOp(stop))) break;
      throw new EelError(`expected ; found ${peek().v ?? peek().t}`, peek().pos);
    }
    if (items.length === 1) return items[0];
    return { k: "seq", items };
  }

  function assign() {
    const lhs = ternary();
    if (peek().t === "op" && ASSIGN.has(peek().v)) {
      const op = peek().v;
      const pos = peek().pos;
      k++;
      if (!(lhs.k === "var" || lhs.k === "mem")) throw new EelError(`cannot assign to ${lhs.k}`, pos);
      return { k: "assign", op, target: lhs, value: assign() };
    }
    return lhs;
  }

  function ternary() {
    const c = binary(0);
    if (isOp("?")) {
      k++;
      const a = assign();
      let b = { k: "num", v: 0 };
      if (isOp(":")) { k++; b = assign(); }
      return { k: "if", c, a, b };
    }
    return c;
  }

  function binary(level) {
    if (level >= BIN.length) return unary();
    let lhs = binary(level + 1);
    for (;;) {
      const t = peek();
      if (t.t !== "op" || !BIN[level].includes(t.v)) return lhs;
      k++;
      // ^ is right-associative, everything else left
      const rhs = t.v === "^" ? binary(level) : binary(level + 1);
      lhs = { k: "bin", op: t.v, l: lhs, r: rhs };
    }
  }

  function unary() {
    if (isOp("-")) { k++; return { k: "neg", e: unary() }; }
    if (isOp("+")) { k++; return unary(); }
    if (isOp("!")) { k++; return { k: "not", e: unary() }; }
    return postfix();
  }

  function postfix() {
    let e = primary();
    while (isOp("[")) {
      k++;
      const idx = isOp("]") ? { k: "num", v: 0 } : seq("]");
      eat("]");
      e = { k: "mem", buf: "megabuf", base: e, idx, bracket: true };
    }
    return e;
  }

  function primary() {
    const t = peek();
    if (t.t === "num") { k++; return { k: "num", v: t.v }; }
    if (t.t === "op" && t.v === "(") {
      k++;
      const e = seq(")");
      eat(")");
      return e;
    }
    if (t.t === "id") {
      k++;
      if (isOp("(")) {
        k++;
        const args = [];
        if (!isOp(")")) {
          for (;;) {
            args.push(seq2());
            if (isOp(",")) { k++; continue; }
            break;
          }
        }
        eat(")");
        return call(t.v, args, t.pos);
      }
      return { k: "var", name: t.v };
    }
    throw new EelError(`unexpected ${t.v ?? t.t}`, t.pos);
  }

  // A function argument may itself be a ;-sequence: loop(10, a+=1; b+=a)
  function seq2() {
    const items = [];
    for (;;) {
      while (isOp(";")) k++;
      if (isOp(",") || isOp(")")) break;
      items.push(assign());
      if (isOp(";")) continue;
      break;
    }
    if (items.length === 0) return { k: "num", v: 0 };
    return items.length === 1 ? items[0] : { k: "seq", items };
  }

  function call(name, args, pos) {
    if (name === "megabuf" || name === "gmegabuf") {
      if (args.length !== 1) throw new EelError(`${name} takes 1 argument`, pos);
      return { k: "mem", buf: name, base: null, idx: args[0] };
    }
    const arity = FUNCS[name]?.n ?? SPECIAL[name];
    if (arity === undefined) throw new EelError(`unknown function ${name}()`, pos);
    if (Array.isArray(arity) ? !arity.includes(args.length) : args.length !== arity)
      throw new EelError(`${name}() takes ${arity} arguments, given ${args.length}`, pos);
    return { k: "call", name, args };
  }

  const tree = seq(null);
  if (peek().t !== "eof") throw new EelError(`trailing input`, peek().pos);
  return tree;
}

// ---- built-ins ---------------------------------------------------------------------

function rand(x) {
  const xf = Math.floor(x);
  return xf < 1 ? Math.random() : Math.random() * xf;
}
function isFin(z) { return Number.isFinite(z); }

// Eager built-ins: arguments evaluated, then f applied.
export const FUNCS = {
  sin: { n: 1, f: Math.sin }, cos: { n: 1, f: Math.cos }, tan: { n: 1, f: Math.tan },
  asin: { n: 1, f: Math.asin }, acos: { n: 1, f: Math.acos }, atan: { n: 1, f: Math.atan },
  atan2: { n: 2, f: Math.atan2 },
  sqr: { n: 1, f: (x) => x * x },
  sqrt: { n: 1, f: (x) => Math.sqrt(Math.abs(x)) },
  invsqrt: { n: 1, f: (x) => 1 / Math.sqrt(Math.abs(x)) },
  pow: { n: 2, f: (x, y) => { const z = Math.pow(x, y); return isFin(z) ? z : 0; } },
  exp: { n: 1, f: Math.exp },
  log: { n: 1, f: Math.log },
  log10: { n: 1, f: (x) => Math.log(x) * Math.LOG10E },
  abs: { n: 1, f: Math.abs },
  min: { n: 2, f: Math.min }, max: { n: 2, f: Math.max },
  sign: { n: 1, f: (x) => (x > 0 ? 1 : x < 0 ? -1 : 0) },
  rand: { n: 1, f: (x) => Math.floor(rand(x)) },
  int: { n: 1, f: Math.floor }, floor: { n: 1, f: Math.floor }, ceil: { n: 1, f: Math.ceil },
  sigmoid: { n: 2, f: (x, y) => { const t = 1 + Math.exp(-x * y); return Math.abs(t) > EPS ? 1 / t : 0; } },
  bnot: { n: 1, f: (x) => (Math.abs(x) < EPS ? 1 : 0) },
  equal: { n: 2, f: (x, y) => (Math.abs(x - y) < EPS ? 1 : 0) },
  above: { n: 2, f: (x, y) => (x > y ? 1 : 0) },
  below: { n: 2, f: (x, y) => (x < y ? 1 : 0) },
  bor: { n: 2, f: (x, y) => (Math.abs(x) > EPS || Math.abs(y) > EPS ? 1 : 0) },
  band: { n: 2, f: (x, y) => (Math.abs(x) > EPS && Math.abs(y) > EPS ? 1 : 0) },
  fmod: { n: 2, f: (x, y) => (y === 0 ? 0 : x % y) },
  exec2: { n: 2, f: (x, y) => y },
  exec3: { n: 3, f: (x, y, z) => z },
};
// Built-ins that control evaluation themselves.
const SPECIAL = { if: 3, loop: 2, while: 1, memcpy: 3, memset: 3, assign: 2 };

// Names a preset may not own, because butterchurn's variable object is a plain
// object and these would reach Object.prototype (constructor, __proto__ …).
const RESERVED = new Set(Object.getOwnPropertyNames(Object.prototype).map((s) => s.toLowerCase()));
export function safeName(n) {
  return RESERVED.has(n) ? "_u_" + n : n;
}

// ---- compile AST -> closures -----------------------------------------------------

const truthy = (x) => Math.abs(x) > EPS;

export function compile(node, stats = { loopCapHits: 0 }) {
  const c = (n) => compile(n, stats);
  switch (node.k) {
    case "num": { const v = node.v; return () => v; }
    case "var": {
      const name = safeName(node.name);
      return (a) => { const v = a[name]; return v === undefined ? 0 : v; };
    }
    case "seq": {
      const fs = node.items.map(c);
      return (a) => { let v = 0; for (let i = 0; i < fs.length; i++) v = fs[i](a); return v; };
    }
    case "neg": { const e = c(node.e); return (a) => -e(a); }
    case "not": { const e = c(node.e); return (a) => (truthy(e(a)) ? 0 : 1); }
    case "if": {
      const cc = c(node.c), ca = c(node.a), cb = c(node.b);
      return (a) => (truthy(cc(a)) ? ca(a) : cb(a));
    }
    case "bin": {
      const l = c(node.l), r = c(node.r);
      switch (node.op) {
        case "+": return (a) => l(a) + r(a);
        case "-": return (a) => l(a) - r(a);
        case "*": return (a) => l(a) * r(a);
        case "/": return (a) => { const x = l(a), y = r(a); return y === 0 ? 0 : x / y; };
        case "%": return (a) => { const x = l(a), y = r(a); return y === 0 ? 0 : Math.floor(x) % Math.floor(y); };
        case "^": return (a) => { const z = Math.pow(l(a), r(a)); return isFin(z) ? z : 0; };
        case "|": return (a) => Math.floor(l(a)) | Math.floor(r(a));
        case "&": return (a) => Math.floor(l(a)) & Math.floor(r(a));
        case "~": return (a) => Math.floor(l(a)) ^ Math.floor(r(a));
        case "<<": return (a) => Math.floor(l(a)) << Math.floor(r(a));
        case ">>": return (a) => Math.floor(l(a)) >> Math.floor(r(a));
        case "==": return (a) => (Math.abs(l(a) - r(a)) < EPS ? 1 : 0);
        case "!=": return (a) => (Math.abs(l(a) - r(a)) < EPS ? 0 : 1);
        case "<": return (a) => (l(a) < r(a) ? 1 : 0);
        case ">": return (a) => (l(a) > r(a) ? 1 : 0);
        case "<=": return (a) => (l(a) <= r(a) ? 1 : 0);
        case ">=": return (a) => (l(a) >= r(a) ? 1 : 0);
        case "&&": return (a) => (truthy(l(a)) && truthy(r(a)) ? 1 : 0);
        case "||": return (a) => (truthy(l(a)) || truthy(r(a)) ? 1 : 0);
      }
      throw new EelError(`operator ${node.op}`);
    }
    case "mem": {
      if (node.bracket) throw new EelError(`x[i] memory addressing is not supported`);
      const buf = node.buf, idx = c(node.idx);
      return (a) => { const v = a[buf][Math.floor(idx(a))]; return v === undefined ? 0 : v; };
    }
    case "assign": {
      const op = node.op, value = c(node.value);
      const combine = COMBINE[op];
      if (node.target.k === "var") {
        const name = safeName(node.target.name);
        if (op === "=") return (a) => (a[name] = value(a));
        return (a) => { const cur = a[name] ?? 0; return (a[name] = combine(cur, value(a))); };
      }
      if (node.target.bracket) throw new EelError(`x[i] memory addressing is not supported`);
      const buf = node.target.buf, idx = c(node.target.idx);
      // Index before value, as the converter's a['megabuf'][Math.floor(i)] = v does.
      // Out of range writes are dropped: butterchurn's arrays are fixed at 1048576.
      return (a) => {
        const i = Math.floor(idx(a));
        const m = a[buf];
        const v = op === "=" ? value(a) : combine(m[i] ?? 0, value(a));
        if (i >= 0 && i < m.length) m[i] = v;
        return v;
      };
    }
    case "call": {
      const args = node.args.map(c);
      switch (node.name) {
        case "if": { const [x, y, z] = args; return (a) => (truthy(x(a)) ? y(a) : z(a)); }
        case "loop": {
          const [n, body] = args;
          return (a) => {
            let count = Math.floor(n(a));
            if (count > LOOP_CAP) { count = LOOP_CAP; stats.loopCapHits++; }
            for (let i = 0; i < count; i++) body(a);
            return 0;
          };
        }
        case "while": {
          const [body] = args;
          return (a) => {
            let i = 0;
            while (truthy(body(a))) if (++i >= LOOP_CAP) { stats.loopCapHits++; break; }
            return 0;
          };
        }
        case "assign": {
          const t = node.args[0];
          if (t.k !== "var") throw new EelError(`assign() needs a variable`);
          return compile({ k: "assign", op: "=", target: t, value: node.args[1] }, stats);
        }
        case "memcpy": {
          const [d, s, l] = args;
          return (a) => {
            let dst = Math.floor(d(a)), src = Math.floor(s(a)), len = Math.floor(l(a));
            const m = a.megabuf;
            if (src < 0) { len += src; dst -= src; src = 0; }
            if (dst < 0) { len += dst; src -= dst; dst = 0; }
            len = Math.min(len, m.length - src, m.length - dst);
            if (len > 0) m.copyWithin(dst, src, src + len);
            return dst;
          };
        }
        case "memset": {
          const [d, v, l] = args;
          return (a) => {
            const dst = Math.max(0, Math.floor(d(a))), val = v(a);
            const len = Math.min(Math.floor(l(a)), a.megabuf.length - dst);
            if (len > 0) a.megabuf.fill(val, dst, dst + len);
            return dst;
          };
        }
      }
      const f = FUNCS[node.name].f;
      if (args.length === 1) { const [x] = args; return (a) => f(x(a)); }
      if (args.length === 2) { const [x, y] = args; return (a) => f(x(a), y(a)); }
      return (a) => f(...args.map((g) => g(a)));
    }
  }
  throw new EelError(`node ${node.k}`);
}

const COMBINE = {
  "+=": (x, y) => x + y,
  "-=": (x, y) => x - y,
  "*=": (x, y) => x * y,
  "/=": (x, y) => (y === 0 ? 0 : x / y),
  "%=": (x, y) => (y === 0 ? 0 : Math.floor(x) % Math.floor(y)),
  "^=": (x, y) => { const z = Math.pow(x, y); return isFin(z) ? z : 0; },
  "|=": (x, y) => Math.floor(x) | Math.floor(y),
  "&=": (x, y) => Math.floor(x) & Math.floor(y),
};

/** An equation block as butterchurn wants it: (a) => a. */
export function equations(src, stats) {
  const tree = parse(src);
  const run = compile(tree, stats);
  return (a) => { run(a); return a; };
}
