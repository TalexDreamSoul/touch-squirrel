"use client";

import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { ChartPalette, Empty, Loader, Text, Tooltip } from "@cloudflare/kumo";
import { useTheme } from "@/lib/theme";
import { formatNumber, withAlpha } from "./chart-shared";

/** 单日数据。date 为 YYYY-MM-DD；detail 为主值的分项拆解。 */
export type CalendarDay = {
  date: string;
  value: number;
  detail?: Record<string, number>;
};

export type ContributionCalendarProps = {
  /** 已按日期升序、连续无缺口的每日数据 */
  days: CalendarDay[];
  /** 图表标题 */
  label: string;
  /** 主值单位，例如「个账号」 */
  unit?: string;
  /** 主色调序号，取 ChartPalette.categorical(colorSeed) */
  colorSeed?: number;
  /** detail 键 → 中文名，用于悬浮提示 */
  detailLabels?: Record<string, string>;
  loading?: boolean;
};

/** 格子边长的下限与上限；实际取值按容器宽度在两者之间伸缩。 */
const CELL_MIN = 9;
const CELL_MAX = 20;
/** 服务端预渲染时没有宽度可测，先用这个值。 */
const CELL_FALLBACK = 11;
const GAP = 3;
/** 月份标签行高度（16px）+ 与格子的间距（4px） */
const MONTH_ROW_OFFSET = 20;

