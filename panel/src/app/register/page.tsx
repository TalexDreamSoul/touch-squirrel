"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import {
  CloudArrowUpIcon,
  DotsThreeIcon,
  DownloadSimpleIcon,
  FileZipIcon,
  TrashIcon,
} from "@phosphor-icons/react";
import {
  Badge,
  Button,
  Dialog,
  DropdownMenu,
  Input,
  LayerCard,
  Select,
  Switch,
  Table,
  Text,
  Tooltip,
} from "@cloudflare/kumo";
import { Drawer } from "@cloudflare/kumo/primitives/drawer";
import { AdminShell } from "@/components/admin-shell";
import { PageHeader } from "@/components/page-header";
import { api, tokenQuery, type RunStatus } from "@/lib/api";

type RunInfo = {
  id: string;
  path: string;
  plugin: string;
  status: string;
  phase?: string;
  phase_detail?: string;
  error?: string;
  target?: number;
  done?: number;
  fail_count?: number;
  cpa_count: number;
  sso_files: number;
  imported_count: number;
  started_at?: string;
  updated_at?: string;
  mod_time?: string;
  duration_ms?: number;
  average_account_ms?: number;
  account_metric_count?: number;
};

type RegistrarOption = {
  id: string;
  name: string;
  enabled?: boolean;
  kind?: string[];
};

type RunFile = { path: string; size?: number };

type MetricEnvironment = {
  os?: string;
  arch?: string;
  hostname?: string;
  email_provider?: string;
  turnstile_provider?: string;
  resin_platform?: string;
  proxy_endpoint?: string;
  egress_ip?: string;
};

type MetricStage = {
  name: string;
  started_at?: string;
  ended_at?: string;
  duration_ms?: number;
  status?: string;
  error?: string;
};

type AccountMetric = {
  id: string;
  label: string;
  started_at?: string;
  completed_at?: string;
  duration_ms?: number;
  status: string;
  error?: string;
  stages?: MetricStage[];
  reported?: boolean;
};

type RunMetrics = {
  run_id: string;
  plugin: string;
  started_at: string;
  completed_at?: string;
  duration_ms?: number;
  status: string;
  error?: string;
  environment: MetricEnvironment;
  stages?: MetricStage[];
  accounts?: AccountMetric[];
};

type Extreme = {
  run_id: string;
  plugin: string;
  account_id: string;
  account: string;
  duration_ms: number;
  completed_at?: string;
  status: string;
  environment: MetricEnvironment;
};

type StageAggregate = {
  name: string;
  count: number;
  avg_ms: number;
  p50_ms: number;
  p95_ms: number;
  max_ms: number;
};

type MetricPoint = {
  run_id: string;
  plugin: string;
  time: string;
  run_duration_ms: number;
  average_account_ms: number;
  p95_account_ms: number;
  account_count: number;
  success_rate: number;
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
  fastest?: Extreme;
  slowest?: Extreme;
  stages: StageAggregate[];
  points: MetricPoint[];
};

const PAGE_SIZE = 10;
const FALLBACK_REGISTRARS: RegistrarOption[] = [
  { id: "xai-accounts", name: "xAI 账号（xai-accounts）", enabled: true },
  { id: "tavily-registrar", name: "Tavily 注册（tavily-registrar）", enabled: true },
];
const PHASES = [
  { key: "idle", label: "待命" },
  { key: "clearance", label: "清障" },
  { key: "register", label: "注册" },
  { key: "oauth", label: "OAuth" },
  { key: "probe", label: "探活" },
] as const;
const STAGE_LABELS: Record<string, string> = {
  clearance: "清障预热",
  initialize_clients: "初始化客户端",
  fetch_config: "获取注册配置",
  registration: "注册流水线",
  shutdown: "任务收尾",
  email_code: "邮箱与验证码",
  verify_email: "验证邮箱",
  signup: "提交注册",
  persist_sso: "保存 SSO",
  oauth: "OAuth 换取凭证",
  probe: "凭证探活",
  persist_cpa: "保存 CPA",
};

function phaseIndex(phase?: string): number {
  const index = PHASES.findIndex((item) => item.key === String(phase || "idle").toLowerCase());
  return index < 0 ? 0 : index;
}

function effectiveStatus(run: RunInfo): string {
  if (run.status === "completed" && (run.target || 0) > 0 && (run.done ?? run.cpa_count) < (run.target || 0)) {
    return "partial";
  }
  return run.status || "unknown";
}

function statusLabel(status: string): string {
  switch (status) {
    case "running":
      return "运行中";
    case "completed":
      return "已完成";
    case "partial":
      return "部分完成";
    case "cancelled":
      return "已终止";
    case "failed":
    case "error":
      return "意外终止";
    default:
      return "状态未知";
  }
}

function statusBadge(run: RunInfo) {
  const status = effectiveStatus(run);
  const danger = status === "failed" || status === "error";
  return (
    <Badge variant={status === "running" || status === "completed" ? "primary" : "secondary"} className={danger ? "text-kumo-danger" : undefined}>
      {statusLabel(status)}
    </Badge>
  );
}

function statusReason(run: RunInfo): string {
  const status = effectiveStatus(run);
  if (status === "failed" || status === "error") return run.error || run.phase_detail || "运行进程意外退出";
  if (status === "cancelled") return run.error || run.phase_detail || "用户手动终止";
  if (status === "partial") return run.phase_detail || `仅完成 ${run.done ?? run.cpa_count}/${run.target || 0}`;
  return "";
}

