"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { ArrowsClockwiseIcon } from "@phosphor-icons/react";
import { Badge, Button, LayerCard, Select, Tabs, Text } from "@cloudflare/kumo";
import { AdminShell } from "@/components/admin-shell";
import { PageHeader } from "@/components/page-header";
import {
  ContributionCalendar,
  DistributionChart,
  DonutChart,
  StatTile,
  TrendChart,
} from "@/components/charts";
import {
  CALENDAR_DETAIL_LABELS,
  RANGE_OPTIONS,
  fetchAnalytics,
  formatCount,
  formatDuration,
  formatPercent,
  type AnalyticsOverview,
  type Calendar,
} from "@/lib/analytics";
import { timezoneOffsetLabel, useTimezone } from "@/lib/timezone";

/** Sums one detail key across a calendar's days. */
function detailTotal(calendar: Calendar | undefined, key: string): number {
  if (!calendar) return 0;
  return calendar.days.reduce((sum, day) => sum + (day.detail?.[key] ?? 0), 0);
}

function byKey(calendars: Calendar[] | undefined, key: string): Calendar | undefined {
  return calendars?.find((calendar) => calendar.key === key);
}

export default function AnalyticsPage() {
  // A full year by default, like GitHub's contribution graph: it fills the
  // calendar card and makes gaps in activity visible at a glance.
  const [days, setDays] = useState("365");
  const [data, setData] = useState<AnalyticsOverview | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [calendarTab, setCalendarTab] = useState("register");
  const { timezone } = useTimezone();

  const load = useCallback(async () => {
    setLoading(true);
    try {
      setData(await fetchAnalytics(Number(days), timezone));
      setError("");
    } catch (loadError) {
      setError(loadError instanceof Error ? loadError.message : "加载失败");
    } finally {
      setLoading(false);
    }
  }, [days, timezone]);

  useEffect(() => {
    void load();
  }, [load]);

  const calendars = data?.calendars;
  const register = byKey(calendars, "register");
  const accounts = byKey(calendars, "accounts");
  const upload = byKey(calendars, "upload");
  const degrade = byKey(calendars, "degrade");
  const patrol = byKey(calendars, "patrol");

  const registerSuccess = detailTotal(register, "success");
  const registerFailed = detailTotal(register, "failed");
  const registerIncomplete = detailTotal(register, "incomplete");
  const registerRate =
    register && register.total > 0 ? registerSuccess / register.total : undefined;

  const degradeChecked = detailTotal(degrade, "checked");
  const degradeHit = detailTotal(degrade, "degraded");
  const degradeRate = degradeChecked > 0 ? degradeHit / degradeChecked : undefined;

  const poolHealth = data?.distributions.find((d) => d.key === "pool_health");
  const healthyCount = poolHealth?.items.find((i) => i.name === "健康")?.value ?? 0;
  const healthRate =
    poolHealth && poolHealth.total > 0 ? healthyCount / poolHealth.total : undefined;

  const trendSeries = useMemo(() => {
    const points = data?.points ?? [];
    if (points.length === 0) return [];
    const toStamp = (value: string) => new Date(value).getTime();
    return [
      {
        name: "账号数",
        points: points.map((p) => [toStamp(p.time), p.account_count] as [number, number]),
      },
    ];
  }, [data?.points]);

  const durationSeries = useMemo(() => {
    const points = data?.points ?? [];
    if (points.length === 0) return [];
    const toStamp = (value: string) => new Date(value).getTime();
    return [
      {
        name: "平均耗时",
        points: points.map((p) => [toStamp(p.time), p.average_account_ms] as [number, number]),
      },
      {
        name: "P95 耗时",
        points: points.map((p) => [toStamp(p.time), p.p95_account_ms] as [number, number]),
        tone: "warning" as const,
      },
    ];
  }, [data?.points]);

  const stageItems = useMemo(
    () =>
      (data?.stages ?? []).slice(0, 8).map((stage) => ({
        name: stage.name,
        value: stage.avg_ms,
      })),
    [data?.stages],
  );

  const calendarTabs = useMemo(
    () => (calendars ?? []).map((c) => ({ value: c.key, label: c.label })),
    [calendars],
  );
  const activeCalendar = byKey(calendars, calendarTab) ?? calendars?.[0];

  return (
    <AdminShell>
      <PageHeader
        title="数据分析"
        description={
          data
            ? `注册产出、凭证流转、巡检与降智的长期趋势 · 按 ${data.timezone.name}（${timezoneOffsetLabel(data.timezone.name)}）划分每一天`
            : "注册产出、凭证流转、巡检与降智的长期趋势"
        }
        actions={
          <>
            <div className="min-w-36">
              <Select value={days} onValueChange={(value) => value && setDays(value)}>
                {RANGE_OPTIONS.map((option) => (
                  <Select.Option key={option.value} value={option.value}>
                    {option.label}
                  </Select.Option>
                ))}
              </Select>
            </div>
            <Button size="sm" variant="secondary" loading={loading} onClick={() => void load()}>
              <ArrowsClockwiseIcon size={16} /> 刷新
            </Button>
          </>
        }
      />

      {error ? (
        <div className="mb-3">
          <Text variant="error">{error}</Text>
        </div>
      ) : null}

      <div className="mb-4 grid gap-3 sm:grid-cols-2 xl:grid-cols-5">
        <StatTile
          label="注册产出"
          value={formatCount(register?.total)}
          hint={`成功 ${formatCount(registerSuccess)} · 失败 ${formatCount(registerFailed)} · 未完成 ${formatCount(registerIncomplete)}`}
          sparkline={register?.days.map((d) => d.value)}
        />
        <StatTile
          label="注册成功率"
          value={formatPercent(registerRate)}
          hint={`${formatCount(data?.register_runs)} 次运行`}
          tone={registerRate === undefined ? "neutral" : registerRate >= 0.8 ? "success" : "warning"}
        />
        <StatTile
          label="入池凭证"
          value={formatCount(accounts?.total)}
          hint={`已同步 ${formatCount(upload?.total)} 条`}
          sparkline={accounts?.days.map((d) => d.value)}
        />
        <StatTile
          label="号池健康率"
          value={formatPercent(healthRate)}
          hint={`健康 ${formatCount(healthyCount)} / ${formatCount(poolHealth?.total)}`}
          tone={healthRate === undefined ? "neutral" : healthRate >= 0.7 ? "success" : "warning"}
        />
        <StatTile
          label="降智检出率"
          value={formatPercent(degradeRate)}
          hint={
            degradeChecked > 0
              ? `${formatCount(degradeHit)} / ${formatCount(degradeChecked)} 次检测`
              : "窗口内未执行抽样"
          }
          tone={degradeRate === undefined ? "neutral" : degradeRate > 0.1 ? "critical" : "success"}
        />
      </div>

      <LayerCard className="mb-4">
        <LayerCard.Secondary>
          <div className="flex w-full flex-wrap items-center justify-between gap-2">
            <span>活动日历</span>
            {activeCalendar ? (
              <div className="flex flex-wrap items-center gap-2">
                <Badge variant="secondary">
                  活跃 {activeCalendar.active_days} 天
                </Badge>
                <Badge variant="secondary">单日峰值 {formatCount(activeCalendar.max)}</Badge>
                {activeCalendar.streak > 0 ? (
                  <Badge variant="primary">连续 {activeCalendar.streak} 天</Badge>
                ) : null}
              </div>
            ) : null}
          </div>
        </LayerCard.Secondary>
        <LayerCard.Primary className="p-4">
          <div className="mb-4">
            <Tabs
              variant="segmented"
              tabs={calendarTabs}
              value={activeCalendar?.key ?? calendarTab}
              onValueChange={(value) => value && setCalendarTab(value)}
            />
          </div>

          {activeCalendar ? (
            <>
              <div className="mb-3">
                <Text size="sm">{activeCalendar.description}</Text>
                {activeCalendar.partial ? (
                  <Text size="xs" variant="secondary">
                    {activeCalendar.since
                      ? `该指标自 ${activeCalendar.since} 起开始记录，更早的空白格表示尚未采集，而非当天没有活动。`
                      : "该指标尚未产生记录：巡检与降智只保留最近 50 条原始数据，日历从首次运行后开始累积。"}
                  </Text>
                ) : null}
              </div>
              <ContributionCalendar
                days={activeCalendar.days}
                label={activeCalendar.label}
                unit={activeCalendar.unit}
                colorSeed={calendarTabs.findIndex((t) => t.value === activeCalendar.key)}
                detailLabels={CALENDAR_DETAIL_LABELS[activeCalendar.key]}
                loading={loading && !data}
              />
            </>
          ) : (
            <Text variant="secondary">暂无日历数据</Text>
          )}
        </LayerCard.Primary>
      </LayerCard>

      <div className="mb-4 grid items-start gap-4 xl:grid-cols-2">
        <LayerCard>
          <LayerCard.Secondary>每次运行的账号产出</LayerCard.Secondary>
          <LayerCard.Primary className="p-4">
            {trendSeries.length > 0 ? (
              <TrendChart
                series={trendSeries}
                type="bar"
                height={260}
                valueFormat={(value) => `${formatCount(value)} 个`}
                loading={loading && !data}
              />
            ) : (
              <Text variant="secondary">窗口内没有注册运行记录</Text>
            )}
          </LayerCard.Primary>
        </LayerCard>

        <LayerCard>
          <LayerCard.Secondary>单账号耗时趋势</LayerCard.Secondary>
          <LayerCard.Primary className="p-4">
            {durationSeries.length > 0 ? (
              <TrendChart
                series={durationSeries}
                height={260}
                valueFormat={formatDuration}
                loading={loading && !data}
              />
            ) : (
              <Text variant="secondary">窗口内没有耗时样本</Text>
            )}
          </LayerCard.Primary>
        </LayerCard>
      </div>

      <div className="mb-4 grid items-start gap-4 lg:grid-cols-2 xl:grid-cols-4">
        {(data?.distributions ?? []).map((dist) => (
          <LayerCard key={dist.key}>
            <LayerCard.Secondary>
              {dist.label} · {formatCount(dist.total)}
            </LayerCard.Secondary>
            <LayerCard.Primary className="p-4">
              {dist.total > 0 ? (
                <DonutChart items={dist.items} height={220} loading={loading && !data} />
              ) : (
                <Text variant="secondary">尚无数据</Text>
              )}
            </LayerCard.Primary>
          </LayerCard>
        ))}
      </div>

      <div className="grid items-start gap-4 xl:grid-cols-2">
        <LayerCard>
          <LayerCard.Secondary>注册阶段平均耗时</LayerCard.Secondary>
          <LayerCard.Primary className="p-4">
            {stageItems.length > 0 ? (
              <DistributionChart
                items={stageItems}
                height={Math.max(200, stageItems.length * 34)}
                loading={loading && !data}
              />
            ) : (
              <Text variant="secondary">窗口内没有阶段打点</Text>
            )}
            {data?.stages?.length ? (
              <div className="mt-3">
                <Text size="xs" variant="secondary">
                  最慢阶段 {data.stages[0].name} 平均 {formatDuration(data.stages[0].avg_ms)}，
                  P95 {formatDuration(data.stages[0].p95_ms)}
                </Text>
              </div>
            ) : null}
          </LayerCard.Primary>
        </LayerCard>

        <LayerCard>
          <LayerCard.Secondary>运维动作</LayerCard.Secondary>
          <LayerCard.Primary className="p-4">
            <div className="flex flex-col gap-3">
              <OpsLine
                label="号池巡检"
                value={`${formatCount(patrol?.total)} 次`}
                detail={
                  patrol && patrol.total > 0
                    ? `覆盖 ${formatCount(detailTotal(patrol, "checked"))} 个账号 · 活跃 ${patrol.active_days} 天`
                    : "窗口内未记录巡检"
                }
              />
              <OpsLine
                label="降智抽样"
                value={`${formatCount(degrade?.total)} 次`}
                detail={
                  degradeChecked > 0
                    ? `检测 ${formatCount(degradeChecked)} 个账号 · 检出 ${formatCount(degradeHit)}`
                    : "窗口内未记录扫描"
                }
              />
              <OpsLine
                label="上传同步"
                value={`${formatCount(upload?.total)} 条`}
                detail={
                  upload && upload.active_days > 0
                    ? `分布在 ${upload.active_days} 天`
                    : "窗口内没有同步动作"
                }
              />
              <Text size="xs" variant="secondary">
                巡检、降智与清理的日历来自日度账本；原始记录只保留最近 50 条，账本自
                {data?.ledger.since ? ` ${data.ledger.since}` : "首次运行"} 起累积。
              </Text>
            </div>
          </LayerCard.Primary>
        </LayerCard>
      </div>
    </AdminShell>
  );
}

function OpsLine({ label, value, detail }: { label: string; value: string; detail: string }) {
  return (
    <div className="flex items-center justify-between gap-4 border-b border-kumo-hairline pb-3 last:border-b-0 last:pb-0">
      <div className="min-w-0">
        <Text size="sm">{label}</Text>
        <Text size="xs" variant="secondary">
          {detail}
        </Text>
      </div>
      <Text size="sm">{value}</Text>
    </div>
  );
}
