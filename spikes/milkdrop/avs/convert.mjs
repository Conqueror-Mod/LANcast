// SPIKE — convert the VISBOT .avs presets with webvsc and list every component.
//   node convert.mjs <node_modules dir> <avs dir> <out dir>
import fs from "node:fs"; import path from "node:path"; import { createRequire } from "node:module";
const [nm, dir, out] = process.argv.slice(2);
const require = createRequire(path.resolve(nm, "x.js"));
const { convertFileSync } = require("@visbot/webvsc"); // 2.3.0 on npm; its README documents a newer convertPreset
fs.mkdirSync(out, { recursive: true });
const tally = {};
const walk = (comps, acc) => { for (const c of comps || []) { acc.push(c.type); walk(c.components, acc); } return acc; };
for (const f of fs.readdirSync(dir).filter((f) => f.endsWith(".avs"))) {
  let j;
  try { j = convertFileSync(path.join(dir, f), { quiet: true }); if (typeof j === "string") j = JSON.parse(j); }
  catch (e) { console.log("FAIL", f, e.message); continue; }
  fs.writeFileSync(path.join(out, f.replace(/\.avs$/, ".json")), JSON.stringify(j, null, 1));
  const types = walk(j.components, []);
  for (const t of new Set(types)) tally[t] = (tally[t] || 0) + 1;
  console.log(f.padEnd(70), types.length, "components");
}
console.log(JSON.stringify(Object.entries(tally).sort((a, b) => b[1] - a[1])));
