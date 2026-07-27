/**
 * xai-accounts JS shell (schema / UI metadata only).
 * Runtime work stays in the Go body for now.
 */
export const id = "xai-accounts";
export const kind = ["registrar"];

export function validateConfig(cfg = {}) {
  const target = Number(cfg.target ?? 10);
  if (!Number.isInteger(target) || target < 1 || target > 10000) {
    return { ok: false, error: "target must be integer 1..10000" };
  }
  return { ok: true, config: { ...cfg, target } };
}

export default { id, kind, validateConfig };