/** 观测元素宽度；未挂载或不支持 ResizeObserver 时返回 0。 */
function useElementWidth(ref: React.RefObject<HTMLElement | null>): number {
  const [width, setWidth] = useState(0);
  useLayoutEffect(() => {
    const element = ref.current;
    if (!element || typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(([entry]) => {
      setWidth(entry.contentRect.width);
    });
    observer.observe(element);
    setWidth(element.getBoundingClientRect().width);
    return () => observer.disconnect();
  }, [ref]);
  return width;
}
const WEEKDAY_GUTTER_LABELS = ["一", "", "三", "", "五", "", ""] as const;
const WEEKDAY_NAMES = ["一", "二", "三", "四", "五", "六", "日"] as const;

function parseDate(value: string): Date {
  const [year, month, day] = value.split("-").map(Number);
  return new Date(year, (month || 1) - 1, day || 1);
}

/** 周一 = 0 … 周日 = 6 */
function mondayIndex(date: Date): number {
  return (date.getDay() + 6) % 7;
}

function CellTooltipContent({
  day,
  unit,
  detailLabels,
}: {
  day: CalendarDay;
  unit?: string;
  detailLabels?: Record<string, string>;
}) {
  const date = parseDate(day.date);
  const weekday = WEEKDAY_NAMES[mondayIndex(date)];
  const detailEntries = Object.entries(day.detail ?? {});
  return (
    <div className="flex max-w-56 flex-col gap-0.5">
      <Text size="xs" variant="secondary" as="span">
        {date.getFullYear()}年{date.getMonth() + 1}月{date.getDate()}日 · 周
        {weekday}
      </Text>
      <Text size="xs" bold as="span">
        {formatNumber(day.value)}
        {unit ? ` ${unit}` : ""}
      </Text>
      {detailEntries.map(([key, count]) => (
        <Text key={key} size="xs" variant="secondary" as="span">
          {detailLabels?.[key] ?? key}：{formatNumber(count)}
        </Text>
      ))}
    </div>
  );
}

/**
 * GitHub 风格贡献日历：列为周（周一起始）、行为周一至周日。
 * 手写 CSS grid 方块矩阵；颜色 0 值一档 + 4 档强度，全部取自 ChartPalette。
 */
export function ContributionCalendar({
  days,
  label,
  unit,
  colorSeed = 0,
  detailLabels,
  loading = false,
}: ContributionCalendarProps) {
  const { resolved } = useTheme();
  const isDarkMode = resolved === "dark";
  const scrollRef = useRef<HTMLDivElement | null>(null);
  const available = useElementWidth(scrollRef);

  const weeks = useMemo(() => {
    const columns: (CalendarDay | null)[][] = [];
    let current: (CalendarDay | null)[] = [];
    days.forEach((day, index) => {
      if (index === 0) {
        const offset = mondayIndex(parseDate(day.date));
        for (let i = 0; i < offset; i += 1) current.push(null);
      }
      current.push(day);
      if (current.length === 7) {
        columns.push(current);
        current = [];
      }
    });
    if (current.length > 0) {
      while (current.length < 7) current.push(null);
      columns.push(current);
    }
    return columns;
  }, [days]);

  const monthLabels = useMemo(() => {
    const labels: { column: number; text: string }[] = [];
    let previousMonth = -1;
    weeks.forEach((week, column) => {
      const first = week.find((day) => day !== null);
      if (!first) return;
      const month = parseDate(first.date).getMonth();
      if (month === previousMonth) return;
      previousMonth = month;
      const last = labels[labels.length - 1];
      // 两个标签相邻过近时保留较新的月份，避免文字重叠
      if (last && column - last.column < 2) labels.pop();
      labels.push({ column, text: `${month + 1}月` });
    });
    return labels;
  }, [weeks]);

  const maxValue = useMemo(
    () => days.reduce((acc, day) => Math.max(acc, day.value), 0),
    [days],
  );

  // Grow the squares to fill the card for short ranges (a 30-day grid at 11px
  // occupies a sliver of a full-width card) and shrink them for a full year,
  // which then scrolls horizontally rather than overflowing.
  const cell = useMemo(() => {
    if (available <= 0 || weeks.length === 0) return CELL_FALLBACK;
    const fitted = Math.floor((available + GAP) / weeks.length) - GAP;
    return Math.min(CELL_MAX, Math.max(CELL_MIN, fitted));
  }, [available, weeks.length]);

  const column = cell + GAP;

  const scale = useMemo(() => {
    const base = ChartPalette.categorical(colorSeed, isDarkMode);
    return [
      ChartPalette.semantic("Skeleton", isDarkMode),
      withAlpha(base, 0.3),
      withAlpha(base, 0.55),
      withAlpha(base, 0.78),
      base,
    ];
  }, [colorSeed, isDarkMode]);

  useEffect(() => {
    const element = scrollRef.current;
    if (element) element.scrollLeft = element.scrollWidth;
  }, [weeks.length, loading]);

  const levelOf = (value: number): number => {
    if (value <= 0 || maxValue <= 0) return 0;
    const ratio = value / maxValue;
    if (ratio <= 0.25) return 1;
    if (ratio <= 0.5) return 2;
    if (ratio <= 0.75) return 3;
    return 4;
  };

  if (loading) {
    return (
      <div className="flex min-h-36 items-center justify-center">
        <Loader aria-label="加载中" />
      </div>
    );
  }

  if (days.length === 0) {
    return (
      <Empty
        size="sm"
        title="暂无数据"
        description="所选时间范围内没有可展示的记录"
      />
    );
  }

  const gridWidth = weeks.length * column - GAP;

  return (
    <div>
      <Text size="sm" bold as="p">
        {label}
      </Text>
      <div className="mt-3 flex gap-2">
        <div
          aria-hidden
          className="grid shrink-0"
          style={{
            gridTemplateRows: `repeat(7, ${cell}px)`,
            rowGap: GAP,
            marginTop: MONTH_ROW_OFFSET,
          }}
        >
          {WEEKDAY_GUTTER_LABELS.map((text, row) =>
            text ? (
              <Text
                key={row}
                size="xs"
                variant="secondary"
                as="span"
                DANGEROUS_style={{ lineHeight: `${cell}px` }}
              >
                {text}
              </Text>
            ) : (
              <span key={row} />
            ),
          )}
        </div>
        <div ref={scrollRef} className="min-w-0 grow overflow-x-auto pb-1">
          <div style={{ width: gridWidth }}>
            <div className="relative mb-1 h-4">
              {monthLabels.map(({ column: weekColumn, text }) => (
                <Text
                  key={`${weekColumn}-${text}`}
                  size="xs"
                  variant="secondary"
                  as="span"
                  DANGEROUS_className="absolute top-0 leading-4 whitespace-nowrap"
                  DANGEROUS_style={{ left: weekColumn * column }}
                >
                  {text}
                </Text>
              ))}
            </div>
            <div
              role="img"
              aria-label={`${label}：${days.length} 天的每日分布`}
              className="grid"
              style={{
                gridTemplateRows: `repeat(7, ${cell}px)`,
                gridAutoFlow: "column",
                gridAutoColumns: `${cell}px`,
                gap: GAP,
              }}
            >
              {weeks.map((week, weekColumn) =>
                week.map((day, row) =>
                  day ? (
                    <Tooltip
                      key={day.date}
                      delay={150}
                      closeDelay={0}
                      content={
                        <CellTooltipContent
                          day={day}
                          unit={unit}
                          detailLabels={detailLabels}
                        />
                      }
                      render={
                        <span
                          className="block rounded-[2px] hover:ring-1 hover:ring-kumo-contrast/40"
                          style={{
                            height: cell,
                            width: cell,
                            backgroundColor: scale[levelOf(day.value)],
                          }}
                        />
                      }
                    />
                  ) : (
                    <span key={`pad-${weekColumn}-${row}`} aria-hidden />
                  ),
                ),
              )}
            </div>
          </div>
        </div>
      </div>
      <div className="mt-2 flex items-center justify-end gap-1">
        <Text size="xs" variant="secondary" as="span">
          少
        </Text>
        {scale.map((color, level) => (
          <span
            key={level}
            className="h-[11px] w-[11px] rounded-[2px]"
            style={{ backgroundColor: color }}
          />
        ))}
        <Text size="xs" variant="secondary" as="span">
          多
        </Text>
      </div>
    </div>
  );
}
