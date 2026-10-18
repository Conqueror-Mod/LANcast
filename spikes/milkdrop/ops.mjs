// SPIKE — how many presets use the operators whose precedence the engines disagree on?
import fs from "node:fs"; import path from "node:path"; import { readMilk } from "./milk.mjs"; import { tokenize } from "./eel.mjs";
function walk(d, o = []) { for (const e of fs.readdirSync(d, { withFileTypes: true })) { const p = path.join(d, e.name); e.isDirectory() ? walk(p, o) : /\.milk$/i.test(e.name) && o.push(p); } return o; }
let n = 0; const c = { amp: 0, bar: 0, caret: 0, negCaret: 0, any: 0 };
for (const d of process.argv.slice(2)) for (const f of walk(d)) {
  n++;
  const p = readMilk(fs.readFileSync(f, "latin1"));
  const src = [p.init, p.frame, p.pixel, ...p.waves.flatMap((w) => [w.init, w.frame, w.point]), ...p.shapes.flatMap((s) => [s.init, s.frame])].join(";");
  let t; try { t = tokenize(src); } catch { continue; }
  const has = (v) => t.some((x) => x.t === "op" && x.v === v);
  // a ^ whose left operand starts with unary minus: "-x^" or "(-x)^" can't be told apart cheaply; count "- id/num ^"
  let neg = false; for (let i = 0; i + 2 < t.length; i++) if (t[i].v === "-" && (i === 0 || t[i - 1].t === "op" && t[i - 1].v !== ")") && t[i + 2].v === "^") neg = true;
  const a = has("&"), b = has("|"), ca = has("^");
  if (a) c.amp++; if (b) c.bar++; if (ca) c.caret++; if (neg) c.negCaret++; if (a || b || ca) c.any++;
}
console.log(JSON.stringify({ presets: n, ...c }));
