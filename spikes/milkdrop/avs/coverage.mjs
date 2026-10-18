// SPIKE — which VISBOT components would webvs 3.0.0 render, by exact name?
import fs from "node:fs"; import path from "node:path";
const WEBVS = new Set(process.argv[3].split(","));
const walk = (comps, acc) => { for (const c of comps || []) { acc.push(c.type); walk(c.components, acc); } return acc; };
let full = 0;
for (const f of fs.readdirSync(process.argv[2]).sort()) {
  const j = JSON.parse(fs.readFileSync(path.join(process.argv[2], f), "utf8"));
  const types = walk(j.components, []).filter((t) => t !== "Comment");
  const missing = [...new Set(types.filter((t) => !WEBVS.has(t)))];
  if (!missing.length) full++;
  console.log(f.slice(0, 50).padEnd(50), `${types.length - types.filter((t) => !WEBVS.has(t)).length}/${types.length} rendered`, missing.length ? "missing: " + missing.join(", ") : "");
}
console.log("presets fully covered:", full);
