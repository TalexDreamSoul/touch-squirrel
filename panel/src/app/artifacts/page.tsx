"use client";

import { useEffect, useState } from "react";
import { Badge, Button, Input, LayerCard, Table, Text } from "@cloudflare/kumo";
import { AdminShell } from "@/components/admin-shell";
import { PageHeader } from "@/components/page-header";
import { api, type ArtifactInfo } from "@/lib/api";

type ArtifactsResp = {
  ok: boolean;
  artifacts?: ArtifactInfo[];
  store?: string;
};

export default function ArtifactsPage() {
  const [plugin, setPlugin] = useState("");
  const [kind, setKind] = useState("");
  const [data, setData] = useState<ArtifactsResp | null>(null);
  const [msg, setMsg] = useState("");
  const [busy, setBusy] = useState(false);

  async function load() {
    setMsg("");
    setBusy(true);
    try {
      const q = new URLSearchParams();
      if (plugin.trim()) q.set("plugin", plugin.trim());
      if (kind.trim()) q.set("kind", kind.trim());
      q.set("limit", "100");
      const d = await api<ArtifactsResp>(`/api/artifacts?${q.toString()}`);
      setData(d);
    } catch (e) {
      setMsg(e instanceof Error ? e.message : "加载失败");
    } finally {
      setBusy(false);
    }
  }

  useEffect(() => {
    void load();
  }, []);

  const rows = data?.artifacts || [];

  return (
    <AdminShell>
      <PageHeader
        title="囤货"
        description="统一 artifact store（session / oauth / key.tavily / …）"
        actions={
          <Button
            size="sm"
            variant="secondary"
            loading={busy}
            onClick={() => void load()}
          >
            刷新
          </Button>
        }
      />

      <LayerCard className="mb-4">
        <LayerCard.Secondary>筛选</LayerCard.Secondary>
        <LayerCard.Primary>
          <div className="flex flex-col gap-3">
            <Input
              label="plugin"
              value={plugin}
              placeholder="tavily-pool / xai-accounts"
              onChange={(e) => setPlugin(e.target.value)}
            />
            <Input
              label="kind"
              value={kind}
              placeholder="key.tavily / session.sso"
              onChange={(e) => setKind(e.target.value)}
            />
            <div className="flex gap-2">
              <Button size="sm" loading={busy} onClick={() => void load()}>
                筛选
              </Button>
            </div>
            <Text size="xs" variant="secondary">
              store: {data?.store || "—"} · count={rows.length}
            </Text>
            {msg ? <Text size="sm">{msg}</Text> : null}
          </div>
        </LayerCard.Primary>
      </LayerCard>

      <LayerCard>
        <LayerCard.Secondary>产物列表</LayerCard.Secondary>
        <LayerCard.Primary>
          {rows.length === 0 ? (
            <Text variant="secondary">暂无产物</Text>
          ) : (
            <Table>
              <Table.Header>
                <Table.Row>
                  <Table.Head>ID</Table.Head>
                  <Table.Head>Plugin</Table.Head>
                  <Table.Head>Kind</Table.Head>
                  <Table.Head>Status</Table.Head>
                  <Table.Head>Created</Table.Head>
                  <Table.Head>Labels</Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {rows.map((a) => {
                  const labs = Object.entries(a.labels || {})
                    .map(([k, v]) => `${k}=${v}`)
                    .join(", ");
                  return (
                    <Table.Row key={a.id}>
                      <Table.Cell>
                        <Text size="xs">{a.id}</Text>
                      </Table.Cell>
                      <Table.Cell>
                        <Text size="sm">{a.plugin}</Text>
                      </Table.Cell>
                      <Table.Cell>
                        <Badge variant="secondary">{a.kind}</Badge>
                      </Table.Cell>
                      <Table.Cell>
                        <Badge
                          variant={a.status === "fresh" ? "primary" : "secondary"}
                        >
                          {a.status}
                        </Badge>
                      </Table.Cell>
                      <Table.Cell>
                        <Text size="xs" variant="secondary">
                          {a.created_at}
                        </Text>
                      </Table.Cell>
                      <Table.Cell>
                        <Text size="xs" variant="secondary">
                          {labs || "—"}
                        </Text>
                      </Table.Cell>
                    </Table.Row>
                  );
                })}
              </Table.Body>
            </Table>
          )}
        </LayerCard.Primary>
      </LayerCard>
    </AdminShell>
  );
}
