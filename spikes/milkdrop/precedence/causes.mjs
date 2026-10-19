// Which operator families account for the presets the patch fixed.
// argv: <canon-report.json before> <canon-report.json after>
import fs from "node:fs";
const [r, b] = process.argv.slice(2).map((f) => JSON.parse(fs.readFileSync(f, "utf8")));
const key = (x) => x.replace(/\\/g, "/").split("/presets/").pop().replace(/^presets\//, "");
const fam = (s) => {
  const [m, e] = s.split(" | ");
  if (m === "&" || m === "|") return "bitwise";
  if (m === "&&" || m === "||") return "logical";
  if (e === "!") return "not";
  if (m === "num") return "num";
  if (e === "call:_aboeq") return "_aboeq";
  return "sign";
};
const sets = {};
for (const p of r.pairs) {
  const f = fam(p.split);
  sets[f] ??= new Set();
  p.valuePresetList.forEach((x) => sets[f].add(key(x)));
}
const after = new Set(b.valueDiffPresets.map(key));
const fixed = r.valueDiffPresets.map(key).filter((x) => !after.has(x));
console.log("fixed", fixed.length);
for (const f of Object.keys(sets)) {
  const inFixed = fixed.filter((x) => sets[f].has(x));
  const only = inFixed.filter((x) => Object.keys(sets).every((g) => g === f || !sets[g].has(x)));
  console.log(f.padEnd(8), "value-presets", sets[f].size, "in fixed", inFixed.length, "only this cause", only.length);
}
console.log("fixed, no statement-level cause:", fixed.filter((x) => !Object.values(sets).some((s) => s.has(x))));
