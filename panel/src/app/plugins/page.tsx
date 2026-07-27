"use client";

import { useEffect, useState } from "react";
import { Badge, Button, LayerCard, Text } from "@cloudflare/kumo";
import { AdminShell } from "@/components/admin-shell";
import { PageHeader } from "@/components/page-header";
import { api, type PluginInfo } from "@/lib/api";

type PluginsResp = {
  ok: boolean;
  plugins?: PluginInfo[];
  home?: string;
  in_tree?: string;
};

export default function PluginsPage() {
  const [data, setData] = useState<PluginsResp | null>(null);
  const [msg, setMsg] = useState("");
  const [busy, setBusy] = useState("");

  async function load() {
    setMsg("");
    const d = await api<PluginsResp>("/api/plugins");
    setData(d);
  }

  useEffect(() => {
    void load().catch((e: unknown) =>
      setMsg(e instanceof Error ? e.message : "加载失败"),
    );
  }, []);

  async function toggle(p: PluginInfo) {
    setBusy(p.id);
    setMsg("");
    try {
      const act = p.enabled ? "disable" : "enable";
      await api(`/api/plugins/${encodeURIComponent(p.id)}/${act}`, {
        method: "POST",
        body: "{}",
      });
      await load();
    } catch (e) {
      setMsg(e instanceof Error ? e.message : "操作失败");
    } finally {
      setBusy("");
    }
  }

  const plugins = data?.plugins || [];

  return (
    <AdminShell>
      <PageHeader
        title="插件"
        description="Host 只做架构；业务在插件里（in-tree + 已安装）"
        actions={
          <Button
            size="sm"
            variant="secondary"
            onClick={() => void load().catch(() => undefined)}
          >
            刷新
          </Button>
        }
      />

      <LayerCard className="mb-4">
        <LayerCard.Secondary>路径</LayerCard.Secondary>
        <LayerCard.Primary>
          <Text size="sm" variant="secondary">
            in-tree: {data?.in_tree || "—"}
          </Text>
          <Text size="sm" variant="secondary">
            home: {data?.home || "—"}
          </Text>
          {msg ? <Text size="sm">{msg}</Text> : null}
        </LayerCard.Primary>
      </LayerCard>

      <LayerCard>
        <LayerCard.Secondary>已发现插件 · {plugins.length}</LayerCard.Secondary>
        <LayerCard.Primary>
          {plugins.length === 0 ? (
            <Text variant="secondary">暂无插件</Text>
          ) : (
            <div className="flex flex-col gap-3">
              {plugins.map((p) => (
                <div
                  key={p.id}
                  className="flex flex-wrap items-center justify-between gap-2 border-b border-kumo-hairline pb-2 last:border-0"
                >
                  <div className="min-w-0">
                    <Text size="sm">
                      {p.name || p.id}{" "}
                      <Badge variant={p.enabled ? "primary" : "secondary"}>
                        {p.enabled ? "enabled" : "disabled"}
                      </Badge>{" "}
                      <Badge variant="secondary">{p.runtime}</Badge>{" "}
                      <Badge variant="secondary">{p.source}</Badge>
                      {p.status ? (
                        <>
                          {" "}
                          <Badge variant="secondary">{p.status}</Badge>
                        </>
                      ) : null}
                    </Text>
                    <Text size="xs" variant="secondary">
                      {p.id} · v{p.version}
                      {p.kind?.length ? ` · ${p.kind.join(",")}` : ""}
                    </Text>
                    {p.description ? (
                      <Text size="xs" variant="secondary">
                        {p.description}
                      </Text>
                    ) : null}
                    {p.artifact_kinds?.length ? (
                      <Text size="xs" variant="secondary">
                        artifacts: {p.artifact_kinds.join(", ")}
                      </Text>
                    ) : null}
                  </div>
                  <Button
                    size="sm"
                    variant={p.enabled ? "secondary" : "primary"}
                    loading={busy === p.id}
                    onClick={() => void toggle(p)}
                  >
                    {p.enabled ? "禁用" : "启用"}
                  </Button>
                </div>
              ))}
            </div>
          )}
        </LayerCard.Primary>
      </LayerCard>
    </AdminShell>
  );
}
