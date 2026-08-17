import { api } from "@/lib/api";

/** One cell of a contribution-style calendar. */
export type CalendarDay = {
  date: string;
  value: number;
  detail?: Record<string, number>;
};

/** One heatmap series plus the headline numbers shown beside it. */
export type Calendar = {
  key: string;
  label: string;
  description: string;
  unit: string;
  total: number;
  max: number;
  active_days: number;
  streak: number;
  days: CalendarDay[];
  /** History only starts when the day ledger did — empty cells before `since` mean "not recorded", not "quiet". */
  partial?: boolean;
  since?: string;
};

export type DistributionItem = {
  name: string;
  value: number;
  /** Matches the chart library's `ChartTone`, so items pass straight to the charts. */
  tone?: "success" | "warning" | "critical" | "neutral";
};

export type Distribution = {
  key: string;
  label: string;
  total: number;
  items: DistributionItem[];
};

export type StageAggregate = {
  name: string;
  count: number;
  avg_ms: number;
  p50_ms: number;
  p95_ms: number;
  max_ms: number;
};

export type RunPoint = {
  run_id: string;
  plugin: string;
  time: string;
  run_duration_ms: number;
  average_account_ms: number;
  p95_account_ms: number;
  account_count: number;
  success_rate: number;
};

/** Which zone decided where each calendar day starts. */
export type TimezoneInfo = {
  name: string;
  /** request = this browser asked for it; config = server default; server = server's own zone. */
  source: "request" | "config" | "server";
  offset_minutes: number;
};

export type AnalyticsOverview = {
  ok: boolean;
  range: { days: number; from: string; to: string };
  timezone: TimezoneInfo;
  calendars: Calendar[];
  distributions: Distribution[];
  stages: StageAggregate[];
  points: RunPoint[];
  register_runs: number;
  ledger: { since: string; totals: Record<string, number> };
};

/**
 * Day boundaries are applied server-side in `timezone`, so the caller has to
 * pass the zone it wants rather than re-bucketing the response.
 */
export function fetchAnalytics(days: number, timezone?: string) {
  const params = new URLSearchParams({ days: String(days) });
  if (timezone) params.set("tz", timezone);
  return api<AnalyticsOverview>(`/api/analytics/overview?${params.toString()}`);
}

/** Chinese labels for the `detail` keys each calendar carries. */
export const CALENDAR_DETAIL_LABELS: Record<string, Record<string, string>> = {
  register: { success: "成功", failed: "失败", incomplete: "未完成" },
  patrol: {
    checked: "覆盖账号",
    healthy: "健康",
    rate_limited: "限流",
    dead: "不可用",
    errors: "巡检失败",
  },
  degrade: {
    checked: "检测账号",
    normal: "正常",
    degraded: "疑似降智",
    exhausted: "额度耗尽",
    errored: "检测错误",
  },
  cleanup: { scanned: "扫描", quota_hits: "命中耗尽", deleted: "已删除" },
};

export const RANGE_OPTIONS = [
  { value: "30", label: "近 30 天" },
  { value: "90", label: "近 90 天" },
  { value: "180", label: "近 180 天" },
  { value: "365", label: "近一年" },
];

export function formatDuration(ms?: number): string {
  if (!ms || ms < 1) return "—";
  if (ms < 1000) return `${Math.round(ms)} 毫秒`;
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)} 秒`;
  return `${(ms / 60_000).toFixed(1)} 分钟`;
}

export function formatPercent(ratio?: number, digits = 1): string {
  if (typeof ratio !== "number" || !Number.isFinite(ratio)) return "—";
  return `${(ratio * 100).toFixed(digits)}%`;
}

export function formatCount(value?: number): string {
  if (typeof value !== "number" || !Number.isFinite(value)) return "—";
  return value.toLocaleString("zh-CN");
}

/** Formats a YYYY-MM-DD key for display, e.g. "8月16日". */
export function formatDay(date: string): string {
  const parts = date.split("-");
  if (parts.length !== 3) return date;
  return `${Number(parts[1])}月${Number(parts[2])}日`;
}
