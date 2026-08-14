"use client";

import { useCallback, useEffect, useState } from "react";
import {
  ArrowsClockwiseIcon,
  PlayIcon,
  ShieldCheckIcon,
} from "@phosphor-icons/react";
import {
  Badge,
  Button,
  Input,
  LayerCard,
  Select,
  Switch,
  Table,
  Text,
} from "@cloudflare/kumo";
import { AdminShell } from "@/components/admin-shell";
import { PageHeader } from "@/components/page-header";
import { api } from "@/lib/api";

type Verdict = "normal" | "degraded" | "exhausted" | "error";

type DegradeOverview = {
  pool_total: number;
  checked: number;
  normal: number;
  degraded: number;
  exhausted: number;
  errored: number;
  isolated: number;
  unchecked: number;
};

type ScanRecord = {
  time: string;
  checked: number;
  normal: number;
  degraded: number;
  exhausted: number;
  errored: number;
  pool_total: number;
  paced_ms: number;
  duration_ms: number;
  error?: string;
};

type ScanStatus = {
  running: boolean;
  note?: string;
  last_run?: string;
  last?: ScanRecord;
};

type ExitStat = {
  exit: string;
  accounts: number;
  requests: number;
  degraded: number;
  window_min: number;
  cap: number;
  over: boolean;
  last_at: string;
};

type DegradeRecord = {
  name: string;
  email?: string;
  verdict: Verdict;
  model?: string;
  reasoning_tokens: number;
  answer_ok: boolean;
  exit?: string;
  detail?: string;
  checked_at: string;
  first_degraded_at?: string;
  isolated: boolean;
  isolated_at?: string;
};

type DegradeSettings = {
  model: string;
  sample: number;
  recheck_min: number;
  exit_window_min: number;
  exit_account_cap: number;
  exit: string;
};

type OverviewResponse = {
  ok: boolean;
  overview: DegradeOverview;
  status: ScanStatus;
  exits: ExitStat[];
  history: ScanRecord[];
  settings: DegradeSettings;
};

const VERDICT_OPTIONS = [
  { value: "all", label: "全部结论" },
  { value: "normal", label: "正常" },
  { value: "degraded", label: "疑似降智" },
  { value: "exhausted", label: "额度耗尽" },
  { value: "error", label: "检测错误" },
];

function verdictLabel(verdict: string) {
  switch (verdict) {
    case "normal":
      return "正常";
    case "degraded":
      return "疑似降智";
    case "exhausted":
      return "额度耗尽";
    default:
      return "检测错误";
  }
}

function formatDate(value?: string) {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString("zh-CN", { hour12: false });
}

function formatDuration(milliseconds?: number) {
  if (!milliseconds || milliseconds < 1) return "—";
  if (milliseconds < 1000) return `${milliseconds} ms`;
  const seconds = Math.round(milliseconds / 1000);
  if (seconds < 60) return `${seconds} 秒`;
  return `${Math.floor(seconds / 60)} 分 ${seconds % 60} 秒`;
}

