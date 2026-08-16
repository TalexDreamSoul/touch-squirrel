"use client";

import { Suspense, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import {
  ArrowsClockwiseIcon,
  DownloadSimpleIcon,
  EyeIcon,
  MagnifyingGlassIcon,
  PowerIcon,
  ProhibitIcon,
  TrashIcon,
  UploadSimpleIcon,
} from "@phosphor-icons/react";
import {
  Badge,
  Button,
  Input,
  LayerCard,
  Select,
  Table,
  Tabs,
  Text,
} from "@cloudflare/kumo";
import { AdminShell } from "@/components/admin-shell";
import { ArtifactWarehouse } from "@/components/artifact-warehouse";
import { PageHeader } from "@/components/page-header";
import { api, getToken, type ClusterStatus } from "@/lib/api";

type LocalPoolItem = {
  name: string;
  email?: string;
  source_run?: string;
  size: number;
  added_at: string;
  synced_at?: string;
  sync_error?: string;
};

type AccountItem = {
  id: string;
  type: string;
  plugin: string;
  label: string;
  status: string;
  email?: string;
  external_id?: string;
  source?: string;
  run_id?: string;
  created_at?: string;
  updated_at?: string;
  last_used_at?: string;
};

type CloudPoolItem = {
  name: string;
  provider?: string;
  type?: string;
  category?: string;
  status?: string;
  status_message?: string;
  email?: string;
  disabled?: boolean;
  size?: number;
  success?: number;
  failed?: number;
};

type Overview = {
  overview?: {
    healthy: number;
    rate_limited: number;
    dead: number;
    disabled: number;
    total: number;
    quota_estimate: number;
  };
  cleanup?: {
    enabled: boolean;
    dry_run: boolean;
    last_reason?: string;
    last?: {
      scanned?: number;
      quota_hits?: number;
      deleted?: number;
      would_delete?: number;
    };
  };
};

type CredentialRow = {
  key: string;
  id: string;
  filename?: string;
  type: string;
  email?: string;
  account?: string;
  source: string;
  plugin?: string;
  channel?: string;
  status: string;
  statusDetail?: string;
  time?: string;
  stats?: string;
  artifactQuery?: string;
  downloadName?: string;
};

type PoolCapabilities = {
  enable: boolean;
  disable: boolean;
  upload_cpa: boolean;
  download: boolean;
  delete: boolean;
  time_filter: boolean;
};

type PoolBatchResult = {
  id: string;
  ok: boolean;
  error?: string;
};

type PoolBatchResponse = {
  ok: boolean;
  total: number;
  succeeded: number;
  failed: number;
  results: PoolBatchResult[];
  queued?: number;
  job_id?: string;
};

type TimeField = "created_at" | "updated_at" | "last_used_at";
type TimeRange = "all" | "24h" | "7d" | "30d" | "90d";

type PoolSource = "accounts" | "local" | "cloud" | "federation";
type TabKey = "list" | "artifacts" | "patrol";

const PAGE_SIZE = 50;
const MAX_SELECTION = 500;
const DEFAULT_CAPABILITIES: Record<PoolSource, PoolCapabilities> = {
  accounts: { enable: true, disable: true, upload_cpa: true, download: true, delete: true, time_filter: true },
  local: { enable: false, disable: false, upload_cpa: true, download: true, delete: true, time_filter: false },
  cloud: { enable: false, disable: false, upload_cpa: false, download: true, delete: true, time_filter: false },
  federation: { enable: false, disable: false, upload_cpa: false, download: false, delete: false, time_filter: false },
};
const ALL_TYPE = "全部分类";
const ALL_STATUS = "全部状态";
const ALL_SYNC = "全部上传状态";
const POOL_SOURCE_LABELS: Record<PoolSource, string> = {
  accounts: "统一号池",
  local: "本地 xAI 文件",
  cloud: "云端 CPA",
  federation: "联邦主节点",
};

function SelectionBox({
  checked,
  indeterminate = false,
  label,
  disabled = false,
  onChange,
}: {
  checked: boolean;
  indeterminate?: boolean;
  label: string;
  disabled?: boolean;
  onChange: () => void;
}) {
  const ref = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (ref.current) ref.current.indeterminate = indeterminate;
  }, [indeterminate]);

  return (
    <label className={`inline-flex h-11 w-11 items-center justify-center -m-3 ${disabled ? "cursor-not-allowed" : "cursor-pointer"}`}>
      <input
        ref={ref}
        type="checkbox"
        className="h-4 w-4 cursor-pointer rounded border-kumo-hairline disabled:cursor-not-allowed"
        checked={checked}
        disabled={disabled}
        aria-label={label}
        onChange={onChange}
      />
    </label>
  );
}

