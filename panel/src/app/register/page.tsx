"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import {
  Badge,
  Button,
  Dialog,
  Input,
  LayerCard,
  Select,
  Switch,
  Table,
  Text,
} from "@cloudflare/kumo";
import { AdminShell } from "@/components/admin-shell";
import { PageHeader } from "@/components/page-header";
import { api, tokenQuery, type RunStatus } from "@/lib/api";

type RunInfo = {
  id: string;
  cpa_count: number;
  sso_files: number;
  mod_time?: string;
  path?: string;
};

type RegistrarOption = {
  id: string;
  name: string;
  enabled?: boolean;
  kind?: string[];
};

type RunFile = { path: string; size?: number };

const PAGE_SIZE = 10;

const FALLBACK_REGISTRARS: RegistrarOption[] = [
  { id: "xai-accounts", name: "xAI 账号（xai-accounts）", enabled: true },
  { id: "tavily-registrar", name: "Tavily 注册（tavily-registrar）", enabled: true },
];

const PHASES: { key: string; label: string }[] = [
  { key: "idle", label: "待命" },
  { key: "clearance", label: "清障" },
  { key: "register", label: "注册" },
  { key: "oauth", label: "OAuth" },
  { key: "probe", label: "探活" },
];

function phaseIndex(phase?: string): number {
  const p = String(phase || "idle").toLowerCase();
  const i = PHASES.findIndex((x) => x.key === p);
  return i < 0 ? 0 : i;
}

function statusBadge(status?: string) {
  const s = String(status || "stopped").toLowerCase();
  if (s === "running") return <Badge variant="primary">运行中</Badge>;
  if (s === "error") return <Badge variant="secondary">错误</Badge>;
  return <Badge variant="secondary">已停止</Badge>;
}

function Pager({
  page,
  totalPages,
  total,
  onChange,
}: {
  page: number;
  totalPages: number;
  total: number;
  onChange: (p: number) => void;
}) {
  if (total <= 0) return null;
  const pages = Math.max(1, totalPages);
  return (
    <div className="flex flex-wrap items-center justify-between gap-2 border-t border-kumo-hairline p-4">
      <Text size="xs" variant="secondary">
        共 {total} · 第 {page}/{pages} 页
      </Text>
      <div className="flex flex-wrap gap-2">
        <Button size="sm" variant="secondary" disabled={page <= 1} onClick={() => onChange(page - 1)}>
          上一页
        </Button>
        <Button
          size="sm"
          variant="secondary"
          disabled={page >= pages}
          onClick={() => onChange(page + 1)}
        >
          下一页
        </Button>
      </div>
    </div>
  );
}

