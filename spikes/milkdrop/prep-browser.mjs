// SPIKE — throwaway. Builds browser/presets.json for the in-browser test:
// each preset as butterchurn 3 wants it for its WASM path — baseVals, waves,
// shapes and GLSL shaders from butterchurn's converter, plus the ORIGINAL EEL
// as *_eqs_eel — and with every *_eqs_str (the JavaScript) deleted, so the
// page has no JavaScript from a preset to run even if it wanted to.
//
//   node prep-browser.mjs <pkgs> <out.json> <file.milk>...
import fs from "node:fs";
import path from "node:path";
import { createRequire } from "node:module";
import { readMilk } from "./milk.mjs";

const [pkgs, outFile, ...files] = process.argv.slice(2);
globalThis.window = globalThis; globalThis.self = globalThis;
globalThis.document = { currentScript: { src: "" }, createTextNode: () => ({}), title: "" };
globalThis.location = { href: "" };
const require = createRequire(import.meta.url);
const conv = require(path.join(pkgs, "milkdrop-preset-converter-0.1.2/package/dist/milkdrop-preset-converter.min.js"));

const out = [];
for (const f of files) {
  const text = fs.readFileSync(f, "latin1");
  const p = readMilk(text);
  let c;
  try { c = await conv.convertPreset(text); } catch (e) { out.push({ name: path.basename(f, ".milk"), error: String(e.message || e).slice(0, 100) }); continue; }
  const preset = {
    name: path.basename(f, ".milk"),
    md2: p.version >= 200,
    hlsl: { warp: p.warp.trim().length > 0, comp: p.comp.trim().length > 0 },
    // the converter's own interpretation of base values and the shaders
    baseVals: c.baseVals, warp: c.warp, comp: c.comp,
    // the EEL, verbatim from the .milk
    init_eqs_eel: p.init, frame_eqs_eel: p.frame, pixel_eqs_eel: p.pixel,
    shapes: c.shapes.map((s, i) => ({ baseVals: s.baseVals, init_eqs_eel: p.shapes[i].init, frame_eqs_eel: p.shapes[i].frame })),
    waves: c.waves.map((w, i) => ({ baseVals: w.baseVals, init_eqs_eel: p.waves[i].init, frame_eqs_eel: p.waves[i].frame, point_eqs_eel: p.waves[i].point })),
    // only for the butterchurn 2 + interpreter page, which compiles these itself
    milk: { init: p.init, frame: p.frame, pixel: p.pixel, shapes: p.shapes, waves: p.waves },
  };
  if (JSON.stringify(preset).includes("_eqs_str")) throw new Error("JS leaked into " + f);
  out.push(preset);
}
fs.writeFileSync(outFile, JSON.stringify(out));
console.log(`${out.length} presets, ${out.filter((p) => p.error).length} failed conversion`);
