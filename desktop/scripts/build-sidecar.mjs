// 把 relay/ 打成单文件 homingd，并作为 Tauri 的资源一起分发。
//
// 为什么用 Node SEA 而不是 bun build --compile：
//   relay/scanners/lib/opencode-db.mjs 用了 node:sqlite，bun 编译出来的二进制里没有这个内建模块，
//   实测 `bun build --compile` 会在运行时报 "No such built-in module: node:sqlite"。
//   Node SEA 用的是 node 自己的运行时，node:sqlite 天然可用。
//
// 用法：
//   node scripts/build-sidecar.mjs                # 当前架构（本机）
//   node scripts/build-sidecar.mjs --arch x64     # 交叉产出 macOS x64
//   node scripts/build-sidecar.mjs --arch win     # 交叉产出 Windows x64
//   node scripts/build-sidecar.mjs --skip-console # 不重建控制台页面

import fs from "node:fs";
import path from "node:path";
import os from "node:os";
import { execFileSync } from "node:child_process";

const here = import.meta.dirname;
const desktopDir = path.resolve(here, "..");
const repoDir = path.resolve(desktopDir, "..");
const relayDir = path.join(repoDir, "relay");
const consoleDir = path.join(repoDir, "console");
const consoleDist = path.join(consoleDir, "dist");
const buildDir = path.join(desktopDir, "build");
const binariesDir = path.join(desktopDir, "src-tauri", "binaries");
const cacheDir = path.join(buildDir, "node-cache");

const args = process.argv.slice(2);
const archArg = (() => {
  const idx = args.indexOf("--arch");
  if (idx >= 0) return args[idx + 1];
  if (args.includes("--win")) return "win";
  return process.arch === "arm64" ? "arm64" : "x64";
})();
const skipConsole = args.includes("--skip-console");
const NODE_VERSION = process.version.replace(/^v/, "");

const log = (msg) => console.log(`[sidecar] ${msg}`);

function run(cmd, commandArgs, options = {}) {
  // Windows 上 npm / npx 是 .cmd，必须经过 shell
  const needsShell = process.platform === "win32" && /^(npm|npx)$/.test(cmd);
  execFileSync(cmd, commandArgs, { stdio: "inherit", shell: needsShell, ...options });
}

function ensureDir(dir) {
  fs.mkdirSync(dir, { recursive: true });
}

// ---------------------------------------------------------------- 1. 控制台页面

if (!skipConsole) {
  const indexFile = path.join(consoleDist, "index.html");
  const sourceNewer = (() => {
    if (!fs.existsSync(indexFile)) return true;
    const builtAt = fs.statSync(indexFile).mtimeMs;
    const walk = (dir) => {
      for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
        if (entry.name === "dist" || entry.name === "node_modules") continue;
        const full = path.join(dir, entry.name);
        if (entry.isDirectory()) {
          if (walk(full)) return true;
        } else if (fs.statSync(full).mtimeMs > builtAt) {
          return true;
        }
      }
      return false;
    };
    return walk(consoleDir);
  })();

  if (sourceNewer) {
    log("控制台页面有改动，重新构建 console/dist");
    run("npm", ["run", "build"], { cwd: consoleDir });
  } else {
    log("console/dist 是最新的，跳过构建");
  }
}

// ---------------------------------------------------------------- 2. 打包成单个 CJS

ensureDir(buildDir);
const entryOut = path.join(buildDir, "homingd.cjs");
log("用 esbuild 打包 relay/server.mjs");
{
  const esbuild = await import("esbuild");
  await esbuild.build({
    entryPoints: [path.join(relayDir, "server.mjs")],
    bundle: true,
    platform: "node",
    format: "cjs",
    target: "node22",
    outfile: entryOut,
    // 打包后 relayDir 走「可执行文件所在目录」，数据目录走平台层
    define: { "process.env.HOMING_PACKAGED": '"1"' },
    external: ["node:sqlite", "node:sea"],
    legalComments: "none",
    logLevel: "warning",
    banner: {
      js: "// Homing homingd — bundled by esbuild, packaged by Node SEA\n",
    },
  });
}
log(`已生成 ${path.relative(repoDir, entryOut)}（${(fs.statSync(entryOut).size / 1024).toFixed(0)} KB）`);

// ---------------------------------------------------------------- 3. SEA 配置（把控制台页面塞进 assets）

const assets = {};
if (fs.existsSync(consoleDist)) {
  const walk = (dir, prefix) => {
    for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
      const full = path.join(dir, entry.name);
      if (entry.isDirectory()) walk(full, `${prefix}${entry.name}/`);
      else assets[`console/${prefix}${entry.name}`] = full;
    }
  };
  walk(consoleDist, "");
}
log(`内嵌控制台文件 ${Object.keys(assets).length} 个`);

