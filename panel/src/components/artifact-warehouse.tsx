"use client";

import {
  useCallback,
  useEffect,
  useRef,
  useState,
} from "react";
import {
  ArrowsClockwiseIcon,
  DownloadSimpleIcon,
  EyeIcon,
  MagnifyingGlassIcon,
} from "@phosphor-icons/react";
import {
  Badge,
  Button,
  Dialog,
  Input,
  LayerCard,
  Select,
  Table,
  Text,
} from "@cloudflare/kumo";
import { api, getToken, type ArtifactInfo } from "@/lib/api";

type ArtifactFacets = {
  plugins: string[];
  kinds: string[];
  channels: string[];
  statuses: string[];
};

type ArtifactsResponse = {
  ok: boolean;
  artifacts?: ArtifactInfo[];
  store?: string;
  total?: number;
  page?: number;
  limit?: number;
  total_pages?: number;
  facets?: ArtifactFacets;
  backfill?: {
    running: boolean;
    report?: { imported: number; updated: number; skipped: number; failed: number };
    error?: string;
  };
};

type ArtifactDetailResponse = {
  ok: boolean;
  artifact: ArtifactInfo;
};

const ALL_PLUGIN = "全部插件";
const ALL_KIND = "全部分类";
const ALL_CHANNEL = "全部渠道";
const ALL_STATUS = "全部状态";
const PAGE_SIZE = 50;
const MAX_SELECTION = 500;

function SelectionBox({
  checked,
  indeterminate = false,
  label,
  onChange,
}: {
  checked: boolean;
  indeterminate?: boolean;
  label: string;
  onChange: () => void;
}) {
  const ref = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (ref.current) ref.current.indeterminate = indeterminate;
  }, [indeterminate]);

  return (
    <input
      ref={ref}
      type="checkbox"
      className="h-4 w-4 cursor-pointer rounded border-kumo-hairline"
      checked={checked}
      aria-label={label}
      onChange={onChange}
    />
  );
}

function statusLabel(status: string) {
  switch (status) {
    case "fresh":
      return "待使用";
    case "healthy":
      return "可用";
    case "limited":
      return "受限";
    case "dead":
      return "失效";
    case "discarded":
      return "已丢弃";
    default:
      return status || "未知";
  }
}

function formatDate(value?: string) {
  if (!value) return { date: "—", time: "" };
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) return { date: value, time: "" };
  return {
    date: parsed.toLocaleDateString("zh-CN"),
    time: parsed.toLocaleTimeString("zh-CN", { hour12: false }),
  };
}

function triggerBlobDownload(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = filename;
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  window.setTimeout(() => URL.revokeObjectURL(url), 1000);
}

async function fetchDownload(path: string, init: RequestInit = {}) {
  const headers = new Headers(init.headers || {});
  const token = getToken();
  if (token) headers.set("X-Panel-Token", token);
  const response = await fetch(path, { ...init, headers });
  if (response.ok) return response;

  const text = await response.text();
  let message = text || "下载失败";
  try {
    const parsed = JSON.parse(text) as { error?: string };
    message = parsed.error || message;
  } catch {
    // Keep the response text when the server did not return JSON.
  }
  throw new Error(message);
}

