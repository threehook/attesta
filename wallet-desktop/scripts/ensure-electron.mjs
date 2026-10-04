// Electron's own install script can finish without unpacking the binary on some Node versions (it exits silently after extracting a single file),
// leaving "Electron failed to install correctly". This checks for that and unpacks the downloaded archive with the system tools instead.
import { execFileSync } from "node:child_process";
import { existsSync, mkdirSync, rmSync, writeFileSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, join } from "node:path";

const require = createRequire(import.meta.url);
const electronDir = dirname(require.resolve("electron/package.json"));
const platformPath =
  process.platform === "darwin" ? "Electron.app/Contents/MacOS/Electron" : process.platform === "win32" ? "electron.exe" : "electron";

if (existsSync(join(electronDir, "path.txt")) && existsSync(join(electronDir, "dist", platformPath))) {
  process.exit(0);
}

const { version } = require("electron/package.json");
const { downloadArtifact } = createRequire(join(electronDir, "package.json"))("@electron/get");
const zip = await downloadArtifact({ version, artifactName: "electron", platform: process.platform, arch: process.arch });

const dist = join(electronDir, "dist");
rmSync(dist, { recursive: true, force: true });
mkdirSync(dist);
if (process.platform === "darwin") execFileSync("ditto", ["-x", "-k", zip, dist]);
else if (process.platform === "win32") execFileSync("tar", ["-xf", zip, "-C", dist]);
else execFileSync("unzip", ["-q", zip, "-d", dist]);

writeFileSync(join(electronDir, "path.txt"), platformPath);
console.log(`unpacked Electron ${version} into ${dist}`);