const seaConfig = {
  main: entryOut,
  output: path.join(buildDir, "homingd.blob"),
  disableExperimentalSEAWarning: true,
  useSnapshot: false,
  useCodeCache: false,
  assets,
};
const seaConfigPath = path.join(buildDir, "sea-config.json");
fs.writeFileSync(seaConfigPath, JSON.stringify(seaConfig, null, 2));
run(process.execPath, ["--experimental-sea-config", seaConfigPath]);
log("已生成 SEA blob");

// ---------------------------------------------------------------- 4. 取得对应平台的 node 运行时

function nodeBinaryFor(arch) {
  if (arch === "arm64" && process.arch === "arm64") return process.execPath;
  if (arch === "x64" && process.arch === "x64") return process.execPath;

  const platform = arch === "win" ? "win-x64" : `darwin-${arch}`;
  const archive = arch === "win" ? `node-v${NODE_VERSION}-${platform}.zip` : `node-v${NODE_VERSION}-${platform}.tar.gz`;
  const target = path.join(cacheDir, archive);
  ensureDir(cacheDir);
  if (!fs.existsSync(target)) {
    const url = `https://registry.npmmirror.com/-/binary/node/v${NODE_VERSION}/${archive}`;
    log(`下载 ${archive}`);
    run("curl", ["-L", "--fail", "--noproxy", "registry.npmmirror.com", "-o", target, url]);
  }
  const extractDir = path.join(cacheDir, `node-v${NODE_VERSION}-${platform}`);
  if (!fs.existsSync(extractDir)) {
    log(`解压 ${archive}`);
    if (arch === "win") run("unzip", ["-q", "-o", target, "-d", cacheDir]);
    else run("tar", ["-xzf", target, "-C", cacheDir]);
  }
  // Windows 的 zip 把 node.exe 放在解压目录根上，macOS 的 tar 才在 bin/ 下
  return arch === "win" ? path.join(extractDir, "node.exe") : path.join(extractDir, "bin", "node");
}

const outputName = archArg === "win" || process.platform === "win32" ? "homingd.exe" : "homingd";
ensureDir(binariesDir);
const outputPath = path.join(binariesDir, outputName);

const sourceNode = nodeBinaryFor(archArg);
log(`运行时：${sourceNode}`);
fs.copyFileSync(sourceNode, outputPath);
fs.chmodSync(outputPath, 0o755);

// ---------------------------------------------------------------- 5. 注入 blob

// postject 的 bin 在不同平台是 .bin 软链或 .cmd，直接跑它的 cli.js 最稳
const postjectCli = (() => {
  const pkgFile = path.join(desktopDir, "node_modules", "postject", "package.json");
  const pkg = JSON.parse(fs.readFileSync(pkgFile, "utf8"));
  const bin = typeof pkg.bin === "string" ? pkg.bin : pkg.bin?.postject || "dist/cli.js";
  return path.join(desktopDir, "node_modules", "postject", bin);
})();
const FUSE = "NODE_SEA_FUSE_fce680ab2cc467b6e072b8b5df1996b2";

if (process.platform === "darwin" && archArg !== "win") {
  try {
    run("codesign", ["--remove-signature", outputPath]);
  } catch {
    log("没有可移除的签名，继续");
  }
}

run(process.execPath, [
  postjectCli,
  outputPath,
  "NODE_SEA_BLOB",
  seaConfig.output,
  "--sentinel-fuse",
  FUSE,
  ...(process.platform === "darwin" && archArg !== "win" ? ["--macho-segment-name", "NODE_SEA"] : []),
]);

if (process.platform === "darwin" && archArg !== "win") {
  run("codesign", ["--sign", "-", "--force", outputPath]);
}

log(`已生成 ${path.relative(repoDir, outputPath)}（${(fs.statSync(outputPath).size / 1024 / 1024).toFixed(1)} MB）`);

// ---------------------------------------------------------------- 6. 冒烟测试（只测本机架构）

const hostArch = process.arch === "arm64" ? "arm64" : "x64";
if (archArg === hostArch && archArg !== "win") {
  const version = execFileSync(outputPath, ["--version"], {
    encoding: "utf8",
    env: { ...process.env, HOMING_DATA_DIR: path.join(buildDir, "smoke-data"), PORT: "18899" },
    timeout: 10_000,
  }).trim();
  log(`冒烟测试：${version}`);
}

console.log(`
下一步：
  npx tauri build --target aarch64-apple-darwin   # Apple 芯片 dmg
  npx tauri build --target x86_64-apple-darwin    # Intel dmg（需先 rustup target add x86_64-apple-darwin）
`);
