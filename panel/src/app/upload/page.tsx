"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { Badge, Button, Dialog, Input, LayerCard, Switch, Table, Text } from "@cloudflare/kumo";
import { AdminShell } from "@/components/admin-shell";
import { PageHeader } from "@/components/page-header";
import { api, getToken } from "@/lib/api";
import { formatDateTime, useTimezone } from "@/lib/timezone";

type UploadItem = {
  id: number;
  name: string;
  status: string;
  attempts: number;
  error?: string;
  fromCache?: boolean;
};

type UploadJob = {
  id: string;
  status: string;
  createdAt: string;
  startedAt?: string;
  finishedAt?: string;
  queuePosition?: number;
  total: number;
  done: number;
  progress: number;
  counts: {
    pending: number;
    uploading: number;
    success: number;
    failed: number;
    skipped: number;
  };
  options: {
    concurrency: number;
    batchSize: number;
    retryLimit: number;
    source?: string;
    runId?: string;
  };
  logs?: string[];
  items?: UploadItem[];
};

const TERMINAL = new Set(["completed", "failed", "cancelled"]);

function statusLabel(status: string) {
  return {
    queued: "排队中",
    running: "上传中",
    completed: "已完成",
    failed: "失败",
    cancelled: "已取消",
  }[status] || status;
}

function sourceLabel(source?: string) {
  if (!source || source === "manual") return "手工上传";
  if (source === "auto-run") return "注册自动上传";
  if (source === "local-sync") return "本地快速同步";
  if (source.startsWith("pool:")) return `凭证池 · ${source.slice(5)}`;
  return source;
}

