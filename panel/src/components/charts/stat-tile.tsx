"use client";

import { ChartPalette, LayerCard, Text } from "@cloudflare/kumo";
import { useTheme } from "@/lib/theme";

export type StatTileDelta = {
  /** 环比变化（百分比数值，正为上升、负为下降） */
  value: number;
  /** 上升是好还是下降是好，决定颜色方向 */
  goodWhen: "up" | "down";
};

/** Same vocabulary as `ChartTone`, minus the identity-only `primary`. */
export type StatTileTone = "success" | "warning" | "critical" | "neutral";

export type StatTileProps = {
  label: string;
  /** 已格式化好的展示值 */
  value: string;
  hint?: string;
  delta?: StatTileDelta;
  tone?: StatTileTone;
  /** 极简迷你走势（22px 高内联 SVG，无坐标轴） */
  sparkline?: number[];
};

function toneDotColor(tone: StatTileTone, isDarkMode: boolean): string | null {
  switch (tone) {
    case "success":
      return ChartPalette.semantic("Success", isDarkMode);
    case "warning":
      return ChartPalette.semantic("Warning", isDarkMode);
    case "critical":
      return ChartPalette.semantic("Attention", isDarkMode);
    default:
      return null;
  }
}

function formatPercent(value: number): string {
  return `${Math.abs(value).toFixed(1).replace(/\.0$/, "")}%`;
}

function DeltaText({ delta }: { delta: StatTileDelta }) {
  if (delta.value === 0) {
    return (
      <Text size="xs" variant="secondary" as="span">
        0%
      </Text>
    );
  }
  const isUp = delta.value > 0;
  const isGood =
    (isUp && delta.goodWhen === "up") || (!isUp && delta.goodWhen === "down");
  return (
    <Text size="xs" bold as="span" variant={isGood ? "success" : "error"}>
      {isUp ? "↑" : "↓"} {formatPercent(delta.value)}
    </Text>
  );
}

function Sparkline({ values, color }: { values: number[]; color: string }) {
  if (values.length < 2) return null;
  const width = 96;
  const height = 22;
  const padY = 2;
  const min = Math.min(...values);
  const max = Math.max(...values);
  const span = max - min || 1;
  const step = width / (values.length - 1);
  const points = values
    .map((value, index) => {
      const x = (index * step).toFixed(1);
      const y = (
        height -
        padY -
        ((value - min) / span) * (height - padY * 2)
      ).toFixed(1);
      return `${x},${y}`;
    })
    .join(" ");
  return (
    <svg
      aria-hidden
      viewBox={`0 0 ${width} ${height}`}
      preserveAspectRatio="none"
      className="h-[22px] w-24 shrink-0 overflow-visible"
    >
      <polyline
        points={points}
        fill="none"
        stroke={color}
        strokeWidth={2}
        strokeLinecap="round"
        strokeLinejoin="round"
        vectorEffect="non-scaling-stroke"
      />
    </svg>
  );
}

/** 指标卡：LayerCard 布局，纯 kumo 组件 + 内联 SVG 迷你走势，不依赖 echarts。 */
export function StatTile({
  label,
  value,
  hint,
  delta,
  tone = "neutral",
  sparkline,
}: StatTileProps) {
  const { resolved } = useTheme();
  const isDarkMode = resolved === "dark";
  const dotColor = toneDotColor(tone, isDarkMode);
  const sparklineColor = dotColor ?? ChartPalette.categorical(0, isDarkMode);

  return (
    <LayerCard className="p-4">
      <div className="flex flex-col gap-1">
        <div className="flex items-center gap-1.5">
          {dotColor ? (
            <span
              aria-hidden
              className="h-2 w-2 shrink-0 rounded-full"
              style={{ backgroundColor: dotColor }}
            />
          ) : null}
          <Text size="xs" variant="secondary" as="span" truncate>
            {label}
          </Text>
        </div>
        <div className="flex items-end justify-between gap-3">
          <Text variant="heading3" as="span" truncate>
            {value}
          </Text>
          {sparkline ? (
            <Sparkline values={sparkline} color={sparklineColor} />
          ) : null}
        </div>
        {delta || hint ? (
          <div className="flex items-center gap-2">
            {delta ? <DeltaText delta={delta} /> : null}
            {hint ? (
              <Text size="xs" variant="secondary" as="span" truncate>
                {hint}
              </Text>
            ) : null}
          </div>
        ) : null}
      </div>
    </LayerCard>
  );
}
