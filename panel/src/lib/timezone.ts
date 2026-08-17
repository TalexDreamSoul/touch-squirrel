/**
 * 时区偏好：浏览器侧的显示时区选择，以及全站统一的时间格式化。
 *
 * 服务端始终以 UTC / RFC3339 存储，这里只负责「把时间戳渲染成给人看的字符串」。
 *
 * 面板是 `output: "export"` 静态导出，组件会在构建期被预渲染，因此
 * `localStorage` 与 `Intl` 只能在挂载之后访问：`useTimezone` 首帧统一返回
 * `SSR_TIMEZONE`，水合完成后再切到真实值，避免 hydration 不一致。
 */

import { useCallback, useMemo, useSyncExternalStore } from "react";

/** localStorage key；空值表示「跟随浏览器」。 */
export const TIMEZONE_STORAGE_KEY = "squirrel-timezone";

/** 偏好变更时派发的 window 事件名，用于同一标签页内的跨组件同步。 */
export const TIMEZONE_CHANGE_EVENT = "squirrel-timezone-change";

/** 服务端预渲染与水合首帧使用的兜底时区。 */
const SSR_TIMEZONE = "UTC";

const LOCALE = "zh-CN";

/** 常用时区选项，供下拉框使用。 */
export const COMMON_TIMEZONES: { value: string; label: string }[] = [
  { value: "Asia/Shanghai", label: "中国 · 上海（北京时间）" },
  { value: "Asia/Hong_Kong", label: "中国 · 香港" },
  { value: "Asia/Taipei", label: "中国 · 台北" },
  { value: "Asia/Tokyo", label: "日本 · 东京" },
  { value: "Asia/Seoul", label: "韩国 · 首尔" },
  { value: "Asia/Singapore", label: "新加坡" },
  { value: "Asia/Kolkata", label: "印度 · 加尔各答" },
  { value: "Asia/Dubai", label: "阿联酋 · 迪拜" },
  { value: "Europe/Moscow", label: "俄罗斯 · 莫斯科" },
  { value: "Europe/Berlin", label: "德国 · 柏林" },
  { value: "Europe/Paris", label: "法国 · 巴黎" },
  { value: "Europe/London", label: "英国 · 伦敦" },
  { value: "America/New_York", label: "美国 · 美东（纽约）" },
  { value: "America/Chicago", label: "美国 · 美中（芝加哥）" },
  { value: "America/Denver", label: "美国 · 山地（丹佛）" },
  { value: "America/Los_Angeles", label: "美国 · 美西（洛杉矶）" },
  { value: "America/Sao_Paulo", label: "巴西 · 圣保罗" },
  { value: "Australia/Sydney", label: "澳大利亚 · 悉尼" },
  { value: "UTC", label: "UTC 世界标准时间" },
];

type TimezoneSnapshot = {
  override: string;
  browser: string;
};

const formatterCache = new Map<string, Intl.DateTimeFormat>();
const validityCache = new Map<string, boolean>();
let detectedBrowserTimezone = "";

// useSyncExternalStore 要求快照引用稳定，所以缓存对象，偏好变更时置空重建。
let clientSnapshot: TimezoneSnapshot | null = null;

/** 时区名可能来自用户手填或旧数据，先校验再使用，避免 Intl 抛错拖垮整页。 */
function isValidTimezone(tz: string): boolean {
  if (!tz) return false;
  const cached = validityCache.get(tz);
  if (cached !== undefined) return cached;
  let valid = true;
  try {
    new Intl.DateTimeFormat(LOCALE, { timeZone: tz });
  } catch {
    valid = false;
  }
  validityCache.set(tz, valid);
  return valid;
}

