// SPIKE (ADR 0091 step g). Mutated shaders, to see how the converter fails.
//
//   node mutate.mjs <jobs.jsonl> <out.jsonl> <count> [seed]
//
// Not a fuzzer with coverage feedback: random byte edits, truncations,
// duplicated and deleted spans, and very deep nesting, applied to real
// prepared shaders. The question is only what an adversarial upload does to
// the host: a wrong answer, a refusal, a trap, or a timeout.
import fs from "node:fs";

const [inPath, outPath, countArg, seedArg] = process.argv.slice(2);
let seed = +(seedArg || 1);
const rnd = () => ((seed = (seed * 1664525 + 1013904223) >>> 0) / 4294967296);
const pick = (n) => Math.floor(rnd() * n);
const src = fs.readFileSync(inPath, "utf8").split("\n").filter(Boolean).map((l) => JSON.parse(l).hlsl);
const TOK = ["{", "}", "(", ")", ";", "[", "]", "#define a a(", "#if", "#endif", "float4", "for(;;)", "\0", "\\", "/*", "\"", "'", "1e9999", "tex2D(", ",", "?", ":", "struct s{", "return", "static", "uniform"];

const out = fs.openSync(outPath, "w");
const n = +countArg;
for (let i = 0; i < n; i++) {
  let s = src[pick(src.length)];
  const kind = i % 6;
  if (kind === 0) for (let k = 0; k < 1 + pick(8); k++) { const p = pick(s.length); s = s.slice(0, p) + String.fromCharCode(pick(128)) + s.slice(p + 1); }
  else if (kind === 1) s = s.slice(0, pick(s.length));
  else if (kind === 2) for (let k = 0; k < 1 + pick(6); k++) { const p = pick(s.length); s = s.slice(0, p) + TOK[pick(TOK.length)] + s.slice(p); }
  else if (kind === 3) { const a = pick(s.length), b = Math.min(s.length, a + pick(400)); s = s.slice(0, b) + s.slice(a, b).repeat(1 + pick(20)) + s.slice(b); }
  else if (kind === 4) { const a = pick(s.length), b = Math.min(s.length, a + pick(400)); s = s.slice(0, a) + s.slice(b); }
  else { const d = 200 + pick(20000); const at = s.lastIndexOf("return float4"); s = s.slice(0, at) + "ret = " + "(".repeat(d) + "1" + ")".repeat(d) + ";\n" + s.slice(at); }
  fs.writeSync(out, JSON.stringify({ id: `m${i}#k${kind}`, hlsl: s }) + "\n");
}
fs.closeSync(out);
