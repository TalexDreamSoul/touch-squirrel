"use client";

import { useEffect, useState } from "react";
import { ArrowsClockwiseIcon, DownloadSimpleIcon } from "@phosphor-icons/react";
import { Badge, Button, LayerCard, Tabs, Text } from "@cloudflare/kumo";
import { AdminShell } from "@/components/admin-shell";
import { PageHeader } from "@/components/page-header";
import {
  api,
  type MarketPlugin,
  type PluginInfo,
  type PluginRepository,
} from "@/lib/api";

type PluginsResp = {
  ok: boolean;
  plugins?: PluginInfo[];
  home?: string;
  in_tree?: string;
};

type MarketResp = {
  ok: boolean;
  plugins?: MarketPlugin[];
  repositories?: PluginRepository[];
};

type TabKey = "market" | "installed";

const TABS: { value: TabKey; label: string }[] = [
  { value: "market", label: "插件市场" },
  { value: "installed", label: "已安装" },
];

function sourceLabel(plugin: PluginInfo) {
  if (plugin.official) return "官方仓库";
  if (plugin.repository_name) return plugin.repository_name;
  if (plugin.source === "in-tree") return "内置";
  return "本机安装";
}

export default function PluginsPage() {
  const [tab, setTab] = useState<TabKey>("market");
  const [installedData, setInstalledData] = useState<PluginsResp | null>(null);
  const [marketData, setMarketData] = useState<MarketResp | null>(null);
  const [msg, setMsg] = useState("");
  const [busy, setBusy] = useState("");

  async function load(clearMessage = true) {
    if (clearMessage) setMsg("");
    const [installed, market] = await Promise.all([
      api<PluginsResp>("/api/plugins"),
      api<MarketResp>("/api/plugin-market"),
    ]);
    setInstalledData(installed);
    setMarketData(market);
  }

  useEffect(() => {
    void load().catch((error: unknown) =>
      setMsg(error instanceof Error ? error.message : "加载失败"),
    );
  }, []);

  async function toggle(plugin: PluginInfo) {
    setBusy(`toggle:${plugin.id}`);
    setMsg("");
    try {
      const action = plugin.enabled ? "disable" : "enable";
      await api(`/api/plugins/${encodeURIComponent(plugin.id)}/${action}`, {
        method: "POST",
        body: "{}",
      });
      await load(false);
    } catch (error) {
      setMsg(error instanceof Error ? error.message : "操作失败");
    } finally {
      setBusy("");
    }
  }

  async function syncAll() {
    setBusy("sync");
    setMsg("");
    try {
      const response = await api<{
        results?: { ok: boolean; repository: PluginRepository; error?: string }[];
      }>("/api/plugin-repositories/sync", { method: "POST", body: "{}" });
      const failed = (response.results || []).filter((result) => !result.ok);
      setMsg(
        failed.length > 0
          ? `${failed.length} 个仓库同步失败：${failed.map((item) => item.repository.name).join("、")}`
          : "仓库同步完成",
      );
      const [installed, market] = await Promise.all([
        api<PluginsResp>("/api/plugins"),
        api<MarketResp>("/api/plugin-market"),
      ]);
      setInstalledData(installed);
      setMarketData(market);
    } catch (error) {
      setMsg(error instanceof Error ? error.message : "同步失败");
    } finally {
      setBusy("");
    }
  }

  async function install(plugin: MarketPlugin) {
    const key = `install:${plugin.repository_id}:${plugin.manifest.id}`;
    setBusy(key);
    setMsg("");
    try {
      await api(
        `/api/plugin-market/${encodeURIComponent(plugin.repository_id)}/plugins/${encodeURIComponent(plugin.manifest.id)}/install`,
        { method: "POST", body: "{}" },
      );
      try {
        await load(false);
        setMsg(`已安装 ${plugin.manifest.name || plugin.manifest.id}`);
      } catch (refreshError) {
        const detail = refreshError instanceof Error ? `：${refreshError.message}` : "";
        setMsg(`插件已安装，但列表刷新失败${detail}`);
      }
    } catch (error) {
      setMsg(error instanceof Error ? error.message : "安装失败");
    } finally {
      setBusy("");
    }
  }

  const installed = installedData?.plugins || [];
  const market = marketData?.plugins || [];
  const repositories = marketData?.repositories || [];

  return (
    <AdminShell>
      <PageHeader
        title="插件"
        description="从受信任的 GitHub 仓库同步、安装并管理插件"
        actions={
          <>
            <Button
              size="sm"
              variant="secondary"
              loading={busy === "sync"}
              disabled={busy !== "" && busy !== "sync"}
              onClick={() => void syncAll()}
            >
              <ArrowsClockwiseIcon size={16} /> 同步仓库
            </Button>
            <Button
              size="sm"
              variant="secondary"
              disabled={busy !== ""}
              onClick={() => void load().catch(() => undefined)}
            >
              刷新
            </Button>
          </>
        }
      />

      {msg ? (
        <div className="mb-3 rounded-md bg-kumo-contrast/5 px-3 py-2">
          <Text size="sm">{msg}</Text>
        </div>
      ) : null}

      <div className="mb-4">
        <Tabs
          variant="segmented"
          tabs={TABS}
          value={tab}
          onValueChange={(value) => value && setTab(value as TabKey)}
        />
      </div>

      {tab === "market" ? (
        <LayerCard>
          <LayerCard.Secondary>
            插件市场 · {market.length} 个插件 · {repositories.length} 个仓库
          </LayerCard.Secondary>
          <LayerCard.Primary>
            {market.length === 0 ? (
              <div className="flex flex-col items-start gap-3 py-2">
                <Text variant="secondary">尚未同步仓库，市场中暂无可安装插件。</Text>
                <Button
                  size="sm"
                  loading={busy === "sync"}
                  disabled={busy !== "" && busy !== "sync"}
                  onClick={() => void syncAll()}
                >
                  <ArrowsClockwiseIcon size={16} /> 首次同步
                </Button>
              </div>
            ) : (
              <div className="flex flex-col gap-3">
                {market.map((plugin) => {
                  const key = `install:${plugin.repository_id}:${plugin.manifest.id}`;
                  const hasDifferentVersion =
                    !!plugin.installed_version &&
                    plugin.installed_version !== plugin.manifest.version;
                  return (
                    <div
                      key={`${plugin.repository_id}:${plugin.manifest.id}`}
                      className="flex flex-wrap items-center justify-between gap-3 border-b border-kumo-hairline pb-3 last:border-0 last:pb-0"
                    >
                      <div className="min-w-0 flex-1">
                        <div className="flex flex-wrap items-center gap-2">
                          <Text size="sm">{plugin.manifest.name || plugin.manifest.id}</Text>
                          {plugin.official ? <Badge variant="primary">官方</Badge> : null}
                          <Badge variant="secondary">{plugin.manifest.runtime}</Badge>
                          {plugin.manifest.status ? (
                            <Badge variant="secondary">{plugin.manifest.status}</Badge>
                          ) : null}
                        </div>
                        <Text size="xs" variant="secondary">
                          {plugin.manifest.id} · v{plugin.manifest.version} · {plugin.repository_name}
                        </Text>
                        {plugin.manifest.description ? (
                          <Text size="xs" variant="secondary">
                            {plugin.manifest.description}
                          </Text>
                        ) : null}
                      </div>
                      <Button
                        size="sm"
                        variant={plugin.installed ? "secondary" : "primary"}
                        loading={busy === key}
                        disabled={plugin.installed || (busy !== "" && busy !== key)}
                        onClick={() => void install(plugin)}
                      >
                        <DownloadSimpleIcon size={16} />
                        {plugin.installed
                          ? "已安装"
                          : hasDifferentVersion
                            ? `更新至 ${plugin.manifest.version}`
                            : "安装"}
                      </Button>
                    </div>
                  );
                })}
              </div>
            )}
          </LayerCard.Primary>
        </LayerCard>
      ) : null}

      {tab === "installed" ? (
        <div className="flex flex-col gap-4">
          <LayerCard>
            <LayerCard.Secondary>本地路径</LayerCard.Secondary>
            <LayerCard.Primary>
              <Text size="sm" variant="secondary">
                内置：{installedData?.in_tree || "—"}
              </Text>
              <Text size="sm" variant="secondary">
                安装目录：{installedData?.home || "—"}
              </Text>
            </LayerCard.Primary>
          </LayerCard>

          <LayerCard>
            <LayerCard.Secondary>已发现插件 · {installed.length}</LayerCard.Secondary>
            <LayerCard.Primary>
              {installed.length === 0 ? (
                <Text variant="secondary">暂无已安装插件</Text>
              ) : (
                <div className="flex flex-col gap-3">
                  {installed.map((plugin) => (
                    <div
                      key={plugin.id}
                      className="flex flex-wrap items-center justify-between gap-3 border-b border-kumo-hairline pb-3 last:border-0 last:pb-0"
                    >
                      <div className="min-w-0 flex-1">
                        <div className="flex flex-wrap items-center gap-2">
                          <Text size="sm">{plugin.name || plugin.id}</Text>
                          {plugin.official ? <Badge variant="primary">官方</Badge> : null}
                          <Badge variant={plugin.enabled ? "primary" : "secondary"}>
                            {plugin.enabled ? "已启用" : "已停用"}
                          </Badge>
                          <Badge variant="secondary">{plugin.runtime}</Badge>
                        </div>
                        <Text size="xs" variant="secondary">
                          {plugin.id} · v{plugin.version} · {sourceLabel(plugin)}
                          {plugin.kind?.length ? ` · ${plugin.kind.join(", ")}` : ""}
                        </Text>
                        {plugin.description ? (
                          <Text size="xs" variant="secondary">
                            {plugin.description}
                          </Text>
                        ) : null}
                      </div>
                      <Button
                        size="sm"
                        variant={plugin.enabled ? "secondary" : "primary"}
                        loading={busy === `toggle:${plugin.id}`}
                        disabled={busy !== "" && busy !== `toggle:${plugin.id}`}
                        onClick={() => void toggle(plugin)}
                      >
                        {plugin.enabled ? "停用" : "启用"}
                      </Button>
                    </div>
                  ))}
                </div>
              )}
            </LayerCard.Primary>
          </LayerCard>
        </div>
      ) : null}
    </AdminShell>
  );
}
