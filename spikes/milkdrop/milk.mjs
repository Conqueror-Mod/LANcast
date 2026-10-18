// SPIKE — throwaway. Reads a .milk file into its parts: base values, the EEL
// blocks (in line order), and the two HLSL shaders.

export function readMilk(text) {
  const base = {};
  const blocks = {}; // key -> [ [n, line], ... ]
  const add = (key, n, line) => { (blocks[key] ??= []).push([n, line]); };
  for (const raw of text.split(/\n/)) {
    const line = raw.replace(/\r$/, "");
    const eq = line.indexOf("=");
    if (eq < 0) continue;
    const key = line.slice(0, eq).trim();
    const val = line.slice(eq + 1);
    const lk = key.toLowerCase();
    let m;
    if ((m = /^per_frame_init_(\d+)$/.exec(lk))) add("init", +m[1], val);
    else if ((m = /^per_frame_(\d+)$/.exec(lk))) add("frame", +m[1], val);
    else if ((m = /^per_pixel_(\d+)$/.exec(lk))) add("pixel", +m[1], val);
    else if ((m = /^wave_(\d)_init(\d+)$/.exec(lk))) add(`wave${m[1]}.init`, +m[2], val);
    else if ((m = /^wave_(\d)_per_frame(\d+)$/.exec(lk))) add(`wave${m[1]}.frame`, +m[2], val);
    else if ((m = /^wave_(\d)_per_point(\d+)$/.exec(lk))) add(`wave${m[1]}.point`, +m[2], val);
    else if ((m = /^shape_(\d)_init(\d+)$/.exec(lk))) add(`shape${m[1]}.init`, +m[2], val);
    else if ((m = /^shape_(\d)_per_frame(\d+)$/.exec(lk))) add(`shape${m[1]}.frame`, +m[2], val);
    else if ((m = /^warp_(\d+)$/.exec(lk))) add("warp", +m[1], val.replace(/^`/, ""));
    else if ((m = /^comp_(\d+)$/.exec(lk))) add("comp", +m[1], val.replace(/^`/, ""));
    else base[lk] = val.trim();
  }
  // MilkDrop wraps a long equation across numbered lines and rejoins them with
  // nothing between: "k1 = is_" + "beat*..." is is_beat. A // comment ends at
  // its own line, so comments are stripped per line before the join.
  const lines = (k) => (blocks[k] ?? []).sort((x, y) => x[0] - y[0]).map((x) => x[1]);
  const join = (k) => lines(k).map((l) => l.replace(/\/\/.*$/, "")).join("");
  const joinShader = (k) => lines(k).join("\n");
  const waves = [0, 1, 2, 3].map((i) => ({
    enabled: num(base[`wavecode_${i}_enabled`]) !== 0 && base[`wavecode_${i}_enabled`] !== undefined,
    samples: num(base[`wavecode_${i}_samples`]) || 512,
    init: join(`wave${i}.init`), frame: join(`wave${i}.frame`), point: join(`wave${i}.point`),
  }));
  const shapes = [0, 1, 2, 3].map((i) => ({
    enabled: num(base[`shapecode_${i}_enabled`]) !== 0 && base[`shapecode_${i}_enabled`] !== undefined,
    instances: Math.max(1, num(base[`shapecode_${i}_num_inst`]) || 1),
    init: join(`shape${i}.init`), frame: join(`shape${i}.frame`),
  }));
  return {
    base,
    version: num(base["milkdrop_preset_version"]) || 100,
    psWarp: num(base["psversion_warp"] ?? base["psversion"]) || 0,
    psComp: num(base["psversion_comp"] ?? base["psversion"]) || 0,
    init: join("init"), frame: join("frame"), pixel: join("pixel"),
    waves, shapes,
    warp: joinShader("warp"), comp: joinShader("comp"),
  };
}

function num(s) {
  const v = parseFloat(s);
  return Number.isFinite(v) ? v : 0;
}
