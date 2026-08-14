import { createHash } from "node:crypto";
import { createReadStream, createWriteStream, existsSync } from "node:fs";
import {
  chmod,
  mkdir,
  readFile,
  rename,
  rm,
  writeFile,
} from "node:fs/promises";
import { homedir } from "node:os";
import path from "node:path";
import { spawn } from "node:child_process";
import { Readable } from "node:stream";
import { pipeline } from "node:stream/promises";

const REPOSITORY = "TalexDreamSoul/touch-squirrel";
const OFFICIAL_REPOSITORY = `https://github.com/${REPOSITORY}`;
const RELEASE_HOSTS = new Set([
  "github.com",
  "objects.githubusercontent.com",
  "release-assets.githubusercontent.com",
]);

const TARGETS = new Map([
  ["darwin:arm64", "squirrel-darwin-arm64"],
  ["darwin:x64", "squirrel-darwin-amd64"],
  ["linux:arm64", "squirrel-linux-arm64"],
  ["linux:x64", "squirrel-linux-amd64"],
  ["win32:x64", "squirrel-windows-amd64.exe"],
]);

export function releaseAssetName(platform = process.platform, arch = process.arch) {
  const name = TARGETS.get(`${platform}:${arch}`);
  if (!name) {
    throw new Error(
      `unsupported platform: ${platform}/${arch}; supported targets: ${[...TARGETS.keys()].join(", ")}`,
    );
  }
  return name;
}

export function parseChecksums(text) {
  const checksums = new Map();
  for (const line of text.split(/\r?\n/)) {
    const match = line.trim().match(/^([a-fA-F0-9]{64})\s+\*?(.+)$/);
    if (match) checksums.set(match[2], match[1].toLowerCase());
  }
  return checksums;
}

export function commandArgs(args) {
  return args.length === 0 ? ["web"] : [...args];
}

export function defaultCacheRoot(platform = process.platform, env = process.env) {
  if (env.SQUIRREL_CACHE_DIR) return path.resolve(env.SQUIRREL_CACHE_DIR);
  if (platform === "darwin") return path.join(homedir(), "Library", "Caches", "touch-squirrel");
  if (platform === "win32") {
    return path.join(env.LOCALAPPDATA || path.join(homedir(), "AppData", "Local"), "touch-squirrel");
  }
  return path.join(env.XDG_CACHE_HOME || path.join(homedir(), ".cache"), "touch-squirrel");
}

export async function packageVersion() {
  const raw = await readFile(new URL("../package.json", import.meta.url), "utf8");
  return JSON.parse(raw).version;
}

function releaseURL(version, filename) {
  return `https://github.com/${REPOSITORY}/releases/download/v${version}/${filename}`;
}

async function fetchRelease(url, label) {
  const response = await fetch(url, {
    redirect: "follow",
    headers: { "user-agent": "@talex-touch/squirrel" },
    signal: AbortSignal.timeout(120_000),
  });
  if (!response.ok || !response.body) {
    throw new Error(`${label} download failed: ${response.status} ${response.statusText}`);
  }
  const finalURL = new URL(response.url);
  if (finalURL.protocol !== "https:" || !RELEASE_HOSTS.has(finalURL.hostname)) {
    throw new Error(`${label} redirected to an untrusted host: ${finalURL.hostname}`);
  }
  return response;
}

async function downloadText(url, label) {
  const response = await fetchRelease(url, label);
  return response.text();
}

async function downloadFile(url, destination, label) {
  const response = await fetchRelease(url, label);
  await pipeline(Readable.fromWeb(response.body), createWriteStream(destination, { mode: 0o600 }));
}

async function sha256(filename) {
  const hash = createHash("sha256");
  for await (const chunk of createReadStream(filename)) hash.update(chunk);
  return hash.digest("hex");
}

async function cachedBinaryIsValid(binary, checksumFile, asset) {
  if (!existsSync(binary) || !existsSync(checksumFile)) return false;
  const checksums = parseChecksums(await readFile(checksumFile, "utf8"));
  const expected = checksums.get(asset);
  if (!expected) return false;
  return (await sha256(binary)) === expected;
}

export async function ensureBinary(options = {}) {
  const env = options.env || process.env;
  if (env.SQUIRREL_BINARY) {
    const override = path.resolve(env.SQUIRREL_BINARY);
    if (!existsSync(override)) throw new Error(`SQUIRREL_BINARY does not exist: ${override}`);
    return override;
  }

  const version = options.version || (await packageVersion());
  const asset = releaseAssetName(options.platform, options.arch);
  const versionDir = path.join(options.cacheRoot || defaultCacheRoot(options.platform, env), version);
  const binary = path.join(versionDir, asset);
  const checksumFile = path.join(versionDir, "checksums.txt");
  await mkdir(versionDir, { recursive: true, mode: 0o700 });

  if (await cachedBinaryIsValid(binary, checksumFile, asset)) return binary;

  console.error(`[squirrel] downloading v${version} for ${options.platform || process.platform}/${options.arch || process.arch}`);
  const checksumsText = await downloadText(releaseURL(version, "checksums.txt"), "checksums");
  const expected = parseChecksums(checksumsText).get(asset);
  if (!expected) throw new Error(`release v${version} does not contain a checksum for ${asset}`);

  const temporary = `${binary}.tmp-${process.pid}-${Date.now()}`;
  try {
    await downloadFile(releaseURL(version, asset), temporary, asset);
    const actual = await sha256(temporary);
    if (actual !== expected) {
      throw new Error(`checksum mismatch for ${asset}: expected ${expected}, received ${actual}`);
    }
    if (process.platform !== "win32") await chmod(temporary, 0o755);
    await rm(binary, { force: true });
    await rename(temporary, binary);
    await writeFile(checksumFile, checksumsText, { mode: 0o600 });
  } finally {
    await rm(temporary, { force: true });
  }
  return binary;
}

export async function runCLI(args, options = {}) {
  const env = options.env || process.env;
  const effectiveArgs = commandArgs(args);
  const version = options.version || (await packageVersion());
  const binary = await ensureBinary({ ...options, version, env });
  if (effectiveArgs[0] === "doctor") {
    console.error(`[squirrel] launcher @talex-touch/squirrel v${version}`);
    console.error(`[squirrel] platform ${options.platform || process.platform}/${options.arch || process.arch}`);
    console.error(`[squirrel] binary ${binary}`);
  }
  const child = spawn(binary, effectiveArgs, {
    stdio: "inherit",
    env: {
      ...env,
      SQUIRREL_DISABLE_IN_TREE_PLUGINS: "1",
      SQUIRREL_OFFICIAL_PLUGIN_REPO: env.SQUIRREL_OFFICIAL_PLUGIN_REPO || OFFICIAL_REPOSITORY,
      SQUIRREL_LAUNCHED_BY: "@talex-touch/squirrel",
    },
  });

  const signals = ["SIGINT", "SIGTERM"];
  const handlers = new Map();
  for (const signal of signals) {
    const handler = () => child.kill(signal);
    handlers.set(signal, handler);
    process.once(signal, handler);
  }

  return new Promise((resolve, reject) => {
    child.once("error", reject);
    child.once("exit", (code, signal) => {
      for (const [name, handler] of handlers) process.removeListener(name, handler);
      if (signal) {
        reject(new Error(`squirrel terminated by ${signal}`));
        return;
      }
      resolve(code ?? 1);
    });
  });
}