function timeRangeStart(range: TimeRange) {
  if (range === "all") return "";
  const hours = range === "24h" ? 24 : Number.parseInt(range, 10) * 24;
  return new Date(Date.now() - hours * 60 * 60 * 1000).toISOString();
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

async function fetchPoolDownload(body: Record<string, unknown>) {
  const headers = new Headers({ "Content-Type": "application/json" });
  const token = getToken();
  if (token) headers.set("X-Panel-Token", token);
  const response = await fetch("/api/pool/batch/download", {
    method: "POST",
    headers,
    body: JSON.stringify(body),
  });
  if (response.ok) return response;
  const text = await response.text();
  let message = text || "下载失败";
  try {
    const parsed = JSON.parse(text) as { error?: string; succeeded?: number; failed?: number };
    message = parsed.error || message;
    if (parsed.failed) {
      message += `（成功 ${parsed.succeeded || 0}，失败 ${parsed.failed}；未生成下载包）`;
    }
  } catch {
    // Keep the raw response when the server did not return JSON.
  }
  throw new Error(message);
}

function credentialStatusLabel(status: string) {
  switch (status.toLowerCase()) {
    case "active":
      return "可用";
    case "disabled":
      return "已停用";
    case "exhausted":
      return "已耗尽";
    case "unknown":
      return "未知";
    case "synced":
      return "已上传";
    case "unsynced":
      return "未上传";
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

function Pager({
  page,
  totalPages,
  total,
  label,
  busy = false,
  onChange,
}: {
  page: number;
  totalPages: number;
  total: number;
  label: string;
  busy?: boolean;
  onChange: (p: number) => void;
}) {
  if (total <= 0) return null;
  const pages = Math.max(1, totalPages);
  return (
    <div className="flex flex-wrap items-center justify-between gap-3 border-t border-kumo-hairline px-4 py-3">
      <Text size="xs" variant="secondary">
        {label} · 共 {total} 条 · 每页 {PAGE_SIZE} 条 · 第 {page}/{pages} 页
      </Text>
      <div className="flex flex-wrap gap-2">
        <Button size="sm" variant="secondary" disabled={busy || page <= 1} onClick={() => onChange(1)}>
          首页
        </Button>
        <Button
          size="sm"
          variant="secondary"
          disabled={busy || page <= 1}
          onClick={() => onChange(page - 1)}
        >
          上一页
        </Button>
        <Button
          size="sm"
          variant="secondary"
          disabled={busy || page >= pages}
          onClick={() => onChange(page + 1)}
        >
          下一页
        </Button>
        <Button
          size="sm"
          variant="secondary"
          disabled={busy || page >= pages}
          onClick={() => onChange(pages)}
        >
          末页
        </Button>
      </div>
    </div>
  );
}

export default function PoolPage() {
  return (
    <Suspense fallback={null}>
      <PoolContent />
    </Suspense>
  );
}

function PoolContent() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const requestedTab = searchParams.get("tab");
  const tab: TabKey = requestedTab === "artifacts" || requestedTab === "patrol" ? requestedTab : "list";
  const artifactQuery = searchParams.get("q") || "";
  const [poolSource, setPoolSource] = useState<PoolSource>("accounts");
  const [accountType, setAccountType] = useState<string>(ALL_TYPE);
  const [accountStatus, setAccountStatus] = useState<string>(ALL_STATUS);
  const [syncStatus, setSyncStatus] = useState<string>(ALL_SYNC);
  const [credentialSearchInput, setCredentialSearchInput] = useState("");
  const [credentialQuery, setCredentialQuery] = useState("");
  const [timeField, setTimeField] = useState<TimeField>("created_at");
  const [timeRange, setTimeRange] = useState<TimeRange>("all");
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [capabilities, setCapabilities] = useState<PoolCapabilities>(DEFAULT_CAPABILITIES.accounts);
  const [batchBusy, setBatchBusy] = useState("");
  const [listBusy, setListBusy] = useState(false);
  const [masterURL, setMasterURL] = useState("");
  const [masters, setMasters] = useState<string[]>([]);
  const [localItems, setLocalItems] = useState<LocalPoolItem[]>([]);
  const [accountItems, setAccountItems] = useState<AccountItem[]>([]);
  const [typeCounts, setTypeCounts] = useState<Record<string, number>>({});
  const [availableTypes, setAvailableTypes] = useState<string[]>([]);
  const [cloudItems, setCloudItems] = useState<CloudPoolItem[]>([]);
  const [poolTotal, setPoolTotal] = useState(0);
  const [poolTotalPages, setPoolTotalPages] = useState(0);
  const [poolPage, setPoolPage] = useState(1);
  const [poolUnsynced, setPoolUnsynced] = useState(0);
  const [poolSynced, setPoolSynced] = useState(0);
  const [fedCanPull, setFedCanPull] = useState(false);
  const [fedShareList, setFedShareList] = useState(true);
  const [poolError, setPoolError] = useState("");
  const [msg, setMsg] = useState("");
  const [busy, setBusy] = useState(false);
  const poolRequest = useRef(0);
  const viewKey = [tab, poolSource, masterURL, poolPage, accountType, accountStatus, syncStatus, credentialQuery, timeField, timeRange].join("|");
  const viewKeyRef = useRef(viewKey);
  viewKeyRef.current = viewKey;

  const [ov, setOv] = useState<Overview | null>(null);
  const [patrolLogs, setPatrolLogs] = useState("");

  const refreshMasters = useCallback(async () => {
    try {
      const [cfg, cl] = await Promise.all([
        api<{ config?: Record<string, unknown> }>("/api/config").catch(() => ({
          config: {} as Record<string, unknown>,
        })),
        api<{ cluster?: ClusterStatus }>("/api/cluster/status").catch(() => ({
          cluster: undefined,
        })),
      ]);
      const conf = cfg.config || {};
      const masterList =
        cl.cluster?.masters?.filter(Boolean) ||
        String(conf.cluster_master_urls || conf.cluster_master_url || "")
          .split(/[\n,;\s]+/)
          .map((s) => s.trim().replace(/\/$/, ""))
          .filter(Boolean);
      setMasters(masterList);
      setMasterURL((prev) => prev || masterList[0] || "");
    } catch {
      /* ignore */
    }
  }, []);

  const refreshPool = useCallback(
    async (
      page: number,
      source: PoolSource,
      master: string,
      typeFilter: string,
      statusFilter: string,
      syncFilter: string,
      queryFilter: string,
      timeFieldFilter: TimeField,
      timeRangeFilter: TimeRange,
    ) => {
      const request = ++poolRequest.current;
      setListBusy(true);
      setPoolError("");
      try {
        if (source === "accounts") {
          const qs = new URLSearchParams({
            source: "accounts",
            page: String(page),
            limit: String(PAGE_SIZE),
          });
          if (typeFilter && typeFilter !== ALL_TYPE) qs.set("type", typeFilter);
          if (statusFilter && statusFilter !== ALL_STATUS) qs.set("status", statusFilter);
          if (queryFilter.trim()) qs.set("q", queryFilter.trim());
          qs.set("time_field", timeFieldFilter);
          const from = timeRangeStart(timeRangeFilter);
          if (from) qs.set("from", from);
          const ap = await api<{
            items?: AccountItem[];
            total?: number;
            total_pages?: number;
            by_type?: Record<string, number>;
            types?: string[];
            capabilities?: PoolCapabilities;
          }>(`/api/pool/list?${qs.toString()}`);
          if (request !== poolRequest.current) return;
          setAccountItems(ap.items || []);
          setLocalItems([]);
          setCloudItems([]);
          setPoolTotal(ap.total || 0);
          setPoolUnsynced(0);
          setPoolTotalPages(ap.total_pages || 0);
          setTypeCounts(ap.by_type || {});
          setAvailableTypes(ap.types || Object.keys(ap.by_type || {}));
          setCapabilities(ap.capabilities || DEFAULT_CAPABILITIES.accounts);
          setFedCanPull(false);
          setFedShareList(true);
          return;
        }
        if (source === "local") {
          const qs = new URLSearchParams({ source: "local", page: String(page), limit: String(PAGE_SIZE) });
          if (syncFilter && syncFilter !== ALL_SYNC) qs.set("sync_status", syncFilter);
          const lp = await api<{
            items?: LocalPoolItem[];
            total?: number;
            synced?: number;
            unsynced?: number;
            total_pages?: number;
            capabilities?: PoolCapabilities;
          }>(`/api/pool/list?${qs.toString()}`);
          if (request !== poolRequest.current) return;
          setLocalItems(lp.items || []);
          setAccountItems([]);
          setCloudItems([]);
          setPoolTotal(lp.total || 0);
          setPoolSynced(lp.synced || 0);
          setPoolUnsynced(lp.unsynced || 0);
          setPoolTotalPages(lp.total_pages || 0);
          setTypeCounts({});
          setAvailableTypes([]);
          setCapabilities(lp.capabilities || DEFAULT_CAPABILITIES.local);
          setFedCanPull(false);
          setFedShareList(true);
          return;
        }
        if (source === "cloud") {
          const qs = new URLSearchParams({ source: "cloud", page: String(page), limit: String(PAGE_SIZE) });
          if (typeFilter && typeFilter !== ALL_TYPE) qs.set("category", typeFilter);
          if (statusFilter && statusFilter !== ALL_STATUS) qs.set("status", statusFilter);
          if (queryFilter.trim()) qs.set("q", queryFilter.trim());
          const cp = await api<{
            files?: CloudPoolItem[]; total?: number; total_pages?: number; can_pull?: boolean;
            categories?: string[]; by_category?: Record<string, number>; capabilities?: PoolCapabilities;
          }>(`/api/pool/list?${qs.toString()}`);
          if (request !== poolRequest.current) return;
          setCloudItems(cp.files || []);
          setLocalItems([]);
          setAccountItems([]);
          setPoolTotal(cp.total || 0);
          setPoolTotalPages(cp.total_pages || 0);
          setPoolSynced(0);
          setPoolUnsynced(0);
          setAvailableTypes(cp.categories || []);
          setTypeCounts(cp.by_category || {});
          setFedCanPull(cp.can_pull !== false);
          setCapabilities(cp.capabilities || DEFAULT_CAPABILITIES.cloud);
          setFedShareList(true);
          return;
        }
        if (!master) {
          setLocalItems([]);
          setAccountItems([]);
          setCloudItems([]);
          setPoolTotal(0);
          setPoolTotalPages(0);
          setCapabilities(DEFAULT_CAPABILITIES.federation);
          setPoolError("请选择联邦主节点");
          return;
        }
        const qs = new URLSearchParams({
          source: "federation", master, page: String(page), limit: String(PAGE_SIZE),
        });
        if (typeFilter && typeFilter !== ALL_TYPE) qs.set("category", typeFilter);
        if (statusFilter && statusFilter !== ALL_STATUS) qs.set("status", statusFilter);
        if (queryFilter.trim()) qs.set("q", queryFilter.trim());
        const fp = await api<{
          files?: CloudPoolItem[]; total?: number; total_pages?: number;
          share_pool_list?: boolean; share_pool_pull?: boolean;
          categories?: string[]; by_category?: Record<string, number>; capabilities?: PoolCapabilities;
        }>(`/api/pool/list?${qs.toString()}`);
        if (request !== poolRequest.current) return;
        setCloudItems(fp.files || []);
        setLocalItems([]);
        setAccountItems([]);
        setPoolTotal(fp.total || 0);
        setPoolTotalPages(fp.total_pages || 0);
        setPoolSynced(0);
        setPoolUnsynced(0);
        setAvailableTypes(fp.categories || []);
        setTypeCounts(fp.by_category || {});
        setFedShareList(fp.share_pool_list !== false);
        setFedCanPull(!!fp.share_pool_pull);
        setCapabilities(fp.capabilities || {
          ...DEFAULT_CAPABILITIES.federation,
          download: !!fp.share_pool_pull,
          upload_cpa: !!fp.share_pool_pull,
        });
      } catch (e) {
        if (request !== poolRequest.current) return;
        setLocalItems([]);
        setAccountItems([]);
        setCloudItems([]);
        setPoolTotal(0);
        setPoolTotalPages(0);
        setCapabilities(DEFAULT_CAPABILITIES[source]);
        setPoolError(e instanceof Error ? e.message : "加载号池失败");
      } finally {
        if (request === poolRequest.current) setListBusy(false);
      }
    },
    [],
  );

  const loadPatrol = useCallback(async () => {
    const [o, l] = await Promise.all([
      api<Overview>("/api/pool/overview"),
      api<{ text?: string; lines?: string[] }>("/api/pool/logs?tail=200"),
    ]);
    setOv(o);
    setPatrolLogs(l.text || (l.lines || []).join("\n"));
  }, []);

  useEffect(() => {
    void refreshMasters();
  }, [refreshMasters]);

  useEffect(() => {
    if (tab !== "list") return;
    void refreshPool(
      poolPage,
      poolSource,
      masterURL,
      accountType,
      accountStatus,
      syncStatus,
      credentialQuery,
      timeField,
      timeRange,
    );
  }, [
    tab,
    poolPage,
    poolSource,
    masterURL,
    accountType,
    accountStatus,
    syncStatus,
    credentialQuery,
    timeField,
    timeRange,
    refreshPool,
  ]);

  useEffect(() => {
    setSelected(new Set());
  }, [poolSource, masterURL, accountType, accountStatus, syncStatus, credentialQuery, timeField, timeRange]);

  useEffect(() => {
    if (tab !== "patrol") return;
    void loadPatrol().catch((e: unknown) =>
      setMsg(e instanceof Error ? e.message : "加载巡检失败"),
    );
  }, [tab, loadPatrol]);

  async function syncLocal() {
    setBusy(true);
    try {
      const d = await api<{ queued?: number; total?: number; job_id?: string }>(
        "/api/local-pool/sync",
        { method: "POST", body: JSON.stringify({ all: false }) },
      );
      setMsg(
        d.queued
          ? `已将 ${d.queued} 条未上传凭证加入队列 · ${d.job_id || "上传任务"}`
          : "没有待上传凭证",
      );
      await refreshPool(
        poolPage,
        "local",
        masterURL,
        accountType,
        accountStatus,
        syncStatus,
        credentialQuery,
        timeField,
        timeRange,
      );
    } catch (e) {
      setMsg(e instanceof Error ? e.message : "同步失败");
    } finally {
      setBusy(false);
    }
  }

  async function importLatest() {
    setBusy(true);
    try {
      const d = await api<{ added?: number; run_id?: string }>(
        "/api/local-pool/import",
        { method: "POST", body: JSON.stringify({}) },
      );
      setMsg(`已入库 ${d.added ?? 0} 个（run ${d.run_id || "latest"}）`);
      setPoolSource("local");
      setPoolPage(1);
      await refreshPool(
        1,
        "local",
        masterURL,
        accountType,
        accountStatus,
        syncStatus,
        credentialQuery,
        timeField,
        timeRange,
      );
    } catch (e) {
      setMsg(e instanceof Error ? e.message : "入库失败");
    } finally {
      setBusy(false);
    }
  }

  async function patrol(mode: "light" | "deep") {
    setBusy(true);
    try {
      await api("/api/pool/patrol", {
        method: "POST",
        body: JSON.stringify({ mode }),
      });
      setMsg(`${mode} 巡检已触发`);
      setTimeout(() => void loadPatrol(), 1500);
    } catch (e) {
      setMsg(e instanceof Error ? e.message : "巡检失败");
    } finally {
      setBusy(false);
    }
  }

  async function cleanup() {
    if (!confirm("清理限额耗尽号？（纯 429 保留；演练模式只报告）")) return;
    setBusy(true);
    try {
      const d = await api<{ result?: { reason?: string } }>("/api/pool/cleanup", {
        method: "POST",
        body: JSON.stringify({ force: true }),
      });
      setMsg(d.result?.reason || "清理完成");
      await loadPatrol();
    } catch (e) {
      setMsg(e instanceof Error ? e.message : "清理失败");
    } finally {
      setBusy(false);
    }
  }

  function activateTab(next: TabKey, artifactFilter = "") {
    const params = new URLSearchParams();
    if (next !== "list") params.set("tab", next);
    if (next === "artifacts" && artifactFilter) params.set("q", artifactFilter);
    const suffix = params.toString();
    router.replace(suffix ? `/pool/?${suffix}` : "/pool/", { scroll: false });
  }

  function beginPoolViewChange() {
    setListBusy(true);
    setSelected(new Set());
  }

  function applyCredentialSearch() {
    beginPoolViewChange();
    setCredentialQuery(credentialSearchInput.trim());
    setPoolPage(1);
  }

  const credentialRows = useMemo<CredentialRow[]>(() => {
    if (poolSource === "accounts") {
      return accountItems.map((item) => ({
        key: item.id,
        id: item.id,
        filename: item.external_id,
        type: item.type,
        email: item.email,
        account:
          item.label && item.label !== item.email && item.label !== item.external_id
            ? item.label
            : undefined,
        source: item.source || "本地索引",
        plugin: item.plugin,
        channel: item.run_id ? `run ${item.run_id}` : undefined,
        status: item.status,
        time:
          timeField === "created_at"
            ? item.created_at
            : timeField === "updated_at"
              ? item.updated_at
              : item.last_used_at,
        artifactQuery: item.external_id || item.email || item.id,
      }));
    }
    if (poolSource === "local") {
      return localItems.map((item) => ({
        key: item.name,
        id: item.name,
        type: "xai",
        email: item.email,
        source: "本地文件",
        plugin: "xai-accounts",
        channel: item.source_run ? `run ${item.source_run}` : undefined,
        status: item.synced_at ? "synced" : "unsynced",
        statusDetail: item.sync_error,
        time: item.synced_at || item.added_at,
        stats: item.size ? `${item.size.toLocaleString("zh-CN")} B` : undefined,
        artifactQuery: item.name,
      }));
    }
    return cloudItems.map((item) => ({
      key: `${poolSource}:${item.name}`,
      id: item.name,
      type: item.category || item.type || item.provider || "CPA",
      email: item.email,
      source: poolSource === "cloud" ? "云端 CPA" : "联邦主节点",
      plugin: item.provider,
      status: item.disabled ? "disabled" : item.status || "active",
      statusDetail: item.status_message,
      stats:
        item.success != null || item.failed != null
          ? `成功 ${item.success || 0} · 失败 ${item.failed || 0}`
          : item.size
            ? `${item.size.toLocaleString("zh-CN")} B`
            : undefined,
      downloadName:
        (poolSource === "cloud" || fedCanPull) && item.name
          ? item.name
          : undefined,
    }));
  }, [accountItems, cloudItems, fedCanPull, localItems, poolSource, timeField]);

  const currentIDs = useMemo(() => credentialRows.map((row) => row.id), [credentialRows]);
  const selectedOnPage = currentIDs.filter((id) => selected.has(id)).length;
  const allOnPageSelected = currentIDs.length > 0 && selectedOnPage === currentIDs.length;

  function toggleCredential(id: string) {
    if (!selected.has(id) && selected.size >= MAX_SELECTION) {
      setMsg(`单次最多选择 ${MAX_SELECTION} 条凭证`);
      return;
    }
    setSelected((current) => {
      const next = new Set(current);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }

  function toggleCredentialPage() {
    if (!allOnPageSelected) {
      const available = MAX_SELECTION - selected.size;
      const adding = currentIDs.filter((id) => !selected.has(id)).length;
      if (adding > available) {
        setMsg(`单次最多选择 ${MAX_SELECTION} 条凭证，本页仅加入前 ${Math.max(0, available)} 条`);
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

  async function runBatchAction(action: "enable" | "disable" | "upload_cpa" | "delete") {
    if (selected.size === 0) return;
    const operationView = viewKeyRef.current;
    const operationSource = poolSource;
    const operationMaster = masterURL;
    const operationIDs = Array.from(selected);
    const operationPage = poolPage;
    if (action === "delete") {
      const confirmed = window.confirm(
        `确认永久删除当前来源中的 ${operationIDs.length} 条凭证？原始产物审计副本会保留。`,
      );
      if (!confirmed) return;
    }
    setBatchBusy(action);
    setMsg("");
    try {
      const response = await api<PoolBatchResponse>("/api/pool/batch", {
        method: "POST",
        body: JSON.stringify({
          source: operationSource,
          action,
          ids: operationIDs,
          master: operationSource === "federation" ? operationMaster : undefined,
        }),
      });
      if (viewKeyRef.current !== operationView) return;
      const failedIDs = new Set(
        (response.results || []).filter((result) => !result.ok).map((result) => result.id),
      );
      setSelected(failedIDs);
      if (action === "upload_cpa" && response.job_id) {
        setMsg(`已将 ${response.queued || response.succeeded || 0} 条凭证加入上传队列 · ${response.job_id}`);
        return;
      }
      const actionLabel = {
        enable: "启用",
        disable: "停用",
        upload_cpa: "上传 CPA",
        delete: "删除",
      }[action];
      setMsg(
        `${actionLabel}完成：成功 ${response.succeeded || 0}，失败 ${response.failed || 0}` +
          (failedIDs.size ? "；失败项已保留选择" : ""),
      );
      const targetPage = action === "upload_cpa" ? operationPage : 1;
      if (targetPage !== poolPage) {
        setListBusy(true);
        setPoolPage(targetPage);
      } else {
        await refreshPool(
          targetPage,
          operationSource,
          operationMaster,
          accountType,
          accountStatus,
          syncStatus,
          credentialQuery,
          timeField,
          timeRange,
        );
      }
    } catch (error) {
      if (viewKeyRef.current === operationView) {
        setMsg(error instanceof Error ? error.message : "批量操作失败");
      }
    } finally {
      setBatchBusy("");
    }
  }

  async function downloadCredentials(ids: string[]) {
    if (ids.length === 0) return;
    const operationView = viewKeyRef.current;
    const operationSource = poolSource;
    const operationMaster = masterURL;
    setBatchBusy("download");
    setMsg("");
    try {
      const response = await fetchPoolDownload({
        source: operationSource,
        ids,
        master: operationSource === "federation" ? operationMaster : undefined,
      });
      triggerBlobDownload(await response.blob(), ids.length === 1 ? "credential.zip" : "credentials.zip");
      if (viewKeyRef.current === operationView) {
        const succeeded = Number(response.headers.get("X-Batch-Succeeded")) || ids.length;
        setMsg(`已打包下载 ${succeeded} 条凭证`);
      }
    } catch (error) {
      if (viewKeyRef.current === operationView) {
        setMsg(error instanceof Error ? error.message : "批量下载失败");
      }
    } finally {
      setBatchBusy("");
    }
  }

  const emptyCredentialMessage =
    poolSource === "accounts"
      ? credentialQuery
        ? "没有符合当前搜索和筛选条件的凭证"
        : "统一号池为空 — 启动 panel 时会自动迁移本地凭证与 Tavily keys"
      : poolSource === "local"
        ? "本地号池为空 — 注册成功后入库，或点「入库最新注册结果」"
        : poolSource === "cloud"
          ? "云端 CPA 暂无条目（检查 CPA_MANAGEMENT 配置）"
          : "联邦主节点无列表或未授权";

  const tabItems = useMemo(
    () => [
      { value: "list", label: `凭证列表${poolTotal ? ` (${poolTotal})` : ""}` },
      { value: "artifacts", label: "原始产物" },
      { value: "patrol", label: "巡检运维" },
    ],
    [poolTotal],
  );

  const o = ov?.overview;
  const c = ov?.cleanup;
  const controlsBusy = listBusy || batchBusy !== "";

  return (
    <AdminShell>
      <PageHeader
        title="凭证池"
        description="统一管理账号、密钥、原始产物和巡检运维"
        actions={
          tab === "list" ? (
            <>
              <Button
                size="sm"
                variant="secondary"
                loading={busy}
                disabled={controlsBusy || busy}
                onClick={() => void importLatest()}
              >
                入库最新注册结果
              </Button>
              {poolSource === "local" ? (
                <Button
                  size="sm"
                  variant="secondary"
                  loading={busy}
                  disabled={controlsBusy || busy || poolUnsynced === 0}
                  onClick={() => void syncLocal()}
                >
                  一键上传未上传{poolUnsynced ? `（${poolUnsynced}）` : ""}
                </Button>
              ) : null}
            </>
          ) : tab === "patrol" ? (
            <>
              <Button
                size="sm"
                variant="secondary"
                loading={busy}
                onClick={() => void patrol("light")}
              >
                轻检
              </Button>
              <Button
                size="sm"
                variant="secondary"
                loading={busy}
                onClick={() => void patrol("deep")}
              >
                深检
              </Button>
              <Button size="sm" loading={busy} onClick={() => void cleanup()}>
                清理耗尽
              </Button>
            </>
          ) : null
        }
      />

      {msg ? (
        <div role="status" aria-live="polite" className="mb-3 rounded-md bg-kumo-contrast/5 px-3 py-2">
          <Text>{msg}</Text>
        </div>
      ) : null}

      <div className={`mb-3 ${controlsBusy ? "pointer-events-none opacity-60" : ""}`}>
        <Tabs
          variant="segmented"
          tabs={tabItems}
          value={tab}
          onValueChange={(v) => {
            if (!v || controlsBusy) return;
            activateTab(v as TabKey);
          }}
        />
      </div>

      {tab === "list" ? (
        <LayerCard>
          <LayerCard.Secondary>
            <div className="flex w-full flex-wrap items-center justify-between gap-2">
              <span>
                凭证列表 · {poolTotal}
                {selected.size ? ` · 已选 ${selected.size}` : ""}
              </span>
              <div className="flex flex-wrap gap-2">
                {selected.size > 0 ? (
                  <Button size="sm" variant="secondary" disabled={controlsBusy} onClick={() => setSelected(new Set())}>
                    取消选择
                  </Button>
                ) : null}
                {selected.size > 0 && capabilities.enable ? (
                  <Button
                    size="sm"
                    variant="secondary"
                    loading={batchBusy === "enable"}
                    disabled={controlsBusy}
                    onClick={() => void runBatchAction("enable")}
                  >
                    <PowerIcon size={16} /> 启用
                  </Button>
                ) : null}
                {selected.size > 0 && capabilities.disable ? (
                  <Button
                    size="sm"
                    variant="secondary"
                    loading={batchBusy === "disable"}
                    disabled={controlsBusy}
                    onClick={() => void runBatchAction("disable")}
                  >
                    <ProhibitIcon size={16} /> 停用
                  </Button>
                ) : null}
                {selected.size > 0 && capabilities.upload_cpa ? (
                  <Button
                    size="sm"
                    variant="secondary"
                    loading={batchBusy === "upload_cpa"}
                    disabled={controlsBusy}
                    onClick={() => void runBatchAction("upload_cpa")}
                  >
                    <UploadSimpleIcon size={16} /> 上传 CPA
                  </Button>
                ) : null}
                {selected.size > 0 && capabilities.download ? (
                  <Button
                    size="sm"
                    variant="secondary"
                    loading={batchBusy === "download"}
                    disabled={controlsBusy}
                    onClick={() => void downloadCredentials(Array.from(selected))}
                  >
                    <DownloadSimpleIcon size={16} /> 下载
                  </Button>
                ) : null}
                {selected.size > 0 && capabilities.delete ? (
                  <Button
                    size="sm"
                    variant="secondary"
                    loading={batchBusy === "delete"}
                    disabled={controlsBusy}
                    onClick={() => void runBatchAction("delete")}
                  >
                    <TrashIcon size={16} /> 删除
                  </Button>
                ) : null}
                <Button
                  size="sm"
                  variant="secondary"
                  loading={listBusy || busy}
                  disabled={controlsBusy}
                  onClick={() =>
                    void refreshPool(
                      poolPage,
                      poolSource,
                      masterURL,
                      accountType,
                      accountStatus,
                      syncStatus,
                      credentialQuery,
                      timeField,
                      timeRange,
                    )
                  }
                >
                  <ArrowsClockwiseIcon size={16} /> 刷新
                </Button>
              </div>
            </div>
          </LayerCard.Secondary>
          <LayerCard.Primary className="p-0">
            <div className="border-b border-kumo-hairline p-4">
              <div className="flex flex-wrap items-end gap-3">
                <div className="min-w-40">
                  <Select
                    label="来源"
                    disabled={controlsBusy}
                    value={POOL_SOURCE_LABELS[poolSource]}
                    onValueChange={(value) => {
                      if (!value) return;
                      const nextSource = (Object.entries(POOL_SOURCE_LABELS).find(
                        ([, label]) => label === value,
                      )?.[0] || "accounts") as PoolSource;
                      setPoolSource(nextSource);
                      setAccountType(ALL_TYPE);
                      setAccountStatus(ALL_STATUS);
                      setSyncStatus(ALL_SYNC);
                      setCredentialSearchInput("");
                      setCredentialQuery("");
                      beginPoolViewChange();
                      setPoolPage(1);
                    }}
                  >
                    {(Object.entries(POOL_SOURCE_LABELS) as [PoolSource, string][]).map(
                      ([source, label]) => (
                        <Select.Option key={source} value={label}>
                          {label}
                        </Select.Option>
                      ),
                    )}
                  </Select>
                </div>
                {poolSource !== "local" ? (
                  <>
                    <div className="min-w-56 flex-1">
                      <Input
                        label="搜索"
                        disabled={controlsBusy}
                        value={credentialSearchInput}
                        placeholder="文件名、邮箱、账号、分类"
                        onChange={(event) => setCredentialSearchInput(event.target.value)}
                        onKeyDown={(event) => {
                          if (event.key === "Enter") applyCredentialSearch();
                        }}
                      />
                    </div>
                    <Button
                      size="sm"
                      shape="square"
                      variant="secondary"
                      icon={MagnifyingGlassIcon}
                      title="搜索凭证"
                      aria-label="搜索凭证"
                      disabled={controlsBusy}
                      onClick={applyCredentialSearch}
                    />
                    <div className="min-w-36">
                      <Select
                        label="分类"
                        disabled={controlsBusy}
                        value={accountType}
                        onValueChange={(value) => {
                          if (!value) return;
                          setAccountType(value);
                          beginPoolViewChange();
                          setPoolPage(1);
                        }}
                      >
                        <Select.Option value={ALL_TYPE}>
                          {ALL_TYPE}
                          {Object.keys(typeCounts).length
                            ? ` (${Object.values(typeCounts).reduce((a, b) => a + b, 0)})`
                            : ""}
                        </Select.Option>
                        {(availableTypes.length ? availableTypes : ["xai", "tavily"]).map(
                          (type) => (
                            <Select.Option key={type} value={type}>
                              {type}
                              {typeCounts[type] != null ? ` (${typeCounts[type]})` : ""}
                            </Select.Option>
                          ),
                        )}
                      </Select>
                    </div>
                    <div className="min-w-32">
                      <Select
                        label="状态"
                        disabled={controlsBusy}
                        value={accountStatus}
                        onValueChange={(value) => {
                          if (!value) return;
                          setAccountStatus(value);
                          beginPoolViewChange();
                          setPoolPage(1);
                        }}
                      >
                        <Select.Option value={ALL_STATUS}>{ALL_STATUS}</Select.Option>
                        <Select.Option value="active">可用</Select.Option>
                        <Select.Option value="disabled">已停用</Select.Option>
                        <Select.Option value="exhausted">已耗尽</Select.Option>
                        <Select.Option value="unknown">未知</Select.Option>
                        <Select.Option value="error">异常</Select.Option>
                      </Select>
                    </div>
                    {poolSource === "accounts" ? (
                      <>
                    <div className="min-w-36">
                      <Select
                        label="时间字段"
                        disabled={controlsBusy}
                        value={
                          timeField === "created_at"
                            ? "创建时间"
                            : timeField === "updated_at"
                              ? "更新时间"
                              : "最后使用"
                        }
                        onValueChange={(value) => {
                          if (!value) return;
                          setTimeField(
                            value === "创建时间"
                              ? "created_at"
                              : value === "更新时间"
                                ? "updated_at"
                                : "last_used_at",
                          );
                          beginPoolViewChange();
                          setPoolPage(1);
                        }}
                      >
                        <Select.Option value="创建时间">创建时间</Select.Option>
                        <Select.Option value="更新时间">更新时间</Select.Option>
                        <Select.Option value="最后使用">最后使用</Select.Option>
                      </Select>
                    </div>
                    <div className="min-w-32">
                      <Select
                        label="时间范围"
                        disabled={controlsBusy}
                        value={
                          timeRange === "all"
                            ? "全部时间"
                            : timeRange === "24h"
                              ? "最近 24 小时"
                              : `最近 ${timeRange.replace("d", " 天")}`
                        }
                        onValueChange={(value) => {
                          if (!value) return;
                          const nextRange: TimeRange =
                            value === "全部时间"
                              ? "all"
                              : value === "最近 24 小时"
                                ? "24h"
                                : value.includes("7 天")
                                  ? "7d"
                                  : value.includes("30 天")
                                    ? "30d"
                                    : "90d";
                          setTimeRange(nextRange);
                          beginPoolViewChange();
                          setPoolPage(1);
                        }}
                      >
                        <Select.Option value="全部时间">全部时间</Select.Option>
                        <Select.Option value="最近 24 小时">最近 24 小时</Select.Option>
                        <Select.Option value="最近 7 天">最近 7 天</Select.Option>
                        <Select.Option value="最近 30 天">最近 30 天</Select.Option>
                        <Select.Option value="最近 90 天">最近 90 天</Select.Option>
                      </Select>
                    </div>
                      </>
                    ) : null}
                  </>
                ) : null}
                {poolSource === "local" ? (
                  <div className="min-w-40">
                    <Select
                      label="上传状态"
                      disabled={controlsBusy}
                      value={syncStatus === ALL_SYNC ? "全部" : syncStatus === "synced" ? "已上传" : "未上传"}
                      onValueChange={(value) => {
                        if (!value) return;
                        setSyncStatus(value === "已上传" ? "synced" : value === "未上传" ? "unsynced" : ALL_SYNC);
                        beginPoolViewChange();
                        setPoolPage(1);
                      }}
                    >
                      <Select.Option value="全部">全部</Select.Option>
                      <Select.Option value="已上传">已上传（{poolSynced}）</Select.Option>
                      <Select.Option value="未上传">未上传（{poolUnsynced}）</Select.Option>
                    </Select>
                  </div>
                ) : null}
                {poolSource === "federation" ? (
                  <div className="min-w-64 flex-1">
                    {masters.length > 0 ? (
                      <Select
                        label="联邦主节点"
                        disabled={controlsBusy}
                        value={masterURL}
                        onValueChange={(value) => {
                          if (!value) return;
                          setMasterURL(value);
                          beginPoolViewChange();
                          setPoolPage(1);
                        }}
                      >
                        {masters.map((master) => (
                          <Select.Option key={master} value={master}>
                            {master}
                          </Select.Option>
                        ))}
                      </Select>
                    ) : (
                      <Input
                        label="联邦主节点 URL"
                        disabled={controlsBusy}
                        value={masterURL}
                        onChange={(event) => {
                          beginPoolViewChange();
                          setMasterURL(event.target.value.trim());
                          setPoolPage(1);
                        }}
                        placeholder="https://master.example.com"
                      />
                    )}
                  </div>
                ) : null}
              </div>
              <div className="mt-3 flex flex-wrap items-center justify-between gap-2">
                <Text size="xs" variant="secondary">
                  {poolSource === "accounts" && credentialQuery
                    ? `搜索“${credentialQuery}” · `
                    : ""}
                  第 {poolPage}/{Math.max(1, poolTotalPages)} 页
                </Text>
                <div className="flex flex-wrap items-center justify-end gap-2">
                  {poolSource === "local" && poolUnsynced > 0 ? (
                    <Text size="xs" variant="secondary">
                      未同步到云端：{poolUnsynced}
                    </Text>
                  ) : null}
                  {poolSource === "federation" ? (
                    <>
                      <Badge variant={fedShareList ? "primary" : "secondary"}>
                        {fedShareList ? "允许看列表" : "禁止看列表"}
                      </Badge>
                      <Badge variant={fedCanPull ? "primary" : "secondary"}>
                        {fedCanPull ? "允许拉取" : "禁止拉取"}
                      </Badge>
                    </>
                  ) : null}
                </div>
              </div>
            </div>

            {poolError ? (
              <div className="px-4 py-12 text-center">
                <Text variant="secondary">{poolError}</Text>
              </div>
            ) : credentialRows.length === 0 ? (
              <div className="px-4 py-12 text-center">
                <Text variant="secondary">{emptyCredentialMessage}</Text>
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
                          label="选择当前页全部凭证"
                          disabled={controlsBusy}
                          onChange={toggleCredentialPage}
                        />
                      </Table.Head>
                      <Table.Head>编号</Table.Head>
                      <Table.Head>凭证 ID</Table.Head>
                      <Table.Head>分类</Table.Head>
                      <Table.Head>邮箱 / 账号</Table.Head>
                      <Table.Head>来源</Table.Head>
                      <Table.Head>插件 / 渠道</Table.Head>
                      <Table.Head>状态</Table.Head>
                      <Table.Head>
                        {poolSource === "accounts"
                          ? timeField === "created_at"
                            ? "创建时间"
                            : timeField === "updated_at"
                              ? "更新时间"
                              : "最后使用"
                          : "时间 / 统计"}
                      </Table.Head>
                      <Table.Head>操作</Table.Head>
                    </Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {credentialRows.map((row, index) => {
                      const time = formatDate(row.time);
                      const healthy = row.status === "active" || row.status === "synced";
                      return (
                        <Table.Row key={row.key}>
                          <Table.Cell>
                            <SelectionBox
                              checked={selected.has(row.id)}
                              label={`选择凭证 ${row.id}`}
                              disabled={controlsBusy}
                              onChange={() => toggleCredential(row.id)}
                            />
                          </Table.Cell>
                          <Table.Cell>
                            <Text size="xs">
                              {(poolPage - 1) * PAGE_SIZE + index + 1}
                            </Text>
                          </Table.Cell>
                          <Table.Cell>
                            <div className="max-w-48">
                              <Text size="xs">{row.id}</Text>
                              {row.filename ? (
                                <Text size="xs" variant="secondary">
                                  {row.filename}
                                </Text>
                              ) : null}
                            </div>
                          </Table.Cell>
                          <Table.Cell>
                            <Badge variant="secondary">{row.type}</Badge>
                          </Table.Cell>
                          <Table.Cell>
                            <div className="max-w-64">
                              <Text size="sm">{row.email || row.account || "—"}</Text>
                              {row.email && row.account ? (
                                <Text size="xs" variant="secondary">
                                  {row.account}
                                </Text>
                              ) : null}
                            </div>
                          </Table.Cell>
                          <Table.Cell>
                            <Badge variant="secondary">{row.source}</Badge>
                          </Table.Cell>
                          <Table.Cell>
                            <div className="max-w-48">
                              <Text size="xs">{row.plugin || "—"}</Text>
                              {row.channel ? (
                                <Text size="xs" variant="secondary">
                                  {row.channel}
                                </Text>
                              ) : null}
                            </div>
                          </Table.Cell>
                          <Table.Cell>
                            <div className="max-w-44">
                              <Badge variant={healthy ? "primary" : "secondary"}>
                                {credentialStatusLabel(row.status)}
                              </Badge>
                              {row.statusDetail ? (
                                <Text size="xs" variant="secondary">
                                  {row.statusDetail}
                                </Text>
                              ) : null}
                            </div>
                          </Table.Cell>
                          <Table.Cell>
                            <Text size="xs">{row.stats || time.date}</Text>
                            {!row.stats && time.time ? (
                              <Text size="xs" variant="secondary">
                                {time.time}
                              </Text>
                            ) : null}
                          </Table.Cell>
                          <Table.Cell>
                            <div className="flex gap-2">
                              {row.artifactQuery ? (
                                <Button
                                  size="sm"
                                  shape="square"
                                  variant="secondary"
                                  icon={EyeIcon}
                                  title="查看原始产物"
                                  aria-label={`查看凭证 ${row.id} 的原始产物`}
                                  disabled={controlsBusy}
                                  onClick={() => activateTab("artifacts", row.artifactQuery)}
                                />
                              ) : null}
                              {capabilities.download ? (
                                <Button
                                  size="sm"
                                  shape="square"
                                  variant="secondary"
                                  icon={DownloadSimpleIcon}
                                  title="下载凭证"
                                  aria-label={`下载凭证 ${row.id}`}
                                  loading={batchBusy === "download"}
                                  disabled={controlsBusy}
                                  onClick={() => void downloadCredentials([row.id])}
                                />
                              ) : null}
                            </div>
                          </Table.Cell>
                        </Table.Row>
                      );
                    })}
                  </Table.Body>
                </Table>
              </div>
            )}

            <Pager
              page={poolPage}
              totalPages={poolTotalPages}
              total={poolTotal}
              label={
                poolSource === "accounts"
                  ? "统一号池"
                  : poolSource === "local"
                    ? "本地 xAI"
                    : poolSource === "cloud"
                      ? "云端 CPA"
                      : "联邦号池"
              }
              busy={controlsBusy}
              onChange={(page) => {
                beginPoolViewChange();
                setPoolPage(page);
              }}
            />
          </LayerCard.Primary>
        </LayerCard>
      ) : tab === "artifacts" ? (
        <ArtifactWarehouse
          initialQuery={artifactQuery}
          onQueryChange={(nextQuery) => activateTab("artifacts", nextQuery)}
        />
      ) : (
        <>
          <div className="mb-4 grid gap-4 sm:grid-cols-2 lg:grid-cols-5">
            <Stat label="健康" value={o?.healthy} />
            <Stat label="限流" value={o?.rate_limited} />
            <Stat label="死号" value={o?.dead} />
            <Stat label="总量" value={o?.total} />
            <Stat label="额度估算" value={o?.quota_estimate} />
          </div>
          <LayerCard className="mb-4">
            <LayerCard.Secondary>清理</LayerCard.Secondary>
            <LayerCard.Primary>
              <Text size="sm">
                {c?.enabled ? "已启用定时" : "未启用定时（可手动）"}
                {c?.dry_run ? " · 演练" : ""}
                {c?.last_reason ? ` · ${c.last_reason}` : ""}
              </Text>
            </LayerCard.Primary>
          </LayerCard>
          <LayerCard>
            <LayerCard.Secondary>巡检 / 清理日志</LayerCard.Secondary>
            <LayerCard.Primary>
              <pre className="max-h-96 overflow-auto whitespace-pre-wrap text-xs">
                {patrolLogs || "（暂无）"}
              </pre>
            </LayerCard.Primary>
          </LayerCard>
        </>
      )}
    </AdminShell>
  );
}

function Stat({ label, value }: { label: string; value?: number }) {
  return (
    <LayerCard>
      <LayerCard.Secondary>{label}</LayerCard.Secondary>
      <LayerCard.Primary>
        <Text variant="heading3" as="h3">
          {value ?? "—"}
        </Text>
      </LayerCard.Primary>
    </LayerCard>
  );
}
