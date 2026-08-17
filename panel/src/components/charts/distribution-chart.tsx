"use client";

import { useMemo } from "react";
import {
  Chart,
  ChartPalette,
  Empty,
  Text,
  type KumoChartOption,
} from "@cloudflare/kumo";
import type { BarSeriesOption, PieSeriesOption } from "echarts/charts";
import { useTheme } from "@/lib/theme";
import {
  ChartLegendRow,
  ChartLoadingBox,
  formatNumber,
  toneColor,
  type ChartTone,
} from "./chart-shared";
import { echarts } from "./echarts-setup";

export type DistributionItem = {
  name: string;
  value: number;
  /** 语义色调；不传时条形图统一用强调色、环形图按序号取 categorical 色 */
  tone?: ChartTone;
};

export type DistributionChartProps = {
  items: DistributionItem[];
  height?: number;
  loading?: boolean;
};

const tooltipValueFormatter = (value: unknown): string =>
  typeof value === "number" ? formatNumber(value) : String(value ?? "-");

/**
 * 横向条形分布图：条尾直接标数值，隐藏数值轴与网格线。
 * 数值为 0 的条目也保留整行（条为空、标签为 0）。
 */
export function DistributionChart({
  items,
  height,
  loading = false,
}: DistributionChartProps) {
  const { resolved } = useTheme();
  const isDarkMode = resolved === "dark";
  const chartHeight = height ?? Math.max(132, items.length * 34 + 16);

  const options = useMemo<KumoChartOption>(() => {
    const barSeries: BarSeriesOption = {
      type: "bar",
      barMaxWidth: 18,
      itemStyle: { borderRadius: [0, 4, 4, 0] },
      label: {
        show: true,
        position: "right",
        color: ChartPalette.text("primary", isDarkMode),
        fontSize: 12,
        formatter: (params) => formatNumber(Number(params.value ?? 0)),
      },
      data: items.map((item) => ({
        name: item.name,
        value: item.value,
        itemStyle: { color: toneColor(item.tone, 0, isDarkMode) },
      })),
    };
    return {
      animationDuration: 240,
      grid: { left: 8, right: 56, top: 8, bottom: 8, containLabel: true },
      tooltip: { trigger: "item", valueFormatter: tooltipValueFormatter },
      xAxis: { type: "value", show: false },
      yAxis: {
        type: "category",
        inverse: true,
        data: items.map((item) => item.name),
        axisLine: { show: false },
        axisTick: { show: false },
        axisLabel: {
          color: ChartPalette.text("secondary", isDarkMode),
          fontSize: 12,
        },
      },
      series: [barSeries],
    };
  }, [items, isDarkMode]);

  if (loading) return <ChartLoadingBox height={chartHeight} />;
  if (items.length === 0) {
    return (
      <Empty size="sm" title="暂无数据" description="还没有可统计的分布数据" />
    );
  }

  return (
    <Chart
      echarts={echarts}
      options={options}
      height={chartHeight}
      isDarkMode={isDarkMode}
    />
  );
}

/**
 * 环形分布图：radius 55%–78%，中心显示总数，图例在图下方。
 * 与 DistributionChart 共用同一套 Props。
 */
export function DonutChart({
  items,
  height,
  loading = false,
}: DistributionChartProps) {
  const { resolved } = useTheme();
  const isDarkMode = resolved === "dark";
  const chartHeight = height ?? 220;

  const colored = useMemo(
    () =>
      items.map((item, index) => ({
        name: item.name,
        value: item.value,
        color: toneColor(item.tone, index, isDarkMode),
      })),
    [items, isDarkMode],
  );

  const total = useMemo(
    () => items.reduce((acc, item) => acc + item.value, 0),
    [items],
  );

  const options = useMemo<KumoChartOption>(() => {
    const pieSeries: PieSeriesOption = {
      type: "pie",
      radius: ["55%", "78%"],
      center: ["50%", "50%"],
      padAngle: 2,
      itemStyle: { borderRadius: 3 },
      label: { show: false },
      labelLine: { show: false },
      emphasis: { scaleSize: 3 },
      data: colored.map((item) => ({
        name: item.name,
        value: item.value,
        itemStyle: { color: item.color },
      })),
    };
    return {
      animationDuration: 240,
      tooltip: { trigger: "item", valueFormatter: tooltipValueFormatter },
      series: [pieSeries],
    };
  }, [colored]);

  if (loading) return <ChartLoadingBox height={chartHeight} />;
  if (items.length === 0) {
    return (
      <Empty size="sm" title="暂无数据" description="还没有可统计的分布数据" />
    );
  }

  return (
    <div>
      <div className="relative">
        <Chart
          echarts={echarts}
          options={options}
          height={chartHeight}
          isDarkMode={isDarkMode}
        />
        <div className="pointer-events-none absolute inset-0 flex flex-col items-center justify-center">
          <Text size="lg" bold as="span">
            {formatNumber(total)}
          </Text>
          <Text size="xs" variant="secondary" as="span">
            总计
          </Text>
        </div>
      </div>
      <ChartLegendRow
        items={colored.map(({ name, color }) => ({ name, color }))}
      />
    </div>
  );
}
