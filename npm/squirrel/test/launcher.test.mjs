import assert from "node:assert/strict";
import { mkdtemp, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";

import {
  commandArgs,
  defaultCacheRoot,
  ensureBinary,
  packageVersion,
  parseChecksums,
  releaseAssetName,
} from "../lib/launcher.mjs";

test("defaults to the web manager", () => {
  assert.deepEqual(commandArgs([]), ["web"]);
  assert.deepEqual(commandArgs(["doctor"]), ["doctor"]);
});

test("reads the packaged launcher version", async () => {
  assert.equal(await packageVersion(), "0.2.0");
});

test("maps supported release assets", () => {
  assert.equal(releaseAssetName("darwin", "arm64"), "squirrel-darwin-arm64");
  assert.equal(releaseAssetName("linux", "x64"), "squirrel-linux-amd64");
  assert.equal(releaseAssetName("win32", "x64"), "squirrel-windows-amd64.exe");
  assert.throws(() => releaseAssetName("freebsd", "x64"), /unsupported platform/);
});

test("parses checksum files", () => {
  const digest = "a".repeat(64);
  const parsed = parseChecksums(`${digest}  squirrel-linux-amd64\ninvalid\n`);
  assert.equal(parsed.get("squirrel-linux-amd64"), digest);
});

test("honors an explicit binary override", async () => {
  const root = await mkdtemp(path.join(os.tmpdir(), "squirrel-launcher-"));
  const binary = path.join(root, "squirrel");
  await writeFile(binary, "test");
  assert.equal(await ensureBinary({ env: { SQUIRREL_BINARY: binary } }), binary);
});

test("honors an explicit cache directory", () => {
  assert.equal(
    defaultCacheRoot("linux", { SQUIRREL_CACHE_DIR: "./cache" }),
    path.resolve("./cache"),
  );
});
