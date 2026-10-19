// SPIKE (ADR 0091 step g). Does the WASI build reproduce the native converter?
//
//   node fidelity.mjs <results.jsonl> <out.json> <pack dir>...
//
// butterchurn's preset packs hold GLSL made by the native addon, and no HLSL.
// The corpus holds the HLSL. A pack preset and a corpus .milk with the same
// file name are taken to be the same preset; for each one this compares the
// pack's warp and comp with processOptimizedShader(our output), byte for byte
// and then with whitespace collapsed.
import fs from "node:fs";
import path from "node:path";
import { processOptimizedShader } from "./pipeline.mjs";

const [resultsPath, outPath, ...packs] = process.argv.slice(2);
const ours = new Map(); // "<file name lowercased, no ext>#warp" -> [result...]
for (const line of fs.readFileSync(resultsPath, "utf8").split("\n")) {
  if (!line) continue;
  const r = JSON.parse(line);
  const cut = r.id.lastIndexOf("#"), file = r.id.slice(0, cut), kind = r.id.slice(cut + 1);
  const key = `${path.basename(file).replace(/\.milk$/i, "").toLowerCase()}#${kind}`;
  if (!ours.has(key)) ours.set(key, []);
  ours.get(key).push(r);
}

const walk = (d) => fs.readdirSync(d, { withFileTypes: true }).flatMap((e) =>
  e.isDirectory() ? walk(path.join(d, e.name)) : e.name.endsWith(".json") && e.name !== "package.json" ? [path.join(d, e.name)] : []);
const squash = (s) => s.replace(/\s+/g, " ").trim();

const tally = {};
const bump = (k) => (tally[k] = (tally[k] ?? 0) + 1);
const examples = {};
for (const pack of packs) {
  for (const f of walk(pack)) {
    let j;
    try { j = JSON.parse(fs.readFileSync(f, "utf8")); } catch { continue; }
    const name = path.basename(f, ".json").toLowerCase();
    for (const kind of ["warp", "comp"]) {
      const theirs = j[kind] ?? "";
      const cands = ours.get(`${name}#${kind}`);
      if (!cands) { bump(theirs ? `${kind}: pack has shader, no corpus match` : `${kind}: neither`); continue; }
      const mine = cands.map((r) => (r.rc === 0 ? processOptimizedShader(r.glsl) : null));
      if (!theirs) { bump(`${kind}: pack empty, corpus has shader`); continue; }
      if (mine.some((m) => m === theirs)) bump(`${kind}: identical`);
      else if (mine.some((m) => m !== null && squash(m) === squash(theirs))) bump(`${kind}: identical but whitespace`);
      else if (mine.every((m) => m === null)) { bump(`${kind}: pack converted, WASI failed`); (examples.wasiFailed ??= []).push(`${name}#${kind} rc=${cands[0].rc}`); }
      else { bump(`${kind}: differs`); (examples.differs ??= []).push(`${name}#${kind}`); }
    }
  }
}
for (const k of Object.keys(examples)) examples[k] = examples[k].slice(0, 12);
fs.writeFileSync(outPath, JSON.stringify({ tally, examples }, null, 1));
console.log(JSON.stringify(tally, null, 1));
