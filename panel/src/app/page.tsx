"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { Badge, Button, LayerCard, Meter, Text } from "@cloudflare/kumo";
import { AdminShell } from "@/components/admin-shell";
import { PageHeader } from "@/components/page-header";
import { ContributionCalendar, DonutChart, StatTile } from "@/components/charts";
import { api, type ClusterStatus, type RunStatus } from "@/lib/api";
import {
  CALENDAR_DETAIL_LABELS,
  fetchAnalytics,
  formatCount,
  formatDuration,
  formatPercent,
  type AnalyticsOverview,
} from "@/lib/analytics";
import { useTimezone } from "@/lib/timezone";

type OverviewResp = {
  ok: boolean;
  overview?: {
    healthy: number;
    rate_limited: number;
    dead: number;
    disabled: number;
    total: number;
    quota_estimate: number;
  };
  patrol?: { enabled: boolean; running: boolean };
  refill?: { enabled: boolean; min_healthy: number; batch: number };
  cleanup?: { enabled: boolean; dry_run: boolean };
};

type HealthResp = {
  ok: boolean;
  service?: string;
  time?: string;
  auth?: boolean;
  build?: { version?: string; commit?: string; repository?: string };
  jobs?: {
    upload?: { total: number; running: number };
    export?: { total: number; running: number };
  };
};

type MetricsSummary = {
  run_count: number;
  account_count: number;
  average_account_ms: number;
  p50_account_ms: number;
  p95_account_ms: number;
  success_rate: number;
  failure_rate: number;
  throughput_per_hour: number;
};

type LocalInventory = { total?: number; synced?: number; unsynced?: number };

/** The live feed refreshes fast; the analytics feed walks run dirs, so it does not. */
const LIVE_INTERVAL_MS = 5_000;
const ANALYTICS_INTERVAL_MS = 60_000;
/** A full year, so the calendar fills its card instead of hugging the left edge. */
const ANALYTICS_DAYS = 365;