function formatterFor(tz: string): Intl.DateTimeFormat {
  const cached = formatterCache.get(tz);
  if (cached) return cached;
  const formatter = new Intl.DateTimeFormat(LOCALE, {
    timeZone: tz,
    hour12: false,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
  formatterCache.set(tz, formatter);
  return formatter;
}

type DateTimeParts = {
  year: string;
  month: string;
  day: string;
  hour: string;
  minute: string;
  second: string;
};

/** 用 formatToParts 自己拼接，避免各 locale 的分隔符差异（zh-CN 默认输出 2026/08/16）。 */
function partsOf(date: Date, tz: string): DateTimeParts {
  const parts: DateTimeParts = {
    year: "",
    month: "",
    day: "",
    hour: "",
    minute: "",
    second: "",
  };
  for (const part of formatterFor(tz).formatToParts(date)) {
    switch (part.type) {
      case "year":
        parts.year = part.value;
        break;
      case "month":
        parts.month = part.value;
        break;
      case "day":
        parts.day = part.value;
        break;
      // 老引擎在 hour12:false 下可能给出 h24 的 "24"，统一成 "00"。
      case "hour":
        parts.hour = part.value === "24" ? "00" : part.value;
        break;
      case "minute":
        parts.minute = part.value;
        break;
      case "second":
        parts.second = part.value;
        break;
      default:
        break;
    }
  }
  return parts;
}

function toDate(value: string | number | Date | undefined): Date | null {
  if (value === undefined) return null;
  if (typeof value === "string") {
    const trimmed = value.trim();
    if (!trimmed) return null;
    const parsed = new Date(trimmed);
    return Number.isNaN(parsed.getTime()) ? null : parsed;
  }
  if (typeof value === "number") {
    if (!Number.isFinite(value)) return null;
    const parsed = new Date(value);
    return Number.isNaN(parsed.getTime()) ? null : parsed;
  }
  if (value instanceof Date) {
    return Number.isNaN(value.getTime()) ? null : value;
  }
  return null;
}

/** 无法解析时原样返回字符串，绝不显示 "Invalid Date"。 */
function fallbackText(value: string | number | Date | undefined): string {
  return typeof value === "string" ? value : "";
}

function resolveTimezone(tz?: string): string {
  if (tz === undefined) return effectiveTimezone();
  return isValidTimezone(tz) ? tz : browserTimezone();
}

/** 浏览器自身时区，如 "Asia/Shanghai"；服务端返回 UTC。 */
export function browserTimezone(): string {
  if (typeof window === "undefined") return SSR_TIMEZONE;
  if (detectedBrowserTimezone) return detectedBrowserTimezone;
  try {
    const resolved = Intl.DateTimeFormat().resolvedOptions().timeZone;
    detectedBrowserTimezone = resolved && isValidTimezone(resolved) ? resolved : SSR_TIMEZONE;
  } catch {
    detectedBrowserTimezone = SSR_TIMEZONE;
  }
  return detectedBrowserTimezone;
}

/** 用户手动指定的时区；"" 表示跟随浏览器。 */
export function getTimezoneOverride(): string {
  if (typeof window === "undefined") return "";
  try {
    return window.localStorage.getItem(TIMEZONE_STORAGE_KEY) || "";
  } catch {
    return "";
  }
}

/** 写入偏好并广播，让当前页与其他标签页的 useTimezone 一起刷新。 */
export function setTimezoneOverride(tz: string): void {
  if (typeof window === "undefined") return;
  const next = tz.trim();
  try {
    if (next) window.localStorage.setItem(TIMEZONE_STORAGE_KEY, next);
    else window.localStorage.removeItem(TIMEZONE_STORAGE_KEY);
  } catch {
    // 隐私模式下 localStorage 可能不可写，此时只让本次渲染跟上即可。
  }
  clientSnapshot = null;
  window.dispatchEvent(new Event(TIMEZONE_CHANGE_EVENT));
}

/** 当前生效的时区 = override || browserTimezone()。 */
export function effectiveTimezone(): string {
  const override = getTimezoneOverride();
  return override && isValidTimezone(override) ? override : browserTimezone();
}

const SERVER_SNAPSHOT: TimezoneSnapshot = { override: "", browser: SSR_TIMEZONE };

function getClientSnapshot(): TimezoneSnapshot {
  if (!clientSnapshot) {
    clientSnapshot = { override: getTimezoneOverride(), browser: browserTimezone() };
  }
  return clientSnapshot;
}

function getServerSnapshot(): TimezoneSnapshot {
  return SERVER_SNAPSHOT;
}

function subscribe(onStoreChange: () => void): () => void {
  if (typeof window === "undefined") return () => undefined;
  const invalidate = () => {
    clientSnapshot = null;
    onStoreChange();
  };
  const onStorage = (event: StorageEvent) => {
    // key 为 null 表示整个 storage 被清空。
    if (event.key !== null && event.key !== TIMEZONE_STORAGE_KEY) return;
    invalidate();
  };
  window.addEventListener(TIMEZONE_CHANGE_EVENT, invalidate);
  window.addEventListener("storage", onStorage);
  return () => {
    window.removeEventListener(TIMEZONE_CHANGE_EVENT, invalidate);
    window.removeEventListener("storage", onStorage);
  };
}

export type TimezonePreference = {
  /** 生效时区。 */
  timezone: string;
  /** 用户手动指定的时区；"" = 跟随浏览器。 */
  override: string;
  setOverride: (tz: string) => void;
  /** 浏览器检测到的时区。 */
  browser: string;
};

/** 返回生效时区，并在偏好变化时让所有使用方重新渲染。 */
export function useTimezone(): TimezonePreference {
  const snapshot = useSyncExternalStore(subscribe, getClientSnapshot, getServerSnapshot);
  const setOverride = useCallback((tz: string) => setTimezoneOverride(tz), []);
  return useMemo(() => {
    const { override, browser } = snapshot;
    return {
      timezone: override && isValidTimezone(override) ? override : browser,
      override,
      setOverride,
      browser,
    };
  }, [snapshot, setOverride]);
}

/** 该时区当前的 UTC 偏移，用于展示，如 "UTC+8" / "UTC+5:30"。 */
export function timezoneOffsetLabel(tz: string): string {
  const zone = isValidTimezone(tz) ? tz : browserTimezone();
  const now = new Date();
  const parts = partsOf(now, zone);
  const asUTC = Date.UTC(
    Number(parts.year),
    Number(parts.month) - 1,
    Number(parts.day),
    Number(parts.hour),
    Number(parts.minute),
    Number(parts.second),
  );
  if (!Number.isFinite(asUTC)) return "UTC+0";
  const base = now.getTime() - now.getMilliseconds();
  const minutes = Math.round((asUTC - base) / 60000);
  const sign = minutes < 0 ? "-" : "+";
  const absolute = Math.abs(minutes);
  const hours = Math.floor(absolute / 60);
  const rest = absolute % 60;
  return rest === 0
    ? `UTC${sign}${hours}`
    : `UTC${sign}${hours}:${String(rest).padStart(2, "0")}`;
}

/** 如 2026-08-16 20:31。 */
export function formatDateTime(value: string | number | Date | undefined, tz?: string): string {
  const date = toDate(value);
  if (!date) return fallbackText(value);
  const parts = partsOf(date, resolveTimezone(tz));
  return `${parts.year}-${parts.month}-${parts.day} ${parts.hour}:${parts.minute}`;
}

/** 如 2026-08-16。 */
export function formatDate(value: string | number | Date | undefined, tz?: string): string {
  const date = toDate(value);
  if (!date) return fallbackText(value);
  const parts = partsOf(date, resolveTimezone(tz));
  return `${parts.year}-${parts.month}-${parts.day}`;
}

/** 如 20:31。 */
export function formatTime(value: string | number | Date | undefined, tz?: string): string {
  const date = toDate(value);
  if (!date) return fallbackText(value);
  const parts = partsOf(date, resolveTimezone(tz));
  return `${parts.hour}:${parts.minute}`;
}
