// SPIKE — does eel.mjs parse the expression code in the VISBOT presets?
import fs from "node:fs"; import path from "node:path"; import { parse } from "../eel.mjs";
const blocks = [];
const walk = (comps, file) => { for (const c of comps || []) {
  if (c.code && typeof c.code === "object") for (const [k, v] of Object.entries(c.code)) if (typeof v === "string" && v.trim()) blocks.push({ file, type: c.type, k, v });
  walk(c.components, file);
} };
for (const f of fs.readdirSync(process.argv[2])) walk(JSON.parse(fs.readFileSync(path.join(process.argv[2], f), "utf8")).components, f);
const fails = {}; let ok = 0;
for (const b of blocks) {
  try { parse(b.v); ok++; }
  catch (e) { const k = e.message.replace(/\d+/g, "N"); (fails[k] ??= []).push(`${b.type}.${b.k}: ${JSON.stringify(b.v.slice(Math.max(0, (e.pos ?? 0) - 25), (e.pos ?? 0) + 25))}`); }
}
console.log(`code blocks: ${blocks.length}, parsed: ${ok}`);
for (const [k, v] of Object.entries(fails)) console.log(`${v.length}x ${k}\n   e.g. ${v[0]}`);
console.log("types carrying code:", [...new Set(blocks.map((b) => b.type))].join(", "));
