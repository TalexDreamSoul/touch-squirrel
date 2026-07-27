export function validateConfig(cfg) {
  const t = Number(cfg.target);
  if (!Number.isFinite(t) || t < 1 || t > 100) {
    return 'target 需为 1–100 的整数';
  }
  return null;
}

export const defaults = {
  target: 5,
  auto: true,
  keepWindows: false,
};