function formatDuration(value?: number): string {
  if (!value || value <= 0) return "—";
  if (value < 1000) return `${value}ms`;
  const seconds = value / 1000;
  if (seconds < 60) return `${seconds.toFixed(seconds < 10 ? 1 : 0)} 秒`;
  const minutes = Math.floor(seconds / 60);
  const rest = Math.round(seconds % 60);
  if (minutes < 60) return `${minutes} 分 ${rest} 秒`;
  const hours = Math.floor(minutes / 60);
  return `${hours} 小时 ${minutes % 60} 分`;
}

function formatDate(value?: string): string {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  }).format(date);
}

function elapsedForRun(run: RunInfo): number {
  if (run.duration_ms && run.duration_ms > 0) return run.duration_ms;
  if (run.started_at && effectiveStatus(run) === "running") {
    const started = new Date(run.started_at).getTime();
    if (Number.isFinite(started)) return Math.max(0, Date.now() - started);
  }
  if (run.started_at && run.updated_at) {
    const started = new Date(run.started_at).getTime();
    const ended = new Date(run.updated_at).getTime();
    if (Number.isFinite(started) && Number.isFinite(ended)) return Math.max(0, ended - started);
  }
  return 0;
}

function shortReason(reason: string): string {
  return reason.length > 28 ? `${reason.slice(0, 28)}…` : reason;
}

function downloadURL(id: string, kind: "cpa" | "all"): string {
  const token = tokenQuery();
  return `/api/runs/${encodeURIComponent(id)}/download?kind=${kind}${token ? `&${token.slice(1)}` : ""}`;
}

function EnvironmentLine({ environment }: { environment?: MetricEnvironment }) {
  if (!environment) return <Text size="xs" variant="secondary">未记录环境</Text>;
  const parts = [
    [environment.os && environment.arch ? `${environment.os}/${environment.arch}` : "", "运行环境"],
    [environment.hostname || "", "主机"],
    [environment.email_provider || "", "邮箱"],
    [environment.turnstile_provider || "", "打码"],
    [environment.resin_platform || "", "Resin"],
    [environment.proxy_endpoint || "", "代理"],
    [environment.egress_ip || "", "出口 IP"],
  ].filter(([value]) => value);
  if (parts.length === 0) return <Text size="xs" variant="secondary">未记录环境</Text>;
  return (
    <div className="flex flex-wrap gap-x-4 gap-y-1">
      {parts.map(([value, label]) => (
        <Text key={label} size="xs" variant="secondary">{label}：{value}</Text>
      ))}
    </div>
  );
}

function DurationChart({ points }: { points: MetricPoint[] }) {
  const usable = points.filter((point) => (point.average_account_ms || point.run_duration_ms) > 0);
  if (usable.length < 2) {
    return <Text variant="secondary">至少完成 2 个带耗时打点的任务后显示历史曲线</Text>;
  }
  const width = 760;
  const height = 220;
  const padding = 28;
  const values = usable.map((point) => point.average_account_ms || point.run_duration_ms);
  const max = Math.max(...values, 1);
  const min = Math.min(...values);
  const span = Math.max(max - min, 1);
  const coords = values.map((value, index) => ({
    x: padding + (index * (width - padding * 2)) / Math.max(1, values.length - 1),
    y: height - padding - ((value - min) / span) * (height - padding * 2),
    value,
    point: usable[index],
  }));
  const path = coords.map((point, index) => `${index === 0 ? "M" : "L"}${point.x.toFixed(1)},${point.y.toFixed(1)}`).join(" ");
  return (
    <div className="overflow-x-auto">
      <svg role="img" aria-label="平均账号注册耗时历史曲线" viewBox={`0 0 ${width} ${height}`} className="min-w-[42rem] text-kumo-brand">
        <title>平均账号注册耗时历史曲线</title>
        <line x1={padding} y1={height - padding} x2={width - padding} y2={height - padding} className="stroke-kumo-line" />
        <line x1={padding} y1={padding} x2={padding} y2={height - padding} className="stroke-kumo-line" />
        <path d={path} fill="none" stroke="currentColor" strokeWidth="2.5" />
        {coords.map(({ x, y, value, point }) => (
          <g key={point.run_id}>
            <circle cx={x} cy={y} r="4" fill="currentColor">
              <title>{`${point.run_id} · ${formatDuration(value)} · 成功率 ${(point.success_rate * 100).toFixed(0)}%`}</title>
            </circle>
          </g>
        ))}
        <text x={padding} y={16} className="fill-kumo-subtle text-xs">{formatDuration(max)}</text>
        <text x={padding} y={height - 6} className="fill-kumo-subtle text-xs">{formatDuration(min)}</text>
      </svg>
    </div>
  );
}

function Pager({ page, totalPages, total, onChange }: { page: number; totalPages: number; total: number; onChange: (page: number) => void }) {
  if (total <= 0) return null;
  const pages = Math.max(1, totalPages);
  return (
    <div className="flex flex-wrap items-center justify-between gap-2 border-t border-kumo-hairline p-4">
      <Text size="xs" variant="secondary">共 {total} · 第 {page}/{pages} 页</Text>
      <div className="flex gap-2">
        <Button size="sm" variant="secondary" disabled={page <= 1} onClick={() => onChange(page - 1)}>上一页</Button>
        <Button size="sm" variant="secondary" disabled={page >= pages} onClick={() => onChange(page + 1)}>下一页</Button>
      </div>
    </div>
  );
}