export default function OverviewPage() {
  const [status, setStatus] = useState<RunStatus | null>(null);
  const [pool, setPool] = useState<OverviewResp | null>(null);
  const [cluster, setCluster] = useState<ClusterStatus | null>(null);
  const [health, setHealth] = useState<HealthResp | null>(null);
  const [metrics, setMetrics] = useState<MetricsSummary | null>(null);
  const [localInventory, setLocalInventory] = useState<LocalInventory | null>(null);
  const [analytics, setAnalytics] = useState<AnalyticsOverview | null>(null);
  const [error, setError] = useState("");
  const { timezone } = useTimezone();

  const load = useCallback(async () => {
    try {
      const [st, ov, cl, service, registration, local] = await Promise.all([
        api<{ ok: boolean; status: RunStatus }>("/api/status"),
        api<OverviewResp>("/api/pool/overview"),
        api<{ ok: boolean; cluster: ClusterStatus }>("/api/cluster/status").catch(() => ({ ok: false, cluster: {} as ClusterStatus })),
        api<HealthResp>("/api/health"),
        api<{ summary?: MetricsSummary }>("/api/register-metrics?range=24h").catch(() => ({ summary: undefined })),
        api<LocalInventory>("/api/pool/list?source=local&page=1&limit=1").catch(() => ({})),
      ]);
      setStatus(st.status || null);
      setPool(ov);
      setCluster(cl.cluster);
      setHealth(service);
      setMetrics(registration.summary || null);
      setLocalInventory(local);
      setError("");
    } catch (loadError) {
      setError(loadError instanceof Error ? loadError.message : "加载失败");
    }
  }, []);

  const loadAnalytics = useCallback(async () => {
    try {
      setAnalytics(await fetchAnalytics(ANALYTICS_DAYS, timezone));
    } catch {
      // Charts are supplementary; a failure here must not blank the live panel.
    }
  }, [timezone]);

  useEffect(() => {
    void load();
    const timer = window.setInterval(() => void load(), LIVE_INTERVAL_MS);
    return () => window.clearInterval(timer);
  }, [load]);

  useEffect(() => {
    void loadAnalytics();
    const timer = window.setInterval(() => void loadAnalytics(), ANALYTICS_INTERVAL_MS);
    return () => window.clearInterval(timer);
  }, [loadAnalytics]);

  const poolOverview = pool?.overview;
  const liveDone = status?.done ?? status?.success ?? 0;
  const liveTarget = status?.target ?? 0;
  const liveProgress = liveTarget > 0 ? Math.min(100, Math.round((liveDone / liveTarget) * 100)) : 0;
  const serviceHealthy = health?.ok === true;

  const registerCalendar = analytics?.calendars.find((c) => c.key === "register");
  const accountsCalendar = analytics?.calendars.find((c) => c.key === "accounts");
  const poolHealthDist = analytics?.distributions.find((d) => d.key === "pool_health");

  const poolItems = useMemo(
    () => [
      { name: "健康", value: poolOverview?.healthy || 0, tone: "success" as const },
      { name: "临时限流", value: poolOverview?.rate_limited || 0, tone: "warning" as const },
      { name: "不可用", value: poolOverview?.dead || 0, tone: "critical" as const },
      { name: "已停用", value: poolOverview?.disabled || 0, tone: "neutral" as const },
    ],
    [poolOverview],
  );

  return (
    <AdminShell>
      <PageHeader
        title="概览"
        description="注册成效、号池质量、任务队列与服务状态"
        actions={
          <Button variant="secondary" size="sm" onClick={() => { void load(); void loadAnalytics(); }}>
            刷新
          </Button>
        }
      />

      {error ? <div className="mb-3"><Text variant="error">{error}</Text></div> : null}

      <div className="mb-4 grid gap-3 sm:grid-cols-2 xl:grid-cols-6">
        <StatTile
          label="服务"
          value={serviceHealthy ? "正常" : "异常"}
          hint={health?.build?.version || health?.service || "touch-squirrel-panel"}
          tone={serviceHealthy ? "success" : "critical"}
        />
        <StatTile
          label="当前注册"
          value={status?.status || "stopped"}
          hint={status?.phase_detail || status?.phase || "无活动任务"}
          tone={status?.status === "running" ? "success" : "neutral"}
        />
        <StatTile
          label="24 小时成功率"
          value={formatPercent(metrics?.success_rate)}
          hint={`${formatCount(metrics?.account_count)} 个账号样本`}
          tone={(metrics?.success_rate || 0) >= 0.8 ? "success" : "warning"}
        />
        <StatTile
          label="24 小时吞吐"
          value={metrics?.throughput_per_hour ? `${metrics.throughput_per_hour.toFixed(1)}/小时` : "—"}
          hint={`${formatCount(metrics?.run_count)} 个任务`}
        />
        <StatTile
          label="本地凭证"
          value={formatCount(localInventory?.total ?? 0)}
          hint={`已上传 ${formatCount(localInventory?.synced ?? 0)} · 未上传 ${formatCount(localInventory?.unsynced ?? 0)}`}
          sparkline={accountsCalendar?.days.map((d) => d.value)}
        />
        <StatTile
          label="上传队列"
          value={formatCount(health?.jobs?.upload?.running || 0)}
          hint={`运行中 · 累计 ${formatCount(health?.jobs?.upload?.total || 0)}`}
          tone={(health?.jobs?.upload?.running || 0) > 0 ? "success" : "neutral"}
        />
      </div>

      <div className="mb-4 grid items-start gap-4 xl:grid-cols-3">
        <LayerCard>
          <LayerCard.Secondary>实时注册流水线</LayerCard.Secondary>
          <LayerCard.Primary className="p-4">
            <div className="flex flex-col gap-3">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <div className="min-w-0">
                  <Text size="sm">run {status?.run_id || "—"}</Text>
                  <Text size="xs" variant="secondary">{status?.phase_detail || status?.phase || "无活动任务"}</Text>
                </div>
                <Badge variant={status?.status === "running" ? "primary" : "secondary"}>{status?.status || "stopped"}</Badge>
              </div>
              <Meter value={liveProgress} max={100} label="当前进度" showValue />
              <div className="grid grid-cols-3 gap-3">
                <Mini label="进度" value={`${liveDone}/${liveTarget || 0}`} />
                <Mini label="失败" value={String(status?.fail_count ?? status?.fail ?? 0)} />
                <Mini label="速度" value={status?.rate_per_min ? `${status.rate_per_min.toFixed(1)}/分` : "—"} />
              </div>
              <Text size="xs" variant="secondary">
                workers S {status?.workers?.s || 0} · P {status?.workers?.p || 0} · C {status?.workers?.c || 0} · OAuth {status?.workers?.oauth || 0}
              </Text>
            </div>
          </LayerCard.Primary>
        </LayerCard>

        <LayerCard>
          <LayerCard.Secondary>24 小时注册质量</LayerCard.Secondary>
          <LayerCard.Primary className="p-4">
            <div className="grid grid-cols-2 gap-4">
              <Mini label="成功率" value={formatPercent(metrics?.success_rate)} />
              <Mini label="失败率" value={formatPercent(metrics?.failure_rate)} />
              <Mini label="平均账号耗时" value={formatDuration(metrics?.average_account_ms)} />
              <Mini label="P95 账号耗时" value={formatDuration(metrics?.p95_account_ms)} />
              <Mini label="P50 账号耗时" value={formatDuration(metrics?.p50_account_ms)} />
              <Mini label="账号样本" value={formatCount(metrics?.account_count || 0)} />
            </div>
          </LayerCard.Primary>
        </LayerCard>

        <LayerCard>
          <LayerCard.Secondary>号池巡检分布</LayerCard.Secondary>
          <LayerCard.Primary className="p-4">
            {poolOverview?.total ? (
              <DonutChart items={poolItems} height={220} />
            ) : poolHealthDist && poolHealthDist.total > 0 ? (
              <DonutChart items={poolHealthDist.items} height={220} />
            ) : (
              <Text variant="secondary">尚未执行号池巡检；本地凭证总量见顶部指标</Text>
            )}
          </LayerCard.Primary>
        </LayerCard>
      </div>

      <LayerCard className="mb-4">
        <LayerCard.Secondary>
          <div className="flex w-full flex-wrap items-center justify-between gap-2">
            <span>近一年注册产出</span>
            {registerCalendar ? (
              <Text size="xs" variant="secondary">
                共 {formatCount(registerCalendar.total)} 个账号 · 活跃 {registerCalendar.active_days} 天 · 单日峰值 {formatCount(registerCalendar.max)}
              </Text>
            ) : null}
          </div>
        </LayerCard.Secondary>
        <LayerCard.Primary className="p-4">
          {registerCalendar ? (
            <ContributionCalendar
              days={registerCalendar.days}
              label={registerCalendar.label}
              unit={registerCalendar.unit}
              detailLabels={CALENDAR_DETAIL_LABELS.register}
            />
          ) : (
            <Text variant="secondary">正在加载注册活动…</Text>
          )}
        </LayerCard.Primary>
      </LayerCard>

      <div className="grid items-start gap-4 lg:grid-cols-2">
        <LayerCard>
          <LayerCard.Secondary>服务与后台任务</LayerCard.Secondary>
          <LayerCard.Primary className="p-4">
            <div className="flex flex-col gap-3">
              <ServiceLine label="Panel API" state={serviceHealthy ? "online" : "offline"} detail={health?.build?.version || health?.build?.commit || "build 未标记"} good={serviceHealthy} />
              <ServiceLine label="上传任务" state={`${health?.jobs?.upload?.running || 0} running`} detail={`${health?.jobs?.upload?.total || 0} total`} good />
              <ServiceLine label="导出任务" state={`${health?.jobs?.export?.running || 0} running`} detail={`${health?.jobs?.export?.total || 0} total`} good />
              <ServiceLine label="号池巡检" state={pool?.patrol?.running ? "running" : pool?.patrol?.enabled ? "standby" : "disabled"} detail={pool?.cleanup?.enabled ? `自动清理${pool.cleanup.dry_run ? "（演练）" : ""}` : "自动清理关闭"} good={!pool?.patrol?.running || pool?.patrol?.enabled !== false} />
            </div>
          </LayerCard.Primary>
        </LayerCard>

        <LayerCard>
          <LayerCard.Secondary>节点与调度</LayerCard.Secondary>
          <LayerCard.Primary className="p-4">
            <div className="flex flex-col gap-3">
              <ServiceLine label="角色" state={cluster?.role || "standalone"} detail={cluster?.node_name || cluster?.node_id?.slice(0, 8) || "本机"} good />
              {cluster?.role === "master" ? (
                <>
                  <ServiceLine label="从节点" state={`${cluster.nodes?.filter((node) => node.online).length || 0}/${cluster.nodes?.length || 0} online`} detail={`号池目标 ${cluster.pool_target || 0} · 缺口 ${cluster.need || 0}`} good={(cluster.nodes?.filter((node) => node.online).length || 0) > 0} />
                  <ServiceLine label="自动补量" state={pool?.refill?.enabled ? "enabled" : "disabled"} detail={`阈值 ${pool?.refill?.min_healthy || 0} · 单批 ${pool?.refill?.batch || 0}`} good={!!pool?.refill?.enabled} />
                </>
              ) : null}
              {cluster?.role === "slave" ? (
                <ServiceLine label="主节点连接" state={cluster.slave_connected ? "connected" : "disconnected"} detail={cluster.last_assign ? `上次分配 ${cluster.last_assign}` : "暂无分配"} good={!!cluster.slave_connected} />
              ) : null}
              {(!cluster?.role || cluster.role === "standalone") ? (
                <Text size="sm" variant="secondary">单机模式，无主从调度依赖</Text>
              ) : null}
            </div>
          </LayerCard.Primary>
        </LayerCard>
      </div>
    </AdminShell>
  );
}

function Mini({ label, value }: { label: string; value: string }) {
  return <div><Text size="xs" variant="secondary">{label}</Text><Text size="sm">{value}</Text></div>;
}

function ServiceLine({ label, state, detail, good }: { label: string; state: string; detail: string; good: boolean }) {
  return (
    <div className="flex items-center justify-between gap-4 border-b border-kumo-hairline pb-3 last:border-b-0 last:pb-0">
      <div><Text size="sm">{label}</Text><Text size="xs" variant="secondary">{detail}</Text></div>
      <Badge variant={good ? "primary" : "secondary"}>{state}</Badge>
    </div>
  );
}
