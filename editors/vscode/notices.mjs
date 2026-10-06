// Writes THIRD_PARTY_NOTICES.md: the licence of every package esbuild
// bundles into dist/extension.js (the production dependencies, not the
// tooling), as `task notices` does for the binary. `npm run package`
// runs it, so every .vsix carries the notices of the code inside it.
import { execFileSync } from "node:child_process";
import * as fs from "node:fs";
import * as path from "node:path";

const tree = JSON.parse(execFileSync("npm", ["ls", "--omit=dev", "--all", "--json", "--long"], { encoding: "utf8", shell: process.platform === "win32" }));
const found = new Map();
(function walk(deps) {
  for (const [name, dep] of Object.entries(deps ?? {})) {
    if (dep.path && !found.has(`${name}@${dep.version}`)) {
      found.set(`${name}@${dep.version}`, { name, version: dep.version, dir: dep.path });
    }
    walk(dep.dependencies);
  }
})(tree.dependencies);

const out = ["# Third-party notices", "", "The apic extension bundles these packages into `dist/extension.js`.", ""];
for (const key of [...found.keys()].sort()) {
  const { name, version, dir } = found.get(key);
  const pkg = JSON.parse(fs.readFileSync(path.join(dir, "package.json"), "utf8"));
  const file = fs.readdirSync(dir).find((f) => /^(licen[cs]e|copying)(\.|$)/i.test(f));
  if (!file) {
    console.error(`notices: ${name}@${version} has no licence file`);
    process.exit(1);
  }
  out.push(`## ${name} ${version}`, "", `Licence: ${pkg.license ?? "see below"}`, "", "```", fs.readFileSync(path.join(dir, file), "utf8").trim(), "```", "");
}
fs.writeFileSync("THIRD_PARTY_NOTICES.md", out.join("\n"));
console.log(`notices: ${found.size} packages`);