export function ArtifactWarehouse({
  initialQuery = "",
  onQueryChange,
}: {
  initialQuery?: string;
  onQueryChange?: (query: string) => void;
}) {
  const [data, setData] = useState<ArtifactsResponse | null>(null);
  const [plugin, setPlugin] = useState(ALL_PLUGIN);
  const [kind, setKind] = useState(ALL_KIND);
  const [channel, setChannel] = useState(ALL_CHANNEL);
  const [status, setStatus] = useState(ALL_STATUS);
  const [searchInput, setSearchInput] = useState(initialQuery);
  const [query, setQuery] = useState(initialQuery);
  const [page, setPage] = useState(1);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [detail, setDetail] = useState<ArtifactInfo | null>(null);
  const [detailOpen, setDetailOpen] = useState(false);
  const [busy, setBusy] = useState("");
  const [message, setMessage] = useState("");
  const listRequest = useRef(0);
  const detailRequest = useRef(0);

  const load = useCallback(async () => {
    const request = ++listRequest.current;
    setBusy("load");
    setMessage("");
    try {
      const params = new URLSearchParams({
        page: String(page),
        limit: String(PAGE_SIZE),
      });
      if (plugin !== ALL_PLUGIN) params.set("plugin", plugin);
      if (kind !== ALL_KIND) params.set("kind", kind);
      if (channel !== ALL_CHANNEL) params.set("channel", channel);
      if (status !== ALL_STATUS) params.set("status", status);
      if (query) params.set("q", query);
      const response = await api<ArtifactsResponse>(
        `/api/artifacts?${params.toString()}`,
      );
      if (request !== listRequest.current) return;
      setData(response);
    } catch (error) {
      if (request !== listRequest.current) return;
      setMessage(error instanceof Error ? error.message : "加载产物失败");
    } finally {
      if (request === listRequest.current) setBusy("");
    }
  }, [channel, kind, page, plugin, query, status]);

  useEffect(() => {
    setSearchInput(initialQuery);
    setQuery(initialQuery);
    setPage(1);
  }, [initialQuery]);

  useEffect(() => {
    void load();
  }, [load]);

  useEffect(() => {
    if (!data?.backfill?.running) return;
    const timer = window.setTimeout(() => void load(), 1000);
    return () => window.clearTimeout(timer);
  }, [data?.backfill?.running, load]);

  const rows = data?.artifacts || [];
  const facets = data?.facets || {
    plugins: [],
    kinds: [],
    channels: [],
    statuses: [],
  };
  const total = data?.total || 0;
  const totalPages = data?.total_pages || 1;
  const currentIDs = rows.map((item) => item.id);
  const selectedOnPage = currentIDs.filter((id) => selected.has(id)).length;
  const allOnPageSelected = currentIDs.length > 0 && selectedOnPage === currentIDs.length;

  function setFilter(
    setter: (value: string) => void,
    value: string | null,
  ) {
    if (!value) return;
    setter(value);
    setPage(1);
  }

  function applySearch() {
    const nextQuery = searchInput.trim();
    setQuery(nextQuery);
    setPage(1);
    onQueryChange?.(nextQuery);
  }

  function toggleOne(id: string) {
    if (!selected.has(id) && selected.size >= MAX_SELECTION) {
      setMessage(`单次最多选择 ${MAX_SELECTION} 份产物`);
      return;
    }
    setSelected((current) => {
      const next = new Set(current);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }

  function togglePage() {
    if (!allOnPageSelected) {
      const adding = currentIDs.filter((id) => !selected.has(id)).length;
      if (selected.size+adding > MAX_SELECTION) {
        setMessage(`单次最多选择 ${MAX_SELECTION} 份产物，已选择可加入的前 ${Math.max(0, MAX_SELECTION-selected.size)} 份`);
      }
    }
    setSelected((current) => {
      const next = new Set(current);
      if (allOnPageSelected) currentIDs.forEach((id) => next.delete(id));
      else currentIDs.forEach((id) => {
        if (next.size < MAX_SELECTION) next.add(id);
      });
      return next;
    });
  }

  async function openDetail(id: string) {
    const request = ++detailRequest.current;
    setBusy(`detail:${id}`);
    setMessage("");
    try {
      const response = await api<ArtifactDetailResponse>(
        `/api/artifacts/${encodeURIComponent(id)}`,
      );
      if (request !== detailRequest.current) return;
      setDetail(response.artifact);
      setDetailOpen(true);
    } catch (error) {
      if (request !== detailRequest.current) return;
      setMessage(error instanceof Error ? error.message : "读取产物失败");
    } finally {
      if (request === detailRequest.current) setBusy("");
    }
  }

  async function downloadOne(item: ArtifactInfo) {
    setBusy(`download:${item.id}`);
    setMessage("");
    try {
      const response = await fetchDownload(
        `/api/artifacts/${encodeURIComponent(item.id)}/download`,
      );
      triggerBlobDownload(await response.blob(), item.filename || `${item.id}.json`);
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "下载产物失败");
    } finally {
      setBusy("");
    }
  }

  async function downloadSelected() {
    if (selected.size === 0) return;
    setBusy("download");
    setMessage("");
    try {
      const response = await fetchDownload("/api/artifacts/download", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ ids: Array.from(selected) }),
      });
      triggerBlobDownload(await response.blob(), "artifacts.zip");
      setMessage(`已打包 ${selected.size} 份产物`);
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "批量下载失败");
    } finally {
      setBusy("");
    }
  }

  const detailTime = formatDate(detail?.created_at);

  return (
    <>
      {message ? (
        <div className="mb-3 rounded-md bg-kumo-contrast/5 px-3 py-2">
          <Text size="sm">{message}</Text>
        </div>
      ) : null}

      <LayerCard>
        <LayerCard.Secondary>
          <div className="flex w-full flex-wrap items-center justify-between gap-2">
            <span>原始产物 · {total}</span>
            <div className="flex flex-wrap gap-2">
              {selected.size > 0 ? (
                <Button
                  size="sm"
                  variant="secondary"
                  onClick={() => setSelected(new Set())}
                >
                  取消选择
                </Button>
              ) : null}
              <Button
                size="sm"
                variant="secondary"
                loading={busy === "load"}
                disabled={busy !== "" && busy !== "load"}
                onClick={() => void load()}
              >
                <ArrowsClockwiseIcon size={16} /> 刷新
              </Button>
              <Button
                size="sm"
                loading={busy === "download"}
                disabled={selected.size === 0 || busy !== ""}
                onClick={() => void downloadSelected()}
              >
                <DownloadSimpleIcon size={16} />
                批量下载{selected.size ? ` · ${selected.size}` : ""}
              </Button>
            </div>
          </div>
        </LayerCard.Secondary>
        <LayerCard.Primary className="p-0">
          <div className="border-b border-kumo-hairline p-4">
            <div className="flex flex-wrap items-end gap-3">
              <div className="min-w-56 flex-1">
                <Input
                  label="搜索"
                  value={searchInput}
                  placeholder="ID、邮箱、账号、文件名"
                  onChange={(event) => setSearchInput(event.target.value)}
                  onKeyDown={(event) => {
                    if (event.key === "Enter") applySearch();
                  }}
                />
              </div>
              <Button
                size="sm"
                variant="secondary"
                aria-label="搜索产物"
                onClick={applySearch}
              >
                <MagnifyingGlassIcon size={16} /> 搜索
              </Button>
              <div className="min-w-36">
                <Select
                  label="分类"
                  value={kind}
                  onValueChange={(value) => setFilter(setKind, value)}
                >
                  <Select.Option value={ALL_KIND}>{ALL_KIND}</Select.Option>
                  {facets.kinds.map((value) => (
                    <Select.Option key={value} value={value}>{value}</Select.Option>
                  ))}
                </Select>
              </div>
              <div className="min-w-36">
                <Select
                  label="渠道"
                  value={channel}
                  onValueChange={(value) => setFilter(setChannel, value)}
                >
                  <Select.Option value={ALL_CHANNEL}>{ALL_CHANNEL}</Select.Option>
                  {facets.channels.map((value) => (
                    <Select.Option key={value} value={value}>{value}</Select.Option>
                  ))}
                </Select>
              </div>
              <div className="min-w-40">
                <Select
                  label="插件"
                  value={plugin}
                  onValueChange={(value) => setFilter(setPlugin, value)}
                >
                  <Select.Option value={ALL_PLUGIN}>{ALL_PLUGIN}</Select.Option>
                  {facets.plugins.map((value) => (
                    <Select.Option key={value} value={value}>{value}</Select.Option>
                  ))}
                </Select>
              </div>
              <div className="min-w-32">
                <Select
                  label="状态"
                  value={status}
                  onValueChange={(value) => setFilter(setStatus, value)}
                >
                  <Select.Option value={ALL_STATUS}>{ALL_STATUS}</Select.Option>
                  {facets.statuses.map((value) => (
                    <Select.Option key={value} value={value}>
                      {statusLabel(value)}
                    </Select.Option>
                  ))}
                </Select>
              </div>
            </div>
            <div className="mt-3 flex flex-wrap items-center justify-between gap-2">
              <Text size="xs" variant="secondary">
                {query ? `搜索“${query}” · ` : ""}第 {page}/{totalPages} 页
              </Text>
              <Text size="xs" variant="secondary">
                {data?.backfill?.running
                  ? "正在整理历史凭证…"
                  : data?.backfill?.error
                    ? `历史凭证整理失败：${data.backfill.error}`
                    : `数据目录：${data?.store || "—"}`}
              </Text>
            </div>
          </div>

          {rows.length === 0 ? (
            <div className="px-4 py-12 text-center">
              <Text variant="secondary">
                {total === 0 && !query ? "尚无插件产物，完成注册或密钥入库后会自动归档" : "没有符合当前筛选条件的产物"}
              </Text>
            </div>
          ) : (
            <div className="overflow-x-auto">
              <Table className="min-w-[1180px]">
                <Table.Header>
                  <Table.Row>
                    <Table.Head>
                      <SelectionBox
                        checked={allOnPageSelected}
                        indeterminate={selectedOnPage > 0 && !allOnPageSelected}
                        label="选择当前页全部产物"
                        onChange={togglePage}
                      />
                    </Table.Head>
                    <Table.Head>编号</Table.Head>
                    <Table.Head>产物 ID</Table.Head>
                    <Table.Head>分类</Table.Head>
                    <Table.Head>邮箱 / 账号</Table.Head>
                    <Table.Head>渠道</Table.Head>
                    <Table.Head>插件</Table.Head>
                    <Table.Head>状态</Table.Head>
                    <Table.Head>时间</Table.Head>
                    <Table.Head>操作</Table.Head>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {rows.map((item, index) => {
                    const time = formatDate(item.created_at);
                    return (
                      <Table.Row key={item.id}>
                        <Table.Cell>
                          <SelectionBox
                            checked={selected.has(item.id)}
                            label={`选择产物 ${item.id}`}
                            onChange={() => toggleOne(item.id)}
                          />
                        </Table.Cell>
                        <Table.Cell>
                          <Text size="xs">{(page - 1) * PAGE_SIZE + index + 1}</Text>
                        </Table.Cell>
                        <Table.Cell>
                          <div className="max-w-40">
                            <Text size="xs">{item.id}</Text>
                            <Text size="xs" variant="secondary">{item.filename}</Text>
                          </div>
                        </Table.Cell>
                        <Table.Cell>
                          <Badge variant="secondary">{item.kind}</Badge>
                        </Table.Cell>
                        <Table.Cell>
                          <div className="max-w-64">
                            <Text size="sm">{item.email || "—"}</Text>
                            {item.account ? (
                              <Text size="xs" variant="secondary">{item.account}</Text>
                            ) : null}
                          </div>
                        </Table.Cell>
                        <Table.Cell>
                          <Badge variant="secondary">{item.channel || "—"}</Badge>
                        </Table.Cell>
                        <Table.Cell>
                          <Text size="xs">{item.plugin}</Text>
                        </Table.Cell>
                        <Table.Cell>
                          <Badge variant={item.status === "healthy" || item.status === "fresh" ? "primary" : "secondary"}>
                            {statusLabel(item.status)}
                          </Badge>
                        </Table.Cell>
                        <Table.Cell>
                          <Text size="xs">{time.date}</Text>
                          <Text size="xs" variant="secondary">{time.time}</Text>
                        </Table.Cell>
                        <Table.Cell>
                          <div className="flex gap-2">
                            <Button
                              size="sm"
                              shape="square"
                              variant="secondary"
                              icon={EyeIcon}
                              title="查看详情"
                              aria-label={`查看产物 ${item.id}`}
                              loading={busy === `detail:${item.id}`}
                              onClick={() => void openDetail(item.id)}
                            />
                            <Button
                              size="sm"
                              shape="square"
                              variant="secondary"
                              icon={DownloadSimpleIcon}
                              title="下载 JSON"
                              aria-label={`下载产物 ${item.id}`}
                              loading={busy === `download:${item.id}`}
                              disabled={busy !== "" && busy !== `download:${item.id}`}
                              onClick={() => void downloadOne(item)}
                            />
                          </div>
                        </Table.Cell>
                      </Table.Row>
                    );
                  })}
                </Table.Body>
              </Table>
            </div>
          )}

          <div className="flex flex-wrap items-center justify-between gap-3 border-t border-kumo-hairline px-4 py-3">
            <Text size="xs" variant="secondary">
              共 {total} 份 · 每页 {PAGE_SIZE} 份
              {selected.size ? ` · 已选择 ${selected.size} 份` : ""}
            </Text>
            <div className="flex gap-2">
              <Button
                size="sm"
                variant="secondary"
                disabled={page <= 1 || busy === "load"}
                onClick={() => setPage(1)}
              >
                首页
              </Button>
              <Button
                size="sm"
                variant="secondary"
                disabled={page <= 1 || busy === "load"}
                onClick={() => setPage((current) => Math.max(1, current - 1))}
              >
                上一页
              </Button>
              <Button
                size="sm"
                variant="secondary"
                disabled={page >= totalPages || busy === "load"}
                onClick={() => setPage((current) => Math.min(totalPages, current + 1))}
              >
                下一页
              </Button>
              <Button
                size="sm"
                variant="secondary"
                disabled={page >= totalPages || busy === "load"}
                onClick={() => setPage(totalPages)}
              >
                末页
              </Button>
            </div>
          </div>
        </LayerCard.Primary>
      </LayerCard>

      <Dialog.Root
        open={detailOpen}
        onOpenChange={(open) => {
          setDetailOpen(!!open);
          if (!open) setDetail(null);
        }}
      >
        <Dialog size="lg" className="flex max-h-[min(90vh,56rem)] flex-col p-6">
          <div className="mb-4">
            <Dialog.Title className="text-xl font-semibold">产物详情</Dialog.Title>
            <Dialog.Description className="mt-1 text-kumo-subtle">
              {detail?.id || "读取中"}
            </Dialog.Description>
          </div>
          {detail ? (
            <div className="min-h-0 flex-1 overflow-y-auto">
              <div className="grid gap-x-6 gap-y-3 border-b border-kumo-hairline pb-4 sm:grid-cols-2">
                {[
                  ["分类", detail.kind],
                  ["状态", statusLabel(detail.status)],
                  ["邮箱", detail.email || "—"],
                  ["账号", detail.account || "—"],
                  ["渠道", detail.channel || "—"],
                  ["插件", detail.plugin],
                  ["来源文件", detail.filename],
                  ["创建时间", `${detailTime.date} ${detailTime.time}`],
                  ["运行 ID", detail.run_id || "—"],
                ].map(([label, value]) => (
                  <div key={label}>
                    <Text size="xs" variant="secondary">{label}</Text>
                    <Text size="sm">{value}</Text>
                  </div>
                ))}
              </div>
              <div className="mt-4">
                <Text size="sm">标签</Text>
                <pre className="mt-2 max-h-40 overflow-auto rounded-md bg-kumo-contrast/5 p-3 text-xs whitespace-pre-wrap break-all">
                  {JSON.stringify(detail.labels || {}, null, 2)}
                </pre>
              </div>
              <div className="mt-4">
                <Text size="sm">原始内容</Text>
                <Text size="xs" variant="secondary">可能包含敏感凭证，仅在当前管理端显示</Text>
                <pre className="mt-2 max-h-80 overflow-auto rounded-md bg-kumo-contrast/5 p-3 text-xs whitespace-pre-wrap break-all">
                  {JSON.stringify(detail.payload ?? {}, null, 2)}
                </pre>
              </div>
            </div>
          ) : null}
          <div className="mt-6 flex justify-end gap-2">
            {detail ? (
              <Button
                size="sm"
                variant="secondary"
                loading={busy === `download:${detail.id}`}
                onClick={() => void downloadOne(detail)}
              >
                <DownloadSimpleIcon size={16} /> 下载 JSON
              </Button>
            ) : null}
            <Dialog.Close
              render={(props) => (
                <Button {...props} size="sm" variant="secondary">关闭</Button>
              )}
            />
          </div>
        </Dialog>
      </Dialog.Root>
    </>
  );
}
