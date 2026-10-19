// SPIKE (ADR 0091 step g). Corpus -> jobs.jsonl for the wazero host.
//
//   node jobs.mjs <out.jsonl> <corpus dir>...
//
// One job per non-empty warp or comp shader. The id is "<pack>/<file>#warp".
// Also writes <out>.meta.json: per preset, its version and which shaders it has.
import fs from "node:fs";
import path from "node:path";
import { splitShaders, prepareShader } from "./pipeline.mjs";

const [out, ...roots] = process.argv.slice(2);
const walk = (d) => fs.readdirSync(d, { withFileTypes: true }).flatMap((e) =>
  e.isDirectory() ? walk(path.join(d, e.name)) : /\.milk$/i.test(e.name) ? [path.join(d, e.name)] : []);

const w = fs.openSync(out, "w");
const meta = {};
let presets = 0, jobs = 0;
for (const root of roots) {
  const pack = path.basename(root);
  for (const f of walk(root).sort()) {
    presets++;
    const id = `${pack}/${path.relative(root, f).replaceAll("\\", "/")}`;
    const text = fs.readFileSync(f, "utf8");
    const s = splitShaders(text);
    meta[id] = { version: s.version, warp: !!s.warp, comp: !!s.comp };
    for (const kind of ["warp", "comp"]) {
      if (!s[kind]) continue;
      fs.writeSync(w, JSON.stringify({ id: `${id}#${kind}`, hlsl: prepareShader(s[kind]) }) + "\n");
      jobs++;
    }
  }
}
fs.closeSync(w);
fs.writeFileSync(out + ".meta.json", JSON.stringify(meta));
console.error(`${presets} presets, ${jobs} shaders`);
