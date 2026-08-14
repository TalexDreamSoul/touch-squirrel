#!/usr/bin/env node

import { runCLI } from "../lib/launcher.mjs";

try {
  const code = await runCLI(process.argv.slice(2));
  process.exitCode = code;
} catch (error) {
  const message = error instanceof Error ? error.message : String(error);
  console.error(`[squirrel] ${message}`);
  process.exitCode = 1;
}
