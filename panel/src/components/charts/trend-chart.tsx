"use client";

import { useCallback, useMemo } from "react";
import { Empty, Text, TimeseriesChart } from "@cloudflare/kumo";
import { useTheme } from "@/lib/theme";
import { ChartLegendRow, toneColor, type ChartTone } from "./chart-shared";
import { echarts } from "./echarts-setup";

export type TrendSeries = {
  /** 系列名称，用于图例与悬浮提示 */
  name: string;
  /** [毫秒时间戳, 数值] 元组，按时间升序 */
  points: [number, number][];
  /** 语义色调；不传时按系列序号取 categorical 色 */
  tone?: ChartTone;
};

export type TrendChartProps = {
  series: TrendSeries[];
  type?: "line" | "bar";
  height?: number;
  /** 数值格式化，同时作用于 Y 轴刻度与悬浮提示 */
  valueFormat?: (value: number) => string;
  loading?: boolean;
};

const pad2 = (value: number) => String(value).padStart(2, "0");

/**
 * 趋势图：包装 kumo TimeseriesChart，自动配色、自动深浅色、
 * 中文日期刻度（整日数据用「M月D日」，含时分用「M/D HH:mm」）。
 */
export function TrendChart({
  series,
  type = "line",
  height = 260,
  valueFormat,
  loading = false,
}: TrendChartProps) {
  const { resolved } = useTheme();
  const isDarkMode = resolved === "dark";

  const data = useMemo(
    () =>
      series.map((item, index) => ({
        name: item.name,
        data: item.points,
        color: toneColor(item.tone, index, isDarkMode),
      })),
    [series, isDarkMode],
  );

  const dayAligned = useMemo(
    () =>
      series.every((item) =>
        item.points.every(([timestamp]) => {
          const date = new Date(timestamp);
          return (
            date.getHours() === 0 &&
            date.getMinutes() === 0 &&
            date.getSeconds() === 0
          );
        }),
      ),
    [series],
  );

  const formatTick = useCallback(
    (timestamp: number) => {
      const date = new Date(timestamp);
      if (dayAligned) return `${date.getMonth() + 1}月${date.getDate()}日`;
      return `${date.getMonth() + 1}/${date.getDate()} ${pad2(date.getHours())}:${pad2(date.getMinutes())}`;
    },
    [dayAligned],
  );

  const maxPoints = series.reduce(
    (most, item) => Math.max(most, item.points.length),
    0,
  );

  if (!loading && maxPoints === 0) {
    return (
      <Empty size="sm" title="暂无数据" description="还没有可绘制的趋势数据" />
    );
  }

  // A single sample has no trend to draw: a bar would stretch across the whole
  // plot and a line would render as an invisible zero-length segment. Show the
  // value itself instead of a chart that looks broken.
  if (!loading && maxPoints < 2) {
    return (
      <div style={{ minHeight: height }} className="flex flex-col justify-center gap-2">
        {series.map((item, index) => {
          const [timestamp, value] = item.points[0] ?? [];
          if (timestamp === undefined) return null;
          return (
            <div key={item.name} className="flex items-baseline justify-between gap-4">
              <span className="inline-flex min-w-0 items-center gap-1.5">
                <span
                  aria-hidden
                  className="h-2 w-2 shrink-0 rounded-full"
                  style={{ backgroundColor: toneColor(item.tone, index, isDarkMode) }}
                />
                <Text size="sm" as="span">{item.name}</Text>
              </span>
              <Text size="sm" as="span">
                {valueFormat ? valueFormat(value) : String(value)}
              </Text>
            </div>
          );
        })}
        <Text size="xs" variant="secondary">
          仅有 1 个样本（{formatTick(series.find((s) => s.points.length > 0)!.points[0][0])}），累积到 2 个后显示趋势曲线。
        </Text>
      </div>
    );
  }

  return (
    <div>
      <TimeseriesChart
        echarts={echarts}
        type={type}
        data={data}
        height={height}
        isDarkMode={isDarkMode}
        loading={loading}
        xAxisTickFormat={formatTick}
        yAxisTickFormat={valueFormat}
        tooltipValueFormat={valueFormat}
      />
      <ChartLegendRow
        items={data.map(({ name, color }) => ({ name, color }))}
      />
    </div>
  );
}