export default function UploadPage() {
  const { timezone } = useTimezone();
  const [jobs, setJobs] = useState<UploadJob[]>([]);
  const [files, setFiles] = useState<FileList | null>(null);
  const [concurrency, setConcurrency] = useState("");
  const [batchSize, setBatchSize] = useState("");
  const [retryLimit, setRetryLimit] = useState("");
  const [skipCached, setSkipCached] = useState(true);
  const [createOpen, setCreateOpen] = useState(false);
  const [detailOpen, setDetailOpen] = useState(false);
  const [selectedID, setSelectedID] = useState("");
  const [msg, setMsg] = useState("");
  const [busy, setBusy] = useState(false);

  const loadJobs = useCallback(async () => {
    try {
      const response = await api<{ jobs?: UploadJob[] }>("/api/transfer/jobs");
      setJobs(response.jobs || []);
    } catch (error) {
      setMsg(error instanceof Error ? error.message : "加载上传任务失败");
    }
  }, []);

  useEffect(() => {
    void loadJobs();
    const timer = window.setInterval(() => void loadJobs(), 1000);
    return () => window.clearInterval(timer);
  }, [loadJobs]);

  const selectedJob = useMemo(
    () => jobs.find((job) => job.id === selectedID) || null,
    [jobs, selectedID],
  );

  const formatWhen = (value?: string) => (value ? formatDateTime(value, timezone) : "—");

  function openCreate() {
    setFiles(null);
    setConcurrency("");
    setBatchSize("");
    setRetryLimit("");
    setSkipCached(true);
    setCreateOpen(true);
  }

  function openDetail(job: UploadJob) {
    setSelectedID(job.id);
    setDetailOpen(true);
  }

  async function prepareAndStart() {
    if (!files || files.length === 0) {
      setMsg("请选择 .json / .zip 文件");
      return;
    }
    setBusy(true);
    setMsg("");
    try {
      const form = new FormData();
      Array.from(files).forEach((file) => form.append("files", file));
      if (concurrency.trim()) form.append("concurrency", concurrency.trim());
      if (batchSize.trim()) form.append("batchSize", batchSize.trim());
      if (retryLimit.trim()) form.append("retryLimit", retryLimit.trim());
      form.append("skipCached", skipCached ? "true" : "false");
      const token = getToken();
      const response = await fetch("/api/transfer/prepare", {
        method: "POST",
        headers: token ? { "X-Panel-Token": token } : undefined,
        body: form,
      });
      const data = (await response.json()) as { error?: string; job?: UploadJob };
      if (!response.ok || !data.job?.id) throw new Error(data.error || "创建上传任务失败");
      await api(`/api/transfer/jobs/${data.job.id}/start`, { method: "POST", body: "{}" });
      setSelectedID(data.job.id);
      setCreateOpen(false);
      setDetailOpen(true);
      setMsg(`任务已加入上传队列 · ${data.job.id}`);
      await loadJobs();
    } catch (error) {
      setMsg(error instanceof Error ? error.message : "创建上传任务失败");
    } finally {
      setBusy(false);
    }
  }

  async function cancel(job: UploadJob) {
    setBusy(true);
    try {
      await api(`/api/transfer/jobs/${job.id}/cancel`, { method: "POST", body: "{}" });
      setMsg(`已取消 ${job.id}`);
      await loadJobs();
    } catch (error) {
      setMsg(error instanceof Error ? error.message : "取消任务失败");
    } finally {
      setBusy(false);
    }
  }

  async function retryFailed(job: UploadJob) {
    setBusy(true);
    try {
      await api(`/api/transfer/jobs/${job.id}/retry-failed`, { method: "POST", body: "{}" });
      setMsg(`失败项已重新入队 · ${job.id}`);
      await loadJobs();
    } catch (error) {
      setMsg(error instanceof Error ? error.message : "重试失败项失败");
    } finally {
      setBusy(false);
    }
  }

  return (
    <AdminShell>
      <PageHeader
        title="凭证上传"
        description="手工、凭证池和注册自动上传共用一个任务队列；任务间串行、任务内分批并发"
        actions={
          <>
            <Button size="sm" variant="secondary" loading={busy} onClick={() => void loadJobs()}>
              刷新
            </Button>
            <Button size="sm" onClick={openCreate}>新建上传任务</Button>
          </>
        }
      />

      {msg ? (
        <div role="status" aria-live="polite" className="mb-3 rounded-md bg-kumo-contrast/5 px-3 py-2">
          <Text>{msg}</Text>
        </div>
      ) : null}

      <LayerCard>
        <LayerCard.Secondary>
          <div className="flex w-full items-center justify-between gap-3">
            <span>上传任务 {jobs.length ? `(${jobs.length})` : ""}</span>
            <Text size="xs" variant="secondary">每秒自动刷新</Text>
          </div>
        </LayerCard.Secondary>
        <LayerCard.Primary className="p-0">
          {jobs.length === 0 ? (
            <div className="p-6"><Text variant="secondary">暂无上传任务</Text></div>
          ) : (
            <div className="overflow-x-auto">
              <Table className="min-w-[980px]">
                <Table.Header>
                  <Table.Row>
                    <Table.Head>任务</Table.Head>
                    <Table.Head>来源</Table.Head>
                    <Table.Head>状态</Table.Head>
                    <Table.Head>进度</Table.Head>
                    <Table.Head>分批策略</Table.Head>
                    <Table.Head>创建时间</Table.Head>
                    <Table.Head>操作</Table.Head>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {jobs.map((job) => (
                    <Table.Row key={job.id}>
                      <Table.Cell><Text size="xs">{job.id}</Text></Table.Cell>
                      <Table.Cell>
                        <Text size="sm">{sourceLabel(job.options.source)}</Text>
                        {job.options.runId ? <Text size="xs" variant="secondary">run {job.options.runId}</Text> : null}
                      </Table.Cell>
                      <Table.Cell>
                        <Badge variant={job.status === "running" || job.status === "completed" ? "primary" : "secondary"}>
                          {statusLabel(job.status)}
                        </Badge>
                        {job.status === "queued" && job.queuePosition ? (
                          <Text size="xs" variant="secondary">队列第 {job.queuePosition} 位</Text>
                        ) : null}
                      </Table.Cell>
                      <Table.Cell>
                        <div className="min-w-40">
                          <Text size="sm">{job.done}/{job.total} · {job.progress}%</Text>
                          <progress className="w-full" max={100} value={job.progress} aria-label={`${job.id} 上传进度`} />
                          <Text size="xs" variant="secondary">成功 {job.counts.success} · 失败 {job.counts.failed} · 跳过 {job.counts.skipped}</Text>
                        </div>
                      </Table.Cell>
                      <Table.Cell>
                        <Text size="xs">每批 {job.options.batchSize}</Text>
                        <Text size="xs" variant="secondary">并发 {job.options.concurrency} · 重试 {job.options.retryLimit}</Text>
                      </Table.Cell>
                      <Table.Cell><Text size="xs">{formatWhen(job.createdAt)}</Text></Table.Cell>
                      <Table.Cell>
                        <div className="flex flex-wrap gap-2">
                          <Button size="sm" variant="secondary" onClick={() => openDetail(job)}>详情</Button>
                          {!TERMINAL.has(job.status) ? (
                            <Button size="sm" variant="secondary" loading={busy} onClick={() => void cancel(job)}>取消</Button>
                          ) : null}
                          {job.counts.failed > 0 && job.status === "completed" ? (
                            <Button size="sm" variant="secondary" loading={busy} onClick={() => void retryFailed(job)}>重试失败项</Button>
                          ) : null}
                        </div>
                      </Table.Cell>
                    </Table.Row>
                  ))}
                </Table.Body>
              </Table>
            </div>
          )}
        </LayerCard.Primary>
      </LayerCard>

      <Dialog.Root open={createOpen} onOpenChange={setCreateOpen}>
        <Dialog size="lg" className="flex max-h-[min(90vh,48rem)] flex-col p-6">
          <div className="mb-4 flex items-start justify-between gap-4">
            <div>
              <Dialog.Title className="text-xl font-semibold">新建上传任务</Dialog.Title>
              <Dialog.Description className="mt-1 text-kumo-subtle">支持多选 JSON / ZIP；空白参数沿用系统设置</Dialog.Description>
            </div>
            <Dialog.Close render={(props) => <Button {...props} size="sm" variant="secondary">关闭</Button>} />
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto">
            <div className="flex flex-col gap-4">
              <label className="flex flex-col gap-2">
                <Text size="sm">凭证文件</Text>
                <input type="file" multiple accept=".json,.zip,application/json,application/zip" onChange={(event) => setFiles(event.target.files)} />
                <Text size="xs" variant="secondary">{files?.length ? `已选择 ${files.length} 个文件` : "尚未选择文件"}</Text>
              </label>
              <div className="grid gap-3 sm:grid-cols-3">
                <Input label="并发数" value={concurrency} placeholder="系统默认" onChange={(event) => setConcurrency(event.target.value)} />
                <Input label="每批数量" value={batchSize} placeholder="系统默认" onChange={(event) => setBatchSize(event.target.value)} />
                <Input label="失败重试" value={retryLimit} placeholder="系统默认" onChange={(event) => setRetryLimit(event.target.value)} />
              </div>
              <Switch label="跳过本机已成功上传的相同凭证" checked={skipCached} onCheckedChange={(value) => setSkipCached(!!value)} />
            </div>
          </div>
          <div className="mt-6 flex justify-end gap-2">
            <Dialog.Close render={(props) => <Button {...props} size="sm" variant="secondary">取消</Button>} />
            <Button size="sm" loading={busy} onClick={() => void prepareAndStart()}>创建并入队</Button>
          </div>
        </Dialog>
      </Dialog.Root>

      <Dialog.Root open={detailOpen} onOpenChange={setDetailOpen}>
        <Dialog size="xl" className="flex max-h-[min(92vh,64rem)] flex-col p-6">
          <div className="mb-4 flex items-start justify-between gap-4">
            <div className="min-w-0">
              <Dialog.Title className="text-xl font-semibold">上传任务详情</Dialog.Title>
              <Dialog.Description className="mt-1 text-kumo-subtle">{selectedJob?.id || selectedID}</Dialog.Description>
            </div>
            <Dialog.Close render={(props) => <Button {...props} size="sm" variant="secondary">关闭</Button>} />
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto">
            {selectedJob ? (
              <div className="flex flex-col gap-5">
                <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-5">
                  <Stat label="状态" value={statusLabel(selectedJob.status)} />
                  <Stat label="进度" value={`${selectedJob.done}/${selectedJob.total} · ${selectedJob.progress}%`} />
                  <Stat label="成功" value={String(selectedJob.counts.success)} />
                  <Stat label="失败" value={String(selectedJob.counts.failed)} />
                  <Stat label="跳过" value={String(selectedJob.counts.skipped)} />
                </div>
                <div>
                  <Text size="sm">凭证明细</Text>
                  <div className="mt-2 max-h-72 overflow-auto rounded-md border border-kumo-hairline">
                    <Table>
                      <Table.Header><Table.Row><Table.Head>文件</Table.Head><Table.Head>状态</Table.Head><Table.Head>尝试</Table.Head><Table.Head>说明</Table.Head></Table.Row></Table.Header>
                      <Table.Body>
                        {(selectedJob.items || []).map((item) => (
                          <Table.Row key={item.id}>
                            <Table.Cell><Text size="xs">{item.name}</Text></Table.Cell>
                            <Table.Cell><Badge variant={item.status === "success" ? "primary" : "secondary"}>{item.status}</Badge></Table.Cell>
                            <Table.Cell><Text size="xs">{item.attempts}</Text></Table.Cell>
                            <Table.Cell><Text size="xs" variant="secondary">{item.error || (item.fromCache ? "本地缓存命中" : "—")}</Text></Table.Cell>
                          </Table.Row>
                        ))}
                      </Table.Body>
                    </Table>
                  </div>
                </div>
                <div>
                  <Text size="sm">实时日志</Text>
                  <pre className="mt-2 max-h-72 overflow-auto whitespace-pre-wrap rounded-md bg-kumo-contrast/5 p-3 text-xs">{(selectedJob.logs || []).join("\n") || "（暂无日志）"}</pre>
                </div>
              </div>
            ) : <Text variant="secondary">任务已过期或不存在</Text>}
          </div>
        </Dialog>
      </Dialog.Root>
    </AdminShell>
  );
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md border border-kumo-hairline p-3">
      <Text size="xs" variant="secondary">{label}</Text>
      <Text size="lg">{value}</Text>
    </div>
  );
}
