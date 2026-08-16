"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { Badge, Button, LayerCard, Text } from "@cloudflare/kumo";
import { AdminShell } from "@/components/admin-shell";
import { PageHeader } from "@/components/page-header";
import { api, type ClusterStatus, type RunStatus } from "@/lib/api";

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

function percent(value?: number) {
  return typeof value === "number" && Number.isFinite(value) ? `${(value * 100).toFixed(1)}%` : "—";
}

function duration(ms?: number) {
  if (!ms) return "—";
  if (ms < 1000) return `${ms} ms`;
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)} s`;
  return `${(ms / 60_000).toFixed(1)} min`;
}

export default function OverviewPage() {
  const [status, setStatus] = useState<RunStatus | null>(null);
  const [pool, setPool] = useState<OverviewResp | null>(null);
  const [cluster, setCluster] = useState<ClusterStatus | null>(null);
  const [health, setHealth] = useState<HealthResp | null>(null);
  const [metrics, setMetrics] = useState<MetricsSummary | null>(null);
  const [localInventory, setLocalInventory] = useState<LocalInventory | null>(null);
  const [error, setError] = useState("");

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

  useEffect(() => {
    void load();
    const timer = window.setInterval(() => void load(), 5000);
    return () => window.clearInterval(timer);
  }, [load]);

  const poolOverview = pool?.overview;
  const liveDone = status?.done ?? status?.success ?? 0;
  const liveTarget = status?.target ?? 0;
  const liveProgress = liveTarget > 0 ? Math.min(100, Math.round((liveDone / liveTarget) * 100)) : 0;
  const serviceHealthy = health?.ok === true;
  const poolRows = useMemo(
    () => [
      { label: "健康", value: poolOverview?.healthy || 0 },
      { label: "临时限流", value: poolOverview?.rate_limited || 0 },
      { label: "不可用", value: poolOverview?.dead || 0 },
      { label: "已停用", value: poolOverview?.disabled || 0 },
    ],
    [poolOverview],
  );

  return (
    <AdminShell>
      <PageHeader
        title="概览"
        description="注册成效、号池质量、任务队列与服务状态"
        actions={<Button variant="secondary" size="sm" onClick={() => void load()}>刷新</Button>}
      />

      {error ? <div className="mb-3"><Text variant="error">{error}</Text></div> : null}

      <div className="mb-4 grid gap-3 sm:grid-cols-2 xl:grid-cols-6">
        <Metric label="服务" value={serviceHealthy ? "正常" : "异常"} hint={health?.service || "touch-squirrel-panel"} primary={serviceHealthy} />
        <Metric label="当前注册" value={status?.status || "stopped"} hint={status?.phase_detail || status?.phase || "无活动任务"} primary={status?.status === "running"} />
        <Metric label="24h 成功率" value={percent(metrics?.success_rate)} hint={`${metrics?.account_count || 0} 个账号样本`} primary={(metrics?.success_rate || 0) >= 0.8} />
        <Metric label="24h 吞吐" value={metrics?.throughput_per_hour ? `${metrics.throughput_per_hour.toFixed(1)}/h` : "—"} hint={`${metrics?.run_count || 0} 个任务`} />
        <Metric label="本地凭证" value={String(localInventory?.total ?? 0)} hint={`已上传 ${localInventory?.synced ?? 0} · 未上传 ${localInventory?.unsynced ?? 0}`} primary={(localInventory?.total || 0) > 0} />
        <Metric label="上传队列" value={String(health?.jobs?.upload?.running || 0)} hint={`运行中 · 累计 ${health?.jobs?.upload?.total || 0}`} primary={(health?.jobs?.upload?.running || 0) > 0} />
      </div>

      <div className="mb-4 grid gap-4 xl:grid-cols-3">
        <LayerCard>
          <LayerCard.Secondary>实时注册流水线</LayerCard.Secondary>
          <LayerCard.Primary className="p-4">
            <div className="flex flex-col gap-3">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <div>
                  <Text size="sm">run {status?.run_id || "—"}</Text>
                  <Text size="xs" variant="secondary">{status?.phase_detail || status?.phase || "无活动任务"}</Text>
                </div>
                <Badge variant={status?.status === "running" ? "primary" : "secondary"}>{status?.status || "stopped"}</Badge>
              </div>
              <progress className="w-full" max={100} value={liveProgress} aria-label="当前注册进度" />
              <div className="grid grid-cols-3 gap-3">
                <Mini label="进度" value={`${liveDone}/${liveTarget || 0}`} />
                <Mini label="失败" value={String(status?.fail_count ?? status?.fail ?? 0)} />
                <Mini label="速度" value={status?.rate_per_min ? `${status.rate_per_min.toFixed(1)}/min` : "—"} />
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
              <Mini label="成功率" value={percent(metrics?.success_rate)} />
              <Mini label="失败率" value={percent(metrics?.failure_rate)} />
              <Mini label="平均账号耗时" value={duration(metrics?.average_account_ms)} />
              <Mini label="P95 账号耗时" value={duration(metrics?.p95_account_ms)} />
              <Mini label="P50 账号耗时" value={duration(metrics?.p50_account_ms)} />
              <Mini label="账号样本" value={String(metrics?.account_count || 0)} />
            </div>
          </LayerCard.Primary>
        </LayerCard>

        <LayerCard>
          <LayerCard.Secondary>号池巡检分布</LayerCard.Secondary>
          <LayerCard.Primary className="p-4">
            {poolOverview?.total ? (
              <div className="flex flex-col gap-3">
                {poolRows.map((row) => (
                  <Distribution key={row.label} label={row.label} value={row.value} total={poolOverview.total} />
                ))}
              </div>
            ) : (
              <Text variant="secondary">尚未执行号池巡检；本地凭证总量见顶部指标</Text>
            )}
          </LayerCard.Primary>
        </LayerCard>
      </div>

      <div className="grid gap-4 lg:grid-cols-2">
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

function Metric({ label, value, hint, primary = false }: { label: string; value: string; hint: string; primary?: boolean }) {
  return (
    <LayerCard>
      <LayerCard.Secondary>{label}</LayerCard.Secondary>
      <LayerCard.Primary className="p-4">
        <div className="flex items-center justify-between gap-2">
          <Text variant="heading3" as="h3">{value}</Text>
          {primary ? <Badge variant="primary">实时</Badge> : null}
        </div>
        <Text size="xs" variant="secondary">{hint}</Text>
      </LayerCard.Primary>
    </LayerCard>
  );
}

function Mini({ label, value }: { label: string; value: string }) {
  return <div><Text size="xs" variant="secondary">{label}</Text><Text size="sm">{value}</Text></div>;
}

function Distribution({ label, value, total }: { label: string; value: number; total: number }) {
  const ratio = total > 0 ? Math.round((value / total) * 100) : 0;
  return (
    <div>
      <div className="mb-1 flex justify-between gap-3"><Text size="xs">{label}</Text><Text size="xs" variant="secondary">{value} · {ratio}%</Text></div>
      <progress className="w-full" max={100} value={ratio} aria-label={`${label}占比`} />
    </div>
  );
}

function ServiceLine({ label, state, detail, good }: { label: string; state: string; detail: string; good: boolean }) {
  return (
    <div className="flex items-center justify-between gap-4 border-b border-kumo-hairline pb-3 last:border-b-0 last:pb-0">
      <div><Text size="sm">{label}</Text><Text size="xs" variant="secondary">{detail}</Text></div>
      <Badge variant={good ? "primary" : "secondary"}>{state}</Badge>
    </div>
  );
}