export default function RegisterPage() {
  const [target, setTarget] = useState("10");
  const [regType, setRegType] = useState("xai-accounts");
  const [registrars, setRegistrars] = useState<RegistrarOption[]>(FALLBACK_REGISTRARS);
  const [status, setStatus] = useState<RunStatus | null>(null);
  const [runs, setRuns] = useState<RunInfo[]>([]);
  const [runsTotal, setRunsTotal] = useState(0);
  const [runsTotalPages, setRunsTotalPages] = useState(0);
  const [runsPage, setRunsPage] = useState(1);
  const [poolTotal, setPoolTotal] = useState(0);
  const [poolUnsynced, setPoolUnsynced] = useState(0);
  const [autoImport, setAutoImport] = useState(true);
  const [autoSync, setAutoSync] = useState(false);
  const [msg, setMsg] = useState("");
  const [busy, setBusy] = useState(false);
  const [importingRunID, setImportingRunID] = useState("");
  const [uploadingRunID, setUploadingRunID] = useState("");
  const [deletingRunID, setDeletingRunID] = useState("");

  const [createOpen, setCreateOpen] = useState(false);
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [selected, setSelected] = useState<RunInfo | null>(null);
  const [drawerTab, setDrawerTab] = useState<"overview" | "logs" | "files" | "analytics">("overview");
  const [runFiles, setRunFiles] = useState<RunFile[]>([]);
  const [log, setLog] = useState("");
  const [logPath, setLogPath] = useState("");
  const [runMetrics, setRunMetrics] = useState<RunMetrics | null>(null);
  const [runMetricSummary, setRunMetricSummary] = useState<MetricsSummary | null>(null);
  const [metricsAvailable, setMetricsAvailable] = useState(false);
  const [metricsRange, setMetricsRange] = useState("7d");
  const [analytics, setAnalytics] = useState<MetricsSummary | null>(null);

  const running = String(status?.status || "").toLowerCase() === "running";

  const refreshCore = useCallback(async () => {
    try {
      const [statusResponse, configResponse, poolResponse, pluginResponse] = await Promise.all([
        api<{ status: RunStatus }>("/api/status"),
        api<{ config?: Record<string, unknown> }>("/api/config").catch(() => ({ config: {} })),
        api<{ total?: number; unsynced?: number }>("/api/pool/list?source=local&page=1&limit=1").catch(() => ({ total: 0, unsynced: 0 })),
        api<{ plugins?: RegistrarOption[] }>("/api/plugins").catch(() => ({ plugins: [] })),
      ]);
      setStatus(statusResponse.status);
      setPoolTotal(poolResponse.total || 0);
      setPoolUnsynced(poolResponse.unsynced || 0);
      const config: Record<string, unknown> = configResponse.config || {};
      if (typeof config.local_pool_auto_import === "boolean") setAutoImport(config.local_pool_auto_import);
      if (typeof config.local_pool_auto_sync === "boolean") setAutoSync(config.local_pool_auto_sync);
      const available = (pluginResponse.plugins || []).filter((plugin) => Array.isArray(plugin.kind) && plugin.kind.map(String).includes("registrar") && plugin.enabled !== false);
      if (available.length > 0) {
        setRegistrars(available);
        setRegType((current) => (available.some((plugin) => plugin.id === current) ? current : available[0].id));
      }
    } catch (error) {
      setMsg(error instanceof Error ? error.message : "刷新失败");
    }
  }, []);

  const refreshRuns = useCallback(async (page: number) => {
    try {
      const response = await api<{ runs?: RunInfo[]; total?: number; total_pages?: number }>(`/api/runs?page=${page}&limit=${PAGE_SIZE}`);
      setRuns(response.runs || []);
      setRunsTotal(response.total || 0);
      setRunsTotalPages(response.total_pages || 0);
    } catch (error) {
      setMsg(error instanceof Error ? error.message : "加载任务失败");
    }
  }, []);

  const refreshAnalytics = useCallback(async (range: string) => {
    try {
      const response = await api<{ summary: MetricsSummary }>(`/api/register-metrics?range=${encodeURIComponent(range)}`);
      setAnalytics(response.summary);
    } catch (error) {
      setMsg(error instanceof Error ? error.message : "加载耗时分析失败");
    }
  }, []);

  const loadRunFiles = useCallback(async (id: string) => {
    try {
      const response = await api<{ files?: RunFile[] }>(`/api/runs/${encodeURIComponent(id)}/files`);
      setRunFiles(response.files || []);
    } catch {
      setRunFiles([]);
    }
  }, []);

  const loadRunLog = useCallback(async (id: string) => {
    try {
      const response = await api<{ log?: string; path?: string }>(`/api/runs/${encodeURIComponent(id)}/log?tail=500`);
      setLog(response.log || "");
      setLogPath(response.path || "");
    } catch (error) {
      setLog(error instanceof Error ? `日志加载失败：${error.message}` : "日志加载失败");
      setLogPath("");
    }
  }, []);

  const loadRunMetrics = useCallback(async (id: string) => {
    try {
      const response = await api<{ metrics_available?: boolean; metrics?: RunMetrics; summary?: MetricsSummary }>(`/api/runs/${encodeURIComponent(id)}/metrics`);
      setMetricsAvailable(!!response.metrics_available);
      setRunMetrics(response.metrics || null);
      setRunMetricSummary(response.summary || null);
    } catch {
      setMetricsAvailable(false);
      setRunMetrics(null);
      setRunMetricSummary(null);
    }
  }, []);

  useEffect(() => {
    void refreshCore();
    void refreshRuns(1);
    const timer = setInterval(() => {
      void refreshCore();
      void refreshRuns(runsPage);
    }, 3000);
    return () => clearInterval(timer);
  }, [refreshCore, refreshRuns, runsPage]);

  useEffect(() => {
    void refreshRuns(runsPage);
  }, [runsPage, refreshRuns]);

  useEffect(() => {
    void refreshAnalytics(metricsRange);
  }, [metricsRange, refreshAnalytics]);

  useEffect(() => {
    if (!drawerOpen || !selected || drawerTab !== "logs" || effectiveStatus(selected) !== "running") return;
    const timer = setInterval(() => void loadRunLog(selected.id), 2000);
    return () => clearInterval(timer);
  }, [drawerOpen, drawerTab, loadRunLog, selected]);

  const tableRuns = useMemo(() => {
    const list = [...runs];
    const currentID = status?.run_id;
    if (currentID && running && !list.some((run) => run.id === currentID)) {
      list.unshift({
        id: currentID,
        path: status.output_dir || "",
        plugin: status.plugin || "xai-accounts",
        status: status.status || "running",
        phase: status.phase,
        phase_detail: status.phase_detail,
        error: status.error,
        target: status.target,
        done: status.done,
        fail_count: status.fail_count,
        cpa_count: status.done || 0,
        sso_files: status.sso_count || 0,
        imported_count: 0,
        started_at: status.started_at,
        updated_at: status.updated_at,
        mod_time: status.updated_at,
      });
    }
    return list.map((run) => {
      if (!currentID || run.id !== currentID) return run;
      return {
        ...run,
        plugin: status?.plugin || run.plugin,
        status: status?.status || run.status,
        phase: status?.phase || run.phase,
        phase_detail: status?.phase_detail || run.phase_detail,
        error: status?.error || run.error,
        target: status?.target ?? run.target,
        done: status?.done ?? run.done,
        fail_count: status?.fail_count ?? run.fail_count,
        cpa_count: status?.done ?? run.cpa_count,
        sso_files: status?.sso_count ?? run.sso_files,
        started_at: status?.started_at || run.started_at,
        updated_at: status?.updated_at || run.updated_at,
      };
    });
  }, [runs, running, status]);

  const selectedLive = !!selected && selected.id === status?.run_id;
  const selectedView = selected && selectedLive ? tableRuns.find((run) => run.id === selected.id) || selected : selected;
  const selectedPhaseIndex = phaseIndex(selectedView?.phase);
  const pluginNames = useMemo(() => new Map(registrars.map((plugin) => [plugin.id, plugin.name || plugin.id])), [registrars]);

  function openTask(run: RunInfo, tab: typeof drawerTab = "overview") {
    setSelected(run);
    setDrawerTab(tab);
    setDrawerOpen(true);
    setRunFiles([]);
    setLog("");
    setLogPath("");
    setRunMetrics(null);
    setRunMetricSummary(null);
    setMetricsAvailable(false);
    void loadRunFiles(run.id);
    void loadRunMetrics(run.id);
    if (tab === "logs") void loadRunLog(run.id);
  }

  function openCurrentTask() {
    const current = tableRuns.find((run) => run.id === status?.run_id);
    if (!current) {
      setMsg("当前没有活动任务");
      return;
    }
    openTask(current);
  }

  async function start() {
    setBusy(true);
    setMsg("");
    try {
      const count = Math.max(1, Math.min(10000, Number.parseInt(target, 10) || 10));
      const response = await api<{ run_id?: string; plugin?: string }>("/api/start", {
        method: "POST",
        body: JSON.stringify({ target: count, plugin: regType }),
      });
      setMsg(`已创建任务 ${response.run_id || ""} · ${response.plugin || regType} · target=${count}`);
      setCreateOpen(false);
      setRunsPage(1);
      await refreshCore();
      await refreshRuns(1);
    } catch (error) {
      setMsg(error instanceof Error ? error.message : "启动失败");
    } finally {
      setBusy(false);
    }
  }

  async function stop() {
    setBusy(true);
    try {
      await api("/api/stop", { method: "POST", body: "{}" });
      setMsg("任务已终止");
      await refreshCore();
      await refreshRuns(runsPage);
    } catch (error) {
      setMsg(error instanceof Error ? error.message : "停止失败");
    } finally {
      setBusy(false);
    }
  }

  async function savePoolFlags(nextImport: boolean, nextSync: boolean) {
    setAutoImport(nextImport);
    setAutoSync(nextSync);
    try {
      await api("/api/config", { method: "PUT", body: JSON.stringify({ local_pool_auto_import: nextImport, local_pool_auto_sync: nextSync }) });
    } catch (error) {
      setMsg(error instanceof Error ? error.message : "保存自动入库设置失败");
    }
  }

  async function importPool(run: RunInfo) {
    setImportingRunID(run.id);
    setMsg("");
    try {
      const response = await api<{ added?: number; run_id?: string }>("/api/local-pool/import", {
        method: "POST",
        body: JSON.stringify({ run_id: run.id }),
      });
      setMsg(`已入库 ${response.added ?? 0} 个（run ${response.run_id || run.id}）`);
      await refreshCore();
      await refreshRuns(runsPage);
    } catch (error) {
      setMsg(error instanceof Error ? error.message : "入库失败");
    } finally {
      setImportingRunID("");
    }
  }

  async function uploadRun(run: RunInfo) {
    setUploadingRunID(run.id);
    setMsg("");
    try {
      const response = await api<{ job?: { id?: string } }>("/api/transfer/prepare", {
        method: "POST",
        body: JSON.stringify({ folderPath: `${run.path}/CPA` }),
      });
      const jobID = response.job?.id || "";
      if (!jobID) throw new Error("上传任务未返回 job id");
      await api(`/api/transfer/jobs/${encodeURIComponent(jobID)}/start`, { method: "POST", body: "{}" });
      setMsg(`任务 ${run.id} 的 CPA 上传已启动 · job ${jobID}`);
    } catch (error) {
      setMsg(error instanceof Error ? error.message : "上传失败");
    } finally {
      setUploadingRunID("");
    }
  }

  async function deleteRun(run: RunInfo) {
    if (!window.confirm(`确认删除任务 ${run.id} 的产物与日志？已入库账号不会删除。`)) return;
    setDeletingRunID(run.id);
    setMsg("");
    try {
      await api(`/api/runs/${encodeURIComponent(run.id)}`, { method: "DELETE" });
      if (selected?.id === run.id) setDrawerOpen(false);
      setMsg(`任务 ${run.id} 已删除`);
      await refreshCore();
      await refreshRuns(runsPage);
      await refreshAnalytics(metricsRange);
    } catch (error) {
      setMsg(error instanceof Error ? error.message : "删除失败");
    } finally {
      setDeletingRunID("");
    }
  }

  const sortedAccounts = useMemo(() => [...(runMetrics?.accounts || [])].sort((a, b) => (b.duration_ms || 0) - (a.duration_ms || 0)), [runMetrics]);

  return (
    <AdminShell>
      <PageHeader
        title="注册任务"
        description="任务状态 · 入库 · 日志 · 耗时分析"
        actions={
          <>
            {running ? <Button size="sm" variant="secondary" loading={busy} onClick={() => void stop()}>停止当前</Button> : null}
            <Button size="sm" variant="secondary" loading={busy} onClick={() => { void refreshCore(); void refreshRuns(runsPage); void refreshAnalytics(metricsRange); }}>刷新</Button>
            <Button size="sm" loading={busy} disabled={running} onClick={() => setCreateOpen(true)}>新建任务</Button>
          </>
        }
      />

      {msg ? <div className="mb-3 rounded-md bg-kumo-contrast/5 px-3 py-2"><Text>{msg}</Text></div> : null}

      <div className="mb-4 grid gap-3 sm:grid-cols-4">
        <LayerCard>
          <LayerCard.Secondary>当前状态</LayerCard.Secondary>
          <LayerCard.Primary className="p-4">
            <div className="flex flex-wrap items-center gap-2">
              {statusBadge({ id: status?.run_id || "", path: status?.output_dir || "", plugin: status?.plugin || "", status: status?.status || "unknown", phase_detail: status?.phase_detail, error: status?.error, target: status?.target, done: status?.done, cpa_count: status?.done || 0, sso_files: status?.sso_count || 0, imported_count: 0 })}
              {status?.run_id ? <Button size="sm" variant="secondary" onClick={openCurrentTask}>{status.run_id}</Button> : <Text size="sm" variant="secondary">无活动任务</Text>}
            </div>
          </LayerCard.Primary>
        </LayerCard>
        <LayerCard>
          <LayerCard.Secondary>进度</LayerCard.Secondary>
          <LayerCard.Primary className="p-4"><Text size="sm">{status?.done ?? 0}/{status?.target ?? 0}{status?.fail_count ? ` · 失败 ${status.fail_count}` : ""}</Text></LayerCard.Primary>
        </LayerCard>
        <LayerCard>
          <LayerCard.Secondary>本地号池</LayerCard.Secondary>
          <LayerCard.Primary className="p-4"><Text size="sm">{poolTotal} 个{poolUnsynced ? ` · 未同步 ${poolUnsynced}` : ""}</Text></LayerCard.Primary>
        </LayerCard>
        <LayerCard>
          <LayerCard.Secondary>自动策略</LayerCard.Secondary>
          <LayerCard.Primary className="p-4">
            <div className="flex flex-col gap-2">
              <Switch label="完成后自动入库" checked={autoImport} onCheckedChange={(value) => void savePoolFlags(!!value, autoSync)} />
              <Switch label="入库后同步云端" checked={autoSync} onCheckedChange={(value) => void savePoolFlags(autoImport, !!value)} />
            </div>
          </LayerCard.Primary>
        </LayerCard>
      </div>

      <LayerCard className="mb-4">
        <LayerCard.Secondary>
          <div className="flex flex-wrap items-center justify-between gap-3">
            <span>注册耗时分析</span>
            <Select label="时间范围" value={metricsRange} onValueChange={(value) => value && setMetricsRange(value)}>
              <Select.Option value="24h">最近 24 小时</Select.Option>
              <Select.Option value="7d">最近 7 天</Select.Option>
              <Select.Option value="30d">最近 30 天</Select.Option>
              <Select.Option value="all">全部</Select.Option>
            </Select>
          </div>
        </LayerCard.Secondary>
        <LayerCard.Primary className="p-4">
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-6">
            {[
              ["平均耗时", formatDuration(analytics?.average_account_ms)],
              ["P50", formatDuration(analytics?.p50_account_ms)],
              ["P95", formatDuration(analytics?.p95_account_ms)],
              ["成功率", analytics?.account_count ? `${(analytics.success_rate * 100).toFixed(1)}%` : "—"],
              ["吞吐", analytics?.throughput_per_hour ? `${analytics.throughput_per_hour.toFixed(1)}/h` : "—"],
              ["样本", `${analytics?.run_count || 0} 任务 · ${analytics?.account_count || 0} 账号`],
            ].map(([label, value]) => (
              <div key={label} className="border-b border-kumo-hairline pb-2 lg:border-b-0 lg:border-r lg:pr-3 lg:last:border-r-0">
                <Text size="xs" variant="secondary">{label}</Text>
                <Text size="sm">{value}</Text>
              </div>
            ))}
          </div>
          <div className="mt-4"><DurationChart points={analytics?.points || []} /></div>
          <div className="mt-4 grid gap-3 lg:grid-cols-2">
            {([['最快账号', analytics?.fastest], ['最慢账号', analytics?.slowest]] as const).map(([label, extreme]) => (
              <div key={label} className="border-t border-kumo-hairline pt-3">
                <Text size="sm">{label} · {extreme ? formatDuration(extreme.duration_ms) : "—"}</Text>
                {extreme ? (
                  <>
                    <Text size="xs" variant="secondary">{extreme.account} · {extreme.plugin} · {formatDate(extreme.completed_at)}</Text>
                    <EnvironmentLine environment={extreme.environment} />
                  </>
                ) : <Text size="xs" variant="secondary">暂无账号级打点</Text>}
              </div>
            ))}
          </div>
          {(analytics?.stages || []).length > 0 ? (
            <div className="mt-4 overflow-x-auto border-t border-kumo-hairline pt-3">
              <Table>
                <Table.Header><Table.Row><Table.Head>最慢阶段</Table.Head><Table.Head>平均</Table.Head><Table.Head>P95</Table.Head><Table.Head>最久</Table.Head><Table.Head>样本</Table.Head></Table.Row></Table.Header>
                <Table.Body>{analytics!.stages.slice(0, 6).map((stage) => <Table.Row key={stage.name}><Table.Cell>{STAGE_LABELS[stage.name] || stage.name}</Table.Cell><Table.Cell>{formatDuration(stage.avg_ms)}</Table.Cell><Table.Cell>{formatDuration(stage.p95_ms)}</Table.Cell><Table.Cell>{formatDuration(stage.max_ms)}</Table.Cell><Table.Cell>{stage.count}</Table.Cell></Table.Row>)}</Table.Body>
              </Table>
            </div>
          ) : null}
        </LayerCard.Primary>
      </LayerCard>

      <LayerCard>
        <LayerCard.Secondary>任务列表 {runsTotal ? `(${runsTotal})` : ""}</LayerCard.Secondary>
        <LayerCard.Primary className="p-0">
          {tableRuns.length === 0 ? <div className="p-4"><Text variant="secondary">暂无任务 — 点「新建任务」选择注册插件并启动</Text></div> : (
            <div className="overflow-x-auto">
              <Table>
                <Table.Header><Table.Row><Table.Head>任务 ID</Table.Head><Table.Head>注册插件</Table.Head><Table.Head>状态</Table.Head><Table.Head>阶段</Table.Head><Table.Head>成果</Table.Head><Table.Head>耗时 / 时间</Table.Head><Table.Head>操作</Table.Head></Table.Row></Table.Header>
                <Table.Body>
                  {tableRuns.map((run) => {
                    const reason = statusReason(run);
                    const isCurrent = run.id === status?.run_id;
                    const imported = run.imported_count > 0;
                    const canImport = !imported && run.cpa_count > 0 && importingRunID !== run.id;
                    return (
                      <Table.Row key={run.id}>
                        <Table.Cell><button type="button" className="text-left" onClick={() => openTask(run)}><Text size="sm">{run.id}</Text>{isCurrent ? <Text size="xs" variant="secondary">当前任务</Text> : null}</button></Table.Cell>
                        <Table.Cell><Text size="sm">{pluginNames.get(run.plugin) || run.plugin || "旧任务未记录"}</Text></Table.Cell>
                        <Table.Cell>
                          <div className="flex max-w-52 flex-col items-start gap-1">
                            {statusBadge(run)}
                            {reason ? <Tooltip content={reason} render={<span tabIndex={0} className="max-w-52 cursor-help truncate text-xs text-kumo-subtle" />}>{shortReason(reason)}</Tooltip> : null}
                          </div>
                        </Table.Cell>
                        <Table.Cell><Text size="sm">{run.phase || "—"}</Text>{run.phase_detail ? <Text size="xs" variant="secondary">{shortReason(run.phase_detail)}</Text> : null}</Table.Cell>
                        <Table.Cell><Text size="sm">CPA {run.cpa_count} · SSO {run.sso_files}</Text><Text size="xs" variant="secondary">{imported ? `已入库 ${run.imported_count}` : "未入库"}</Text></Table.Cell>
                        <Table.Cell><Text size="sm">{formatDuration(elapsedForRun(run))}</Text><Text size="xs" variant="secondary">平均账号 {formatDuration(run.average_account_ms)}</Text><Text size="xs" variant="secondary">{formatDate(run.started_at || run.mod_time)}</Text></Table.Cell>
                        <Table.Cell>
                          <div className="flex flex-wrap gap-2">
                            <Button size="sm" variant="secondary" onClick={() => openTask(run)}>详情</Button>
                            <Button size="sm" variant="secondary" onClick={() => openTask(run, "logs")}>日志</Button>
                            <Button size="sm" variant="secondary" loading={importingRunID === run.id} disabled={!canImport} onClick={() => void importPool(run)}>{imported ? "已入库" : "入库"}</Button>
                            <DropdownMenu>
                              <DropdownMenu.Trigger render={<Button size="sm" shape="square" variant="secondary" icon={DotsThreeIcon} aria-label={`任务 ${run.id} 更多操作`} />} />
                              <DropdownMenu.Content>
                                <DropdownMenu.Item icon={DownloadSimpleIcon} onClick={() => window.open(downloadURL(run.id, "cpa"), "_blank")}>下载 CPA</DropdownMenu.Item>
                                <DropdownMenu.Item icon={FileZipIcon} onClick={() => window.open(downloadURL(run.id, "all"), "_blank")}>打包全部产物</DropdownMenu.Item>
                                <DropdownMenu.Item icon={CloudArrowUpIcon} disabled={!run.path || run.cpa_count <= 0 || uploadingRunID === run.id} onClick={() => void uploadRun(run)}>{uploadingRunID === run.id ? "上传中" : "上传 CPA"}</DropdownMenu.Item>
                                <DropdownMenu.Separator />
                                <DropdownMenu.Item icon={TrashIcon} variant="danger" disabled={effectiveStatus(run) === "running" || deletingRunID === run.id} onClick={() => void deleteRun(run)}>删除任务</DropdownMenu.Item>
                              </DropdownMenu.Content>
                            </DropdownMenu>
                          </div>
                        </Table.Cell>
                      </Table.Row>
                    );
                  })}
                </Table.Body>
              </Table>
            </div>
          )}
          <Pager page={runsPage} totalPages={runsTotalPages} total={runsTotal} onChange={setRunsPage} />
        </LayerCard.Primary>
      </LayerCard>

      <Dialog.Root open={createOpen} onOpenChange={(open) => setCreateOpen(!!open)}>
        <Dialog size="base" className="flex max-h-[min(90vh,40rem)] flex-col p-6">
          <div className="mb-4"><Dialog.Title className="text-xl font-semibold">新建注册任务</Dialog.Title><Dialog.Description className="mt-1 text-kumo-subtle">选择 registrar 插件与目标数量</Dialog.Description></div>
          <div className="min-h-0 flex-1 overflow-y-auto"><div className="flex flex-col gap-3">
            <Select label="注册插件" value={regType} onValueChange={(value) => value && setRegType(value)}>{registrars.map((plugin) => <Select.Option key={plugin.id} value={plugin.id}>{plugin.name || plugin.id}</Select.Option>)}</Select>
            <Input label="目标数量" value={target} onChange={(event) => setTarget(event.target.value)} />
            {running ? <Text size="sm" variant="secondary">当前已有任务在跑，请先停止再新建</Text> : null}
          </div></div>
          <div className="mt-6 flex justify-end gap-2"><Dialog.Close render={(props) => <Button {...props} size="sm" variant="secondary">取消</Button>} /><Button size="sm" loading={busy} disabled={running} onClick={() => void start()}>启动</Button></div>
        </Dialog>
      </Dialog.Root>

      <Drawer.Root open={drawerOpen} onOpenChange={(open) => { setDrawerOpen(open); if (!open) setSelected(null); }} swipeDirection="right">
        <Drawer.Portal>
          <Drawer.Backdrop className="fixed inset-0 z-40 bg-kumo-contrast/30 opacity-[calc(1-var(--drawer-swipe-progress))] transition-opacity duration-200 data-starting-style:opacity-0 data-ending-style:opacity-0" />
          <Drawer.Viewport className="fixed inset-0 z-50 flex justify-end">
            <Drawer.Popup className="h-dvh w-full max-w-3xl bg-kumo-base text-kumo-default ring-1 ring-kumo-line outline-none [transform:translateX(var(--drawer-swipe-movement-x))] transition-transform duration-200 ease-out data-starting-style:translate-x-full data-ending-style:translate-x-full">
              <Drawer.Content className="flex h-full min-h-0 flex-col">
                <div className="flex items-start justify-between gap-4 border-b border-kumo-hairline p-5">
                  <div className="min-w-0"><Drawer.Title className="truncate text-xl font-semibold">任务 {selectedView?.id || "—"}</Drawer.Title><Drawer.Description className="mt-1 text-sm text-kumo-subtle">{selectedView ? `${statusLabel(effectiveStatus(selectedView))} · ${pluginNames.get(selectedView.plugin) || selectedView.plugin || "插件未记录"}` : "任务详情"}</Drawer.Description></div>
                  <div className="flex flex-wrap justify-end gap-2">{selectedView && effectiveStatus(selectedView) === "running" ? <Button size="sm" variant="secondary" loading={busy} onClick={() => void stop()}>停止</Button> : null}{selectedView ? <Button size="sm" variant="secondary" loading={importingRunID === selectedView.id} disabled={selectedView.imported_count > 0 || selectedView.cpa_count <= 0} onClick={() => void importPool(selectedView)}>{selectedView.imported_count > 0 ? `已入库 ${selectedView.imported_count}` : "入库"}</Button> : null}<Drawer.Close render={<Button size="sm" variant="secondary" />}>关闭</Drawer.Close></div>
                </div>
                <div className="flex flex-wrap gap-2 border-b border-kumo-hairline px-5 py-3">{(["overview", "logs", "files", "analytics"] as const).map((tab) => <Button key={tab} size="sm" variant={drawerTab === tab ? "primary" : "secondary"} onClick={() => { setDrawerTab(tab); if (tab === "logs" && selectedView) void loadRunLog(selectedView.id); }}>{tab === "overview" ? "概览 / 阶段" : tab === "logs" ? "日志" : tab === "files" ? "产物" : "耗时分析"}</Button>)}</div>
                <div className="min-h-0 flex-1 overflow-y-auto p-5">
                  {drawerTab === "overview" && selectedView ? (
                    <div className="flex flex-col gap-5">
                      <div><Text size="sm">流水线阶段</Text><div className="mt-2 flex flex-wrap gap-2">{PHASES.map((phase, index) => <Badge key={phase.key} variant={index <= selectedPhaseIndex && selectedLive ? "primary" : "secondary"}>{index + 1}. {phase.label}</Badge>)}</div><Text size="xs" variant="secondary">{selectedView.phase_detail || "—"}</Text></div>
                      <div className="grid gap-3 sm:grid-cols-2"><LayerCard><LayerCard.Secondary>结果</LayerCard.Secondary><LayerCard.Primary className="p-4"><Text size="sm">插件 {pluginNames.get(selectedView.plugin) || selectedView.plugin || "未记录"}<br />CPA {selectedView.cpa_count} · SSO {selectedView.sso_files}<br />入库 {selectedView.imported_count}<br />失败 {selectedView.fail_count || 0}</Text></LayerCard.Primary></LayerCard><LayerCard><LayerCard.Secondary>时间</LayerCard.Secondary><LayerCard.Primary className="p-4"><Text size="sm">开始 {formatDate(selectedView.started_at)}<br />更新 {formatDate(selectedView.updated_at || selectedView.mod_time)}<br />总耗时 {formatDuration(elapsedForRun(selectedView))}<br />账号平均 {formatDuration(selectedView.average_account_ms)}</Text></LayerCard.Primary></LayerCard></div>
                      {statusReason(selectedView) ? <LayerCard><LayerCard.Secondary>终止原因</LayerCard.Secondary><LayerCard.Primary className="p-4"><Text variant={effectiveStatus(selectedView) === "failed" ? "error" : "secondary"}>{statusReason(selectedView)}</Text></LayerCard.Primary></LayerCard> : null}
                    </div>
                  ) : null}
                  {drawerTab === "logs" ? <div className="flex flex-col gap-3"><div className="flex flex-wrap items-center gap-2"><Button size="sm" variant="secondary" onClick={() => selectedView && void loadRunLog(selectedView.id)}>刷新日志</Button><Text size="xs" variant="secondary">{logPath || "对应任务日志"}{selectedView && effectiveStatus(selectedView) === "running" ? " · 自动刷新" : ""}</Text></div><pre className="max-h-[70vh] overflow-auto rounded-md bg-kumo-contrast/5 p-3 text-xs whitespace-pre-wrap">{log || "（暂无日志）"}</pre></div> : null}
                  {drawerTab === "files" ? <div className="flex flex-col gap-3">{selectedView ? <div className="flex gap-2"><Button size="sm" variant="secondary" onClick={() => window.open(downloadURL(selectedView.id, "cpa"), "_blank")}>下载 CPA</Button><Button size="sm" variant="secondary" onClick={() => window.open(downloadURL(selectedView.id, "all"), "_blank")}>打包全部产物</Button></div> : null}{runFiles.length === 0 ? <Text variant="secondary">暂无产物文件</Text> : <div className="flex flex-col gap-1">{runFiles.map((file) => <Text key={file.path} size="sm">{file.path}{typeof file.size === "number" ? ` · ${file.size}B` : ""}</Text>)}</div>}</div> : null}
                  {drawerTab === "analytics" ? (
                    !metricsAvailable || !runMetrics ? <Text variant="secondary">此任务没有账号级耗时打点。旧任务或未上报 duration/stages 的 Bridge 插件会明确保持为空，不推测耗时。</Text> : (
                      <div className="flex flex-col gap-5">
                        <div className="grid gap-3 sm:grid-cols-4">{[["总耗时", formatDuration(runMetrics.duration_ms)], ["账号平均", formatDuration(runMetricSummary?.average_account_ms)], ["P95", formatDuration(runMetricSummary?.p95_account_ms)], ["成功率", runMetricSummary?.account_count ? `${(runMetricSummary.success_rate * 100).toFixed(1)}%` : "—"]].map(([label, value]) => <div key={label} className="border-b border-kumo-hairline pb-2"><Text size="xs" variant="secondary">{label}</Text><Text size="sm">{value}</Text></div>)}</div>
                        <EnvironmentLine environment={runMetrics.environment} />
                        {(runMetrics.stages || []).length > 0 ? <div><Text size="sm">运行阶段</Text><div className="mt-2 overflow-x-auto"><Table><Table.Header><Table.Row><Table.Head>阶段</Table.Head><Table.Head>耗时</Table.Head><Table.Head>状态</Table.Head></Table.Row></Table.Header><Table.Body>{runMetrics.stages!.map((stage, index) => <Table.Row key={`${stage.name}-${index}`}><Table.Cell>{STAGE_LABELS[stage.name] || stage.name}</Table.Cell><Table.Cell>{formatDuration(stage.duration_ms)}</Table.Cell><Table.Cell>{stage.status || "—"}</Table.Cell></Table.Row>)}</Table.Body></Table></div></div> : null}
                        {runMetricSummary?.fastest || runMetricSummary?.slowest ? <div className="grid gap-3 sm:grid-cols-2">{([['最快', runMetricSummary?.fastest], ['最慢', runMetricSummary?.slowest]] as const).map(([label, item]) => <LayerCard key={label}><LayerCard.Secondary>{label}</LayerCard.Secondary><LayerCard.Primary className="p-4"><Text size="sm">{item?.account || "—"} · {formatDuration(item?.duration_ms)}</Text><Text size="xs" variant="secondary">{formatDate(item?.completed_at)}</Text></LayerCard.Primary></LayerCard>)}</div> : null}
                        {sortedAccounts.length > 0 ? <div><Text size="sm">账号完整注册耗时</Text><div className="mt-2 overflow-x-auto"><Table><Table.Header><Table.Row><Table.Head>账号</Table.Head><Table.Head>状态</Table.Head><Table.Head>总耗时</Table.Head><Table.Head>最慢阶段</Table.Head><Table.Head>完成时间</Table.Head></Table.Row></Table.Header><Table.Body>{sortedAccounts.map((account) => { const slowestStage = [...(account.stages || [])].sort((a, b) => (b.duration_ms || 0) - (a.duration_ms || 0))[0]; return <Table.Row key={account.id}><Table.Cell>{account.label}</Table.Cell><Table.Cell>{account.status}</Table.Cell><Table.Cell>{formatDuration(account.duration_ms)}</Table.Cell><Table.Cell>{slowestStage ? `${STAGE_LABELS[slowestStage.name] || slowestStage.name} · ${formatDuration(slowestStage.duration_ms)}` : account.reported ? "插件仅上报总耗时" : "—"}</Table.Cell><Table.Cell>{formatDate(account.completed_at)}</Table.Cell></Table.Row>; })}</Table.Body></Table></div></div> : <Text variant="secondary">插件未上报账号级耗时</Text>}
                      </div>
                    )
                  ) : null}
                </div>
              </Drawer.Content>
            </Drawer.Popup>
          </Drawer.Viewport>
        </Drawer.Portal>
      </Drawer.Root>
    </AdminShell>
  );
}