export default function DegradePage() {
  const [data, setData] = useState<OverviewResponse | null>(null);
  const [records, setRecords] = useState<DegradeRecord[]>([]);
  const [total, setTotal] = useState(0);
  const [verdict, setVerdict] = useState("all");
  const [query, setQuery] = useState("");
  const [sample, setSample] = useState(0);
  const [recheck, setRecheck] = useState(false);
  const [busy, setBusy] = useState("");
  const [msg, setMsg] = useState("");

  const load = useCallback(async (quiet = false) => {
    if (!quiet) setMsg("");
    try {
      const params = new URLSearchParams({ limit: "500" });
      if (verdict !== "all") params.set("verdict", verdict);
      if (query.trim()) params.set("q", query.trim());
      const [overviewResponse, accountResponse] = await Promise.all([
        api<OverviewResponse>("/api/degrade/overview"),
        api<{ ok: boolean; items?: DegradeRecord[]; total?: number }>(
          `/api/degrade/accounts?${params.toString()}`,
        ),
      ]);
      setData(overviewResponse);
      setRecords(accountResponse.items || []);
      setTotal(accountResponse.total || 0);
      setSample((current) => current || overviewResponse.settings.sample || 5);
    } catch (error) {
      if (!quiet) setMsg(error instanceof Error ? error.message : "加载失败");
    }
  }, [query, verdict]);

  useEffect(() => {
    void load();
  }, [load]);

  useEffect(() => {
    if (!data?.status.running) return;
    const timer = window.setInterval(() => void load(true), 3000);
    return () => window.clearInterval(timer);
  }, [data?.status.running, load]);

  async function startScan() {
    setBusy("scan");
    setMsg("");
    try {
      await api("/api/degrade/scan", {
        method: "POST",
        body: JSON.stringify({ sample: Math.max(1, sample), recheck }),
      });
      setMsg("抽样扫描已启动");
      await load(true);
    } catch (error) {
      setMsg(error instanceof Error ? error.message : "启动扫描失败");
    } finally {
      setBusy("");
    }
  }

  async function setIsolated(record: DegradeRecord, isolated: boolean) {
    setBusy(`isolate:${record.name}`);
    setMsg("");
    try {
      await api("/api/degrade/isolate", {
        method: "POST",
        body: JSON.stringify({ name: record.name, isolated }),
      });
      setMsg(isolated ? "账号已加入本地隔离" : "账号已解除本地隔离");
      await load(true);
    } catch (error) {
      setMsg(error instanceof Error ? error.message : "隔离操作失败");
    } finally {
      setBusy("");
    }
  }

  async function isolateAllDegraded() {
    setBusy("isolate-all");
    setMsg("");
    try {
      const response = await api<{ isolated?: number }>("/api/degrade/isolate", {
        method: "POST",
        body: JSON.stringify({ all_degraded: true }),
      });
      setMsg(`已隔离 ${response.isolated || 0} 个疑似降智账号`);
      await load(true);
    } catch (error) {
      setMsg(error instanceof Error ? error.message : "批量隔离失败");
    } finally {
      setBusy("");
    }
  }

  const overview = data?.overview;
  const status = data?.status;
  const settings = data?.settings;
  const history = data?.history || [];
  const exits = data?.exits || [];

  return (
    <AdminShell>
      <PageHeader
        title="降智分析"
        description="抽样识别账号是否被静默降级，并控制单一出口的探测账号数量"
        actions={
          <>
            <Button
              size="sm"
              variant="secondary"
              disabled={busy !== ""}
              onClick={() => void load()}
            >
              <ArrowsClockwiseIcon size={16} /> 刷新
            </Button>
            <Button
              size="sm"
              loading={busy === "scan" || !!status?.running}
              disabled={busy !== "" || !!status?.running}
              onClick={() => void startScan()}
            >
              <PlayIcon size={16} /> 开始抽样
            </Button>
          </>
        }
      />

      {msg ? (
        <div className="mb-3 rounded-md bg-kumo-contrast/5 px-3 py-2">
          <Text size="sm">{msg}</Text>
        </div>
      ) : null}

      <LayerCard className="mb-4">
        <LayerCard.Secondary>扫描控制</LayerCard.Secondary>
        <LayerCard.Primary>
          <div className="flex flex-col gap-3">
            <Text size="sm">
              每次探测都会消耗账号额度，并计入当前出口的近期账号数。扫描会按出口窗口自动等待，不会一次遍历整个号池。
            </Text>
            <div className="grid items-end gap-3 md:grid-cols-[minmax(160px,220px)_auto_minmax(0,1fr)]">
              <Input
                label="本次抽样数量"
                type="number"
                min={1}
                max={100}
                value={String(sample)}
                onChange={(event) => setSample(Math.max(1, Number(event.target.value) || 1))}
              />
              <Switch label="忽略复检间隔" checked={recheck} onCheckedChange={setRecheck} />
              <Text size="xs" variant="secondary">
                模型 {settings?.model || "—"} · 当前出口 {settings?.exit || "—"} · {settings?.exit_window_min || 0} 分钟最多 {settings?.exit_account_cap || 0} 个账号
              </Text>
            </div>
            {status?.running ? (
              <Text size="sm">扫描进行中：{status.note || "准备中"}</Text>
            ) : (
              <Text size="xs" variant="secondary">
                默认抽样 {settings?.sample || 0} 个账号；{settings?.recheck_min || 0} 分钟内已检测账号默认跳过。
              </Text>
            )}
          </div>
        </LayerCard.Primary>
      </LayerCard>

      <LayerCard className="mb-4">
        <LayerCard.Secondary>检测概览</LayerCard.Secondary>
        <LayerCard.Primary>
          <div className="grid gap-x-6 gap-y-3 sm:grid-cols-2 lg:grid-cols-4">
            {[
              ["号池总数", overview?.pool_total || 0],
              ["已检测", overview?.checked || 0],
              ["正常", overview?.normal || 0],
              ["疑似降智", overview?.degraded || 0],
              ["额度耗尽", overview?.exhausted || 0],
              ["检测错误", overview?.errored || 0],
              ["已隔离", overview?.isolated || 0],
              ["未检测", overview?.unchecked || 0],
            ].map(([label, value]) => (
              <div key={label} className="border-b border-kumo-hairline pb-2">
                <Text size="xs" variant="secondary">{label}</Text>
                <Text>{value}</Text>
              </div>
            ))}
          </div>
        </LayerCard.Primary>
      </LayerCard>

      <LayerCard className="mb-4">
        <LayerCard.Secondary>账号结论 · {total}</LayerCard.Secondary>
        <LayerCard.Primary>
          <div className="mb-4 flex flex-wrap items-end gap-3">
            <div className="min-w-44">
              <Select label="结论" value={verdict} onValueChange={(value) => value && setVerdict(value)}>
                {VERDICT_OPTIONS.map((option) => (
                  <Select.Option key={option.value} value={option.value}>{option.label}</Select.Option>
                ))}
              </Select>
            </div>
            <div className="min-w-64 flex-1">
              <Input
                label="搜索账号或邮箱"
                value={query}
                onChange={(event) => setQuery(event.target.value)}
                placeholder="auth 文件名 / email"
              />
            </div>
            <Button
              size="sm"
              variant="secondary"
              loading={busy === "isolate-all"}
              disabled={!overview?.degraded || busy !== ""}
              onClick={() => void isolateAllDegraded()}
            >
              <ShieldCheckIcon size={16} /> 隔离全部降智账号
            </Button>
          </div>

          {records.length === 0 ? (
            <Text variant="secondary">暂无符合条件的检测记录</Text>
          ) : (
            <div className="overflow-x-auto">
              <Table>
                <Table.Header>
                  <Table.Row>
                    <Table.Head>账号</Table.Head>
                    <Table.Head>结论</Table.Head>
                    <Table.Head>推理信号</Table.Head>
                    <Table.Head>出口</Table.Head>
                    <Table.Head>检测时间</Table.Head>
                    <Table.Head>操作</Table.Head>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {records.map((record) => (
                    <Table.Row key={record.name}>
                      <Table.Cell>
                        <div className="max-w-64">
                          <Text size="sm">{record.email || record.name}</Text>
                          <Text size="xs" variant="secondary">{record.name}</Text>
                        </div>
                      </Table.Cell>
                      <Table.Cell>
                        <Badge variant={record.verdict === "normal" ? "primary" : "secondary"}>
                          {verdictLabel(record.verdict)}
                        </Badge>
                        {record.isolated ? <Badge variant="secondary">已隔离</Badge> : null}
                      </Table.Cell>
                      <Table.Cell>
                        <Text size="sm">reasoning {record.reasoning_tokens || 0}</Text>
                        <Text size="xs" variant="secondary">
                          {record.answer_ok ? "答案正确" : record.detail || "无附加信号"}
                        </Text>
                      </Table.Cell>
                      <Table.Cell><Text size="xs">{record.exit || "direct"}</Text></Table.Cell>
                      <Table.Cell>
                        <Text size="xs">{formatDate(record.checked_at)}</Text>
                        {record.first_degraded_at ? (
                          <Text size="xs" variant="secondary">首次异常 {formatDate(record.first_degraded_at)}</Text>
                        ) : null}
                      </Table.Cell>
                      <Table.Cell>
                        <Button
                          size="sm"
                          variant="secondary"
                          loading={busy === `isolate:${record.name}`}
                          disabled={busy !== "" && busy !== `isolate:${record.name}`}
                          onClick={() => void setIsolated(record, !record.isolated)}
                        >
                          {record.isolated ? "解除隔离" : "本地隔离"}
                        </Button>
                      </Table.Cell>
                    </Table.Row>
                  ))}
                </Table.Body>
              </Table>
            </div>
          )}
        </LayerCard.Primary>
      </LayerCard>

      <div className="grid items-start gap-4 lg:grid-cols-2">
        <LayerCard>
          <LayerCard.Secondary>出口窗口</LayerCard.Secondary>
          <LayerCard.Primary>
            {exits.length === 0 ? (
              <Text variant="secondary">尚无本面板发起的探测流量</Text>
            ) : (
              <div className="flex flex-col gap-3">
                {exits.map((exit) => (
                  <div key={exit.exit} className="border-b border-kumo-hairline pb-3 last:border-0 last:pb-0">
                    <div className="flex flex-wrap items-center justify-between gap-2">
                      <Text size="sm">{exit.exit}</Text>
                      <Badge variant={exit.over ? "secondary" : "primary"}>
                        {exit.accounts}/{exit.cap} 个账号
                      </Badge>
                    </div>
                    <Text size="xs" variant="secondary">
                      {exit.window_min} 分钟内 {exit.requests} 次请求 · {exit.degraded} 个降智 · 最近 {formatDate(exit.last_at)}
                    </Text>
                  </div>
                ))}
              </div>
            )}
          </LayerCard.Primary>
        </LayerCard>

        <LayerCard>
          <LayerCard.Secondary>最近扫描</LayerCard.Secondary>
          <LayerCard.Primary>
            {history.length === 0 ? (
              <Text variant="secondary">尚未执行抽样扫描</Text>
            ) : (
              <div className="flex flex-col gap-3">
                {history.slice(0, 8).map((item) => (
                  <div key={`${item.time}-${item.duration_ms}`} className="border-b border-kumo-hairline pb-3 last:border-0 last:pb-0">
                    <div className="flex flex-wrap items-center justify-between gap-2">
                      <Text size="sm">{formatDate(item.time)}</Text>
                      <Text size="xs" variant="secondary">{formatDuration(item.duration_ms)}</Text>
                    </div>
                    <Text size="xs" variant="secondary">
                      检测 {item.checked} · 正常 {item.normal} · 降智 {item.degraded} · 耗尽 {item.exhausted} · 错误 {item.errored}
                    </Text>
                    {item.error ? <Text size="xs">{item.error}</Text> : null}
                  </div>
                ))}
              </div>
            )}
          </LayerCard.Primary>
        </LayerCard>
      </div>
    </AdminShell>
  );
}