export default function RegisterPage() {
  const [target, setTarget] = useState("10");
  const [regType, setRegType] = useState("xai-accounts");
  const [registrars, setRegistrars] = useState<RegistrarOption[]>(FALLBACK_REGISTRARS);
  const [status, setStatus] = useState<RunStatus | null>(null);
  const [log, setLog] = useState("");
  const [logPath, setLogPath] = useState("");
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

  const [createOpen, setCreateOpen] = useState(false);
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [selected, setSelected] = useState<RunInfo | null>(null);
  const [drawerTab, setDrawerTab] = useState<"overview" | "logs" | "files">("overview");
  const [runFiles, setRunFiles] = useState<RunFile[]>([]);

  const running = useMemo(
    () => String(status?.status || "").toLowerCase() === "running",
    [status],
  );

  const refreshCore = useCallback(async () => {
    try {
      const [st, cfg, lp, plugins] = await Promise.all([
        api<{ status: RunStatus }>("/api/status"),
        api<{ config?: Record<string, unknown> }>("/api/config").catch(() => ({
          config: {} as Record<string, unknown>,
        })),
        api<{ total?: number; unsynced?: number }>(
          "/api/pool/list?source=local&page=1&limit=1",
        ).catch(() => ({ total: 0, unsynced: 0 })),
        api<{ plugins?: RegistrarOption[] }>("/api/plugins").catch(() => ({
          plugins: [],
        })),
      ]);
      setStatus(st.status);
      setPoolTotal(lp.total || 0);
      setPoolUnsynced(lp.unsynced || 0);
      const conf = cfg.config || {};
      if (typeof conf.local_pool_auto_import === "boolean") {
        setAutoImport(conf.local_pool_auto_import);
      }
      if (typeof conf.local_pool_auto_sync === "boolean") {
        setAutoSync(conf.local_pool_auto_sync);
      }
      const regs = (plugins.plugins || []).filter((p) => {
        const kinds = Array.isArray(p.kind) ? p.kind.map(String) : [];
        return kinds.includes("registrar") && p.enabled !== false;
      });
      if (regs.length > 0) {
        setRegistrars(regs);
        setRegType((cur) => (regs.some((r) => r.id === cur) ? cur : regs[0].id));
      }
    } catch (e) {
      setMsg(e instanceof Error ? e.message : "刷新失败");
    }
  }, []);

  const refreshRuns = useCallback(async (page: number) => {
    try {
      const rs = await api<{
        runs?: RunInfo[];
        total?: number;
        total_pages?: number;
      }>(`/api/runs?page=${page}&limit=${PAGE_SIZE}`);
      setRuns(rs.runs || []);
      setRunsTotal(rs.total || 0);
      setRunsTotalPages(rs.total_pages || 0);
    } catch (e) {
      setMsg(e instanceof Error ? e.message : "加载任务失败");
    }
  }, []);

  const refreshLogs = useCallback(async () => {
    try {
      const lg = await api<{ log?: string; path?: string }>("/api/logs?tail=400");
      setLog(lg.log || "");
      setLogPath(lg.path || "");
    } catch {
      /* no log yet */
    }
  }, []);

  const loadRunFiles = useCallback(async (id: string) => {
    try {
      const d = await api<{ files?: RunFile[] }>(
        `/api/runs/${encodeURIComponent(id)}/files`,
      );
      setRunFiles(d.files || []);
    } catch {
      setRunFiles([]);
    }
  }, []);

  useEffect(() => {
    void refreshCore();
    void refreshRuns(1);
    const t = setInterval(() => {
      void refreshCore();
      void refreshRuns(runsPage);
    }, 3000);
    return () => clearInterval(t);
  }, [refreshCore, refreshRuns, runsPage]);

  useEffect(() => {
    void refreshRuns(runsPage);
  }, [runsPage, refreshRuns]);

  useEffect(() => {
    if (!drawerOpen) return;
    void refreshLogs();
    if (!running) return;
    const t = setInterval(() => void refreshLogs(), 2000);
    return () => clearInterval(t);
  }, [drawerOpen, running, refreshLogs]);

  function openTask(run: RunInfo) {
    setSelected(run);
    setDrawerTab("overview");
    setDrawerOpen(true);
    void refreshLogs();
    void loadRunFiles(run.id);
  }

  function openCurrentTask() {
    if (!status?.run_id) {
      setMsg("当前没有活动任务");
      return;
    }
    openTask({
      id: status.run_id,
      cpa_count: status.done ?? status.success ?? 0,
      sso_files: status.sso_count ?? 0,
      mod_time: status.updated_at || status.started_at,
    });
  }

  async function start() {
    setBusy(true);
    setMsg("");
    try {
      const n = Math.max(1, Math.min(10000, parseInt(target, 10) || 10));
      const d = await api<{ run_id?: string; plugin?: string }>("/api/start", {
        method: "POST",
        body: JSON.stringify({ target: n, plugin: regType }),
      });
      setMsg(`已创建任务 ${d.run_id || ""} · ${regType} · target=${n}`);
      setCreateOpen(false);
      await refreshCore();
      await refreshRuns(1);
      setRunsPage(1);
      if (d.run_id) {
        openTask({
          id: d.run_id,
          cpa_count: 0,
          sso_files: 0,
          mod_time: new Date().toISOString(),
        });
      }
    } catch (e) {
      setMsg(e instanceof Error ? e.message : "启动失败");
    } finally {
      setBusy(false);
    }
  }

  async function stop() {
    setBusy(true);
    try {
      await api("/api/stop", { method: "POST", body: "{}" });
      setMsg("已停止");
      try {
        await api("/api/local-pool/import", {
          method: "POST",
          body: JSON.stringify({}),
        });
      } catch {
        /* optional */
      }
      await refreshCore();
      await refreshRuns(runsPage);
    } catch (e) {
      setMsg(e instanceof Error ? e.message : "停止失败");
    } finally {
      setBusy(false);
    }
  }

  async function savePoolFlags(nextImport: boolean, nextSync: boolean) {
    setAutoImport(nextImport);
    setAutoSync(nextSync);
    try {
      await api("/api/config", {
        method: "PUT",
        body: JSON.stringify({
          local_pool_auto_import: nextImport,
          local_pool_auto_sync: nextSync,
        }),
      });
    } catch (e) {
      setMsg(e instanceof Error ? e.message : "保存自动入库设置失败");
    }
  }

  async function importPool(runId?: string) {
    setBusy(true);
    try {
      const d = await api<{ added?: number; run_id?: string }>(
        "/api/local-pool/import",
        {
          method: "POST",
          body: JSON.stringify(runId ? { run_id: runId } : {}),
        },
      );
      setMsg(
        `已入库 ${d.added ?? 0} 个（run ${d.run_id || "latest"}）· 去「号池」查看`,
      );
      await refreshCore();
      await refreshRuns(runsPage);
    } catch (e) {
      setMsg(e instanceof Error ? e.message : "入库失败");
    } finally {
      setBusy(false);
    }
  }

  const tableRuns = useMemo(() => {
    const list = [...runs];
    const rid = status?.run_id;
    if (rid && running && !list.some((r) => r.id === rid)) {
      list.unshift({
        id: rid,
        cpa_count: status?.done ?? 0,
        sso_files: status?.sso_count ?? 0,
        mod_time: status?.updated_at || status?.started_at,
      });
    }
    return list;
  }, [runs, status, running]);

  const isSelectedCurrent =
    !!selected && !!status?.run_id && selected.id === status.run_id;

  const drawerPhase = isSelectedCurrent ? status?.phase : undefined;
  const drawerPhaseDetail = isSelectedCurrent
    ? status?.phase_detail
    : selected
      ? "历史任务"
      : "";
  const drawerStatus = isSelectedCurrent
    ? status?.status
    : selected
      ? "stopped"
      : undefined;
  const curPhaseIdx = phaseIndex(drawerPhase);

  return (
    <AdminShell>
      <PageHeader
        title="注册任务"
        description="任务列表 · 阶段进度 · 日志 / 结果抽屉"
        actions={
          <>
            {running ? (
              <Button size="sm" variant="secondary" loading={busy} onClick={() => void stop()}>
                停止当前
              </Button>
            ) : null}
            <Button
              size="sm"
              variant="secondary"
              loading={busy}
              onClick={() => {
                void refreshCore();
                void refreshRuns(runsPage);
              }}
            >
              刷新
            </Button>
            <Button
              size="sm"
              loading={busy}
              disabled={running}
              onClick={() => setCreateOpen(true)}
            >
              新建任务
            </Button>
          </>
        }
      />

      {msg ? (
        <div className="mb-3 rounded-md bg-kumo-contrast/5 px-3 py-2">
          <Text>{msg}</Text>
        </div>
      ) : null}

      <div className="mb-4 grid gap-3 sm:grid-cols-4">
        <LayerCard>
          <LayerCard.Secondary>当前状态</LayerCard.Secondary>
          <LayerCard.Primary className="p-4">
            <div className="flex flex-wrap items-center gap-2">
              {statusBadge(status?.status)}
              {status?.run_id ? (
                <Button size="sm" variant="secondary" onClick={openCurrentTask}>
                  {status.run_id}
                </Button>
              ) : (
                <Text size="sm" variant="secondary">
                  无活动任务
                </Text>
              )}
            </div>
            {running ? (
              <Text size="xs" variant="secondary">
                阶段 {status?.phase || "—"}
                {status?.phase_detail ? ` · ${status.phase_detail}` : ""}
              </Text>
            ) : null}
          </LayerCard.Primary>
        </LayerCard>
        <LayerCard>
          <LayerCard.Secondary>进度</LayerCard.Secondary>
          <LayerCard.Primary className="p-4">
            <Text size="sm">
              {status?.done ?? 0}/{status?.target ?? 0}
              {status?.fail_count ? ` · 失败 ${status.fail_count}` : ""}
            </Text>
          </LayerCard.Primary>
        </LayerCard>
        <LayerCard>
          <LayerCard.Secondary>本地号池</LayerCard.Secondary>
          <LayerCard.Primary className="p-4">
            <Text size="sm">
              {poolTotal} 个{poolUnsynced ? ` · 未同步 ${poolUnsynced}` : ""}
            </Text>
          </LayerCard.Primary>
        </LayerCard>
        <LayerCard>
          <LayerCard.Secondary>自动策略</LayerCard.Secondary>
          <LayerCard.Primary className="p-4">
            <div className="flex flex-col gap-2">
              <Switch
                label="完成后自动入库"
                checked={autoImport}
                onCheckedChange={(v) => void savePoolFlags(!!v, autoSync)}
              />
              <Switch
                label="入库后同步云端"
                checked={autoSync}
                onCheckedChange={(v) => void savePoolFlags(autoImport, !!v)}
              />
            </div>
          </LayerCard.Primary>
        </LayerCard>
      </div>

      <LayerCard>
        <LayerCard.Secondary>
          任务列表 {runsTotal ? `(${runsTotal})` : ""}
        </LayerCard.Secondary>
        <LayerCard.Primary className="p-0">
          {tableRuns.length === 0 ? (
            <div className="p-4">
              <Text variant="secondary">
                暂无任务 — 点「新建任务」选择注册类型并启动
              </Text>
            </div>
          ) : (
            <Table>
              <Table.Header>
                <Table.Row>
                  <Table.Head>任务 ID</Table.Head>
                  <Table.Head>状态</Table.Head>
                  <Table.Head>阶段</Table.Head>
                  <Table.Head>成果</Table.Head>
                  <Table.Head>时间</Table.Head>
                  <Table.Head>操作</Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {tableRuns.map((r) => {
                  const isLive = !!status?.run_id && r.id === status.run_id;
                  const st = isLive ? status?.status : "stopped";
                  const ph = isLive ? status?.phase : "—";
                  const detail = isLive ? status?.phase_detail : "";
                  return (
                    <Table.Row key={r.id}>
                      <Table.Cell>
                        <button type="button" className="text-left" onClick={() => openTask(r)}>
                          <Text size="sm">{r.id}</Text>
                          {isLive ? (
                            <Text size="xs" variant="secondary">
                              当前任务
                            </Text>
                          ) : null}
                        </button>
                      </Table.Cell>
                      <Table.Cell>{statusBadge(st)}</Table.Cell>
                      <Table.Cell>
                        <Text size="sm">{String(ph || "—")}</Text>
                        {detail ? (
                          <Text size="xs" variant="secondary">
                            {detail}
                          </Text>
                        ) : null}
                      </Table.Cell>
                      <Table.Cell>
                        <Text size="sm">
                          CPA {isLive ? (status?.done ?? r.cpa_count) : r.cpa_count}
                          {" · "}
                          SSO {isLive ? (status?.sso_count ?? r.sso_files) : r.sso_files}
                        </Text>
                      </Table.Cell>
                      <Table.Cell>
                        <Text size="xs" variant="secondary">
                          {r.mod_time || "—"}
                        </Text>
                      </Table.Cell>
                      <Table.Cell>
                        <div className="flex flex-wrap gap-2">
                          <Button size="sm" variant="secondary" onClick={() => openTask(r)}>
                            详情
                          </Button>
                          <Button
                            size="sm"
                            variant="secondary"
                            loading={busy}
                            onClick={() => void importPool(r.id)}
                          >
                            入库
                          </Button>
                          <Button
                            size="sm"
                            variant="secondary"
                            onClick={() => {
                              window.open(
                                `/api/runs/${encodeURIComponent(r.id)}/download${tokenQuery()}`,
                                "_blank",
                              );
                            }}
                          >
                            下载
                          </Button>
                        </div>
                      </Table.Cell>
                    </Table.Row>
                  );
                })}
              </Table.Body>
            </Table>
          )}
          <Pager
            page={runsPage}
            totalPages={runsTotalPages}
            total={runsTotal}
            onChange={setRunsPage}
          />
        </LayerCard.Primary>
      </LayerCard>

      <Dialog.Root open={createOpen} onOpenChange={(next) => setCreateOpen(!!next)}>
        <Dialog size="base" className="flex max-h-[min(90vh,40rem)] flex-col p-6">
          <div className="mb-4">
            <Dialog.Title className="text-xl font-semibold">新建注册任务</Dialog.Title>
            <Dialog.Description className="mt-1 text-kumo-subtle">
              选择 registrar 插件与目标数量
            </Dialog.Description>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto">
            <div className="flex flex-col gap-3">
              <Select
                label="注册类型"
                value={regType}
                onValueChange={(v) => {
                  if (!v) return;
                  setRegType(v);
                }}
              >
                {registrars.map((r) => (
                  <Select.Option key={r.id} value={r.id}>
                    {r.name || r.id}
                  </Select.Option>
                ))}
              </Select>
              <Input
                label="目标数量"
                value={target}
                onChange={(e) => setTarget(e.target.value)}
              />
              {running ? (
                <Text size="sm" variant="secondary">
                  当前已有任务在跑，请先停止再新建
                </Text>
              ) : null}
            </div>
          </div>
          <div className="mt-6 flex flex-wrap justify-end gap-2">
            <Dialog.Close
              render={(p) => (
                <Button {...p} size="sm" variant="secondary">
                  取消
                </Button>
              )}
            />
            <Button size="sm" loading={busy} disabled={running} onClick={() => void start()}>
              启动
            </Button>
          </div>
        </Dialog>
      </Dialog.Root>

      <Dialog.Root
        open={drawerOpen}
        onOpenChange={(next) => {
          setDrawerOpen(!!next);
          if (!next) setSelected(null);
        }}
      >
        <Dialog size="xl" className="flex max-h-[min(90vh,56rem)] flex-col p-6">
          <div className="mb-4 flex items-start justify-between gap-4">
            <div className="min-w-0">
              <Dialog.Title className="text-xl font-semibold">
                任务 {selected?.id || "—"}
              </Dialog.Title>
              <Dialog.Description className="mt-1 text-kumo-subtle">
                {statusBadge(drawerStatus)} {isSelectedCurrent ? "实时" : "历史"} · 阶段 / 日志 / 产物
              </Dialog.Description>
            </div>
            <div className="flex flex-wrap gap-2">
              {isSelectedCurrent && running ? (
                <Button size="sm" variant="secondary" loading={busy} onClick={() => void stop()}>
                  停止
                </Button>
              ) : null}
              {selected ? (
                <Button
                  size="sm"
                  variant="secondary"
                  loading={busy}
                  onClick={() => void importPool(selected.id)}
                >
                  入库号池
                </Button>
              ) : null}
              <Dialog.Close
                render={(p) => (
                  <Button {...p} size="sm" variant="secondary">
                    关闭
                  </Button>
                )}
              />
            </div>
          </div>

          <div className="mb-3 flex flex-wrap gap-2">
            {(
              [
                ["overview", "概览 / 阶段"],
                ["logs", "日志"],
                ["files", "产物"],
              ] as const
            ).map(([k, label]) => (
              <Button
                key={k}
                size="sm"
                variant={drawerTab === k ? "primary" : "secondary"}
                onClick={() => setDrawerTab(k)}
              >
                {label}
              </Button>
            ))}
          </div>

          <div className="min-h-0 flex-1 overflow-y-auto">
            {drawerTab === "overview" ? (
              <div className="flex flex-col gap-4">
                <div>
                  <Text size="sm">流水线阶段</Text>
                  <div className="mt-2 flex flex-wrap gap-2">
                    {PHASES.map((p, i) => {
                      const active = i === curPhaseIdx && isSelectedCurrent;
                      const done = isSelectedCurrent && i < curPhaseIdx;
                      return (
                        <Badge key={p.key} variant={active || done ? "primary" : "secondary"}>
                          {i + 1}. {p.label}
                          {active ? " ←" : ""}
                        </Badge>
                      );
                    })}
                  </div>
                  <Text size="xs" variant="secondary">
                    {drawerPhaseDetail || "—"}
                  </Text>
                </div>

                <div className="grid gap-3 sm:grid-cols-2">
                  <LayerCard>
                    <LayerCard.Secondary>计数</LayerCard.Secondary>
                    <LayerCard.Primary className="p-4">
                      {isSelectedCurrent ? (
                        <Text size="sm">
                          目标 {status?.target ?? 0}
                          <br />
                          完成 {status?.done ?? 0}
                          <br />
                          SSO {status?.sso_count ?? 0} · OAuth {status?.oauth_count ?? 0}
                          <br />
                          失败 {status?.fail_count ?? 0}
                          {status?.rate_per_min
                            ? ` · ${Number(status.rate_per_min).toFixed(1)}/min`
                            : ""}
                        </Text>
                      ) : (
                        <Text size="sm">
                          CPA {selected?.cpa_count ?? 0}
                          <br />
                          SSO 文件 {selected?.sso_files ?? 0}
                        </Text>
                      )}
                    </LayerCard.Primary>
                  </LayerCard>
                  <LayerCard>
                    <LayerCard.Secondary>运行信息</LayerCard.Secondary>
                    <LayerCard.Primary className="p-4">
                      <Text size="sm">
                        PID {isSelectedCurrent ? status?.pid || "—" : "—"}
                        <br />
                        日志 {isSelectedCurrent ? logPath || status?.log_path || "—" : "—"}
                        <br />
                        输出{" "}
                        {isSelectedCurrent
                          ? status?.output_dir || "—"
                          : selected?.path || "—"}
                        <br />
                        更新{" "}
                        {isSelectedCurrent
                          ? status?.updated_at || "—"
                          : selected?.mod_time || "—"}
                      </Text>
                      {isSelectedCurrent && status?.error ? (
                        <Text size="xs" variant="secondary">
                          错误：{status.error}
                        </Text>
                      ) : null}
                    </LayerCard.Primary>
                  </LayerCard>
                </div>
              </div>
            ) : null}

            {drawerTab === "logs" ? (
              <div className="flex flex-col gap-2">
                <div className="flex gap-2">
                  <Button size="sm" variant="secondary" onClick={() => void refreshLogs()}>
                    刷新日志
                  </Button>
                  <Text size="xs" variant="secondary">
                    {logPath || status?.log_path || "默认最新日志"}
                    {isSelectedCurrent && running ? " · 自动刷新" : ""}
                  </Text>
                </div>
                <pre className="max-h-[50vh] overflow-auto rounded-md bg-kumo-contrast/5 p-3 text-xs whitespace-pre-wrap">
                  {log || "（暂无日志）"}
                </pre>
              </div>
            ) : null}

            {drawerTab === "files" ? (
              <div className="flex flex-col gap-2">
                {selected ? (
                  <Button
                    size="sm"
                    variant="secondary"
                    onClick={() => {
                      window.open(
                        `/api/runs/${encodeURIComponent(selected.id)}/download${tokenQuery()}`,
                        "_blank",
                      );
                    }}
                  >
                    打包下载
                  </Button>
                ) : null}
                {runFiles.length === 0 ? (
                  <Text variant="secondary">暂无产物文件列表</Text>
                ) : (
                  <div className="flex flex-col gap-1">
                    {runFiles.map((f) => (
                      <Text key={f.path} size="sm">
                        {f.path}
                        {typeof f.size === "number" ? ` · ${f.size}B` : ""}
                      </Text>
                    ))}
                  </div>
                )}
              </div>
            ) : null}
          </div>
        </Dialog>
      </Dialog.Root>
    </AdminShell>
  );
}
