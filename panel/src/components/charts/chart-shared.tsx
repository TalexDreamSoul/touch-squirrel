"use client";

import { ChartPalette, Loader, Text } from "@cloudflare/kumo";

/**
 * 系列/条目的语义色调。primary 走强调色，其余映射到 ChartPalette.semantic。
 * 后端 `/api/analytics/overview` 的 `tone` 字段用的是同一套词表。
 */
export type ChartTone =
  | "primary"
  | "success"
  | "warning"
  | "critical"
  | "neutral";

/**
 * 按色调取色：
 * - success / warning / critical / neutral → 语义色（状态含义）
 * - primary → 强调色（categorical 第 0 位）
 * - 未指定 → 按系列序号取 categorical 色（身份含义，顺序固定不轮换）
 */
export function toneColor(
  tone: ChartTone | undefined,
  index: number,
  isDarkMode: boolean,
): string {
  switch (tone) {
    case "success":
      return ChartPalette.semantic("Success", isDarkMode);
    case "warning":
      return ChartPalette.semantic("Warning", isDarkMode);
    case "critical":
      return ChartPalette.semantic("Attention", isDarkMode);
    case "neutral":
      return ChartPalette.semantic("Neutral", isDarkMode);
    case "primary":
      return ChartPalette.categorical(0, isDarkMode);
    default:
      return ChartPalette.categorical(index, isDarkMode);
  }
}

/** 将 #RRGGBB 转为带透明度的 rgba()；非法输入原样返回。 */
export function withAlpha(hexColor: string, alpha: number): string {
  const raw = hexColor.replace("#", "");
  const hex =
    raw.length === 3
      ? raw
          .split("")
          .map((ch) => ch + ch)
          .join("")
      : raw;
  if (!/^[0-9a-fA-F]{6}$/.test(hex)) return hexColor;
  const num = Number.parseInt(hex, 16);
  const r = (num >> 16) & 255;
  const g = (num >> 8) & 255;
  const b = num & 255;
  return `rgba(${r}, ${g}, ${b}, ${alpha})`;
}

/** 千分位数字格式化（面向中文界面）。 */
export function formatNumber(value: number): string {
  return value.toLocaleString("zh-CN");
}

/** 图例条目：色点 + 名称。文字始终用文本色，颜色只出现在色点上。 */
export function ChartLegendItem({ name, color }: { name: string; color: string }) {
  return (
    <span className="inline-flex min-w-0 items-center gap-1.5">
      <span
        aria-hidden
        className="h-2 w-2 shrink-0 rounded-full"
        style={{ backgroundColor: color }}
      />
      <Text size="xs" variant="secondary" as="span" truncate>
        {name}
      </Text>
    </span>
  );
}

/** 横向图例行；少于 2 个条目时不渲染（单系列由标题承担说明）。 */
export function ChartLegendRow({
  items,
}: {
  items: { name: string; color: string }[];
}) {
  if (items.length < 2) return null;
  return (
    <div className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1">
      {items.map((item) => (
        <ChartLegendItem key={item.name} name={item.name} color={item.color} />
      ))}
    </div>
  );
}

/** 与图表同高的加载占位。 */
export function ChartLoadingBox({ height }: { height: number }) {
  return (
    <div
      className="flex items-center justify-center"
      style={{ height }}
    >
      <Loader aria-label="加载中" />
    </div>
  );
}
