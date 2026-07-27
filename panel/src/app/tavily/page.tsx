"use client";

import { useEffect, useState } from "react";
import {
  Badge,
  Button,
  Dialog,
  Input,
  LayerCard,
  Table,
  Text,
} from "@cloudflare/kumo";
import { AdminShell } from "@/components/admin-shell";
import { PageHeader } from "@/components/page-header";
import { api, type TavilyKeyInfo } from "@/lib/api";

type KeysResp = {
  ok: boolean;
  keys?: TavilyKeyInfo[];
  keys_total?: number;
  keys_active?: number;
  state?: string;
  hint?: string;
};

export default function TavilyPage() {
  const [data, setData] = useState<KeysResp | null>(null);
  const [apiKey, setApiKey] = useState("");
  const [note, setNote] = useState("");
  const [msg, setMsg] = useState("");
  const [busy, setBusy] = useState(false);
  const [open, setOpen] = useState(false);

  async function load() {
    setMsg("");
    const d = await api<KeysResp>("/api/tavily/keys");
    setData(d);
  }

  useEffect(() => {
    void load().catch((e: unknown) =>
      setMsg(e instanceof Error ? e.message : "加载失败"),
    );
  }, []);

  async function addKey() {
    const key = apiKey.trim();
    if (!key) {
      setMsg("请填写 api_key");
      return;
    }
    setBusy(true);
    setMsg("");
    try {
      await api("/api/tavily/keys", {
        method: "POST",
        body: JSON.stringify({ api_key: key, note: note.trim() }),
      });
      setApiKey("");
      setNote("");
      setOpen(false);
      setMsg("已加入密钥");
      await load();
    } catch (e) {
      setMsg(e instanceof Error ? e.message : "添加失败");
    } finally {
      setBusy(false);
    }
  }

  async function setStatus(id: string, status: string) {
    setBusy(true);
    try {
      await api(`/api/tavily/keys/${id}/status`, {
        method: "POST",
        body: JSON.stringify({ status }),
      });
      await load();
    } catch (e) {
      setMsg(e instanceof Error ? e.message : "更新失败");
    } finally {
      setBusy(false);
    }
  }

  const keys = data?.keys || [];

  return (
    <AdminShell>
      <PageHeader
        title="Tavily 池"
        description="API key 池（与 xAI 号池分离）。MCP/HTTP：squirrel pool serve"
        actions={
          <>
            <Button
              size="sm"
              variant="secondary"
              onClick={() => void load().catch(() => undefined)}
            >
              刷新
            </Button>
            <Button size="sm" onClick={() => setOpen(true)}>
              加入密钥
            </Button>
          </>
        }
      />

      {msg ? (
        <div className="mb-3 rounded-md bg-kumo-contrast/5 px-3 py-2">
          <Text>{msg}</Text>
        </div>
      ) : null}

      <LayerCard className="mb-4">
        <LayerCard.Secondary>概览</LayerCard.Secondary>
        <LayerCard.Primary className="p-4">
          <Text size="sm">
            active {data?.keys_active ?? 0}/{data?.keys_total ?? 0}
            {data?.state ? ` · ${data.state}` : ""}
          </Text>
          {data?.hint ? (
            <Text size="xs" variant="secondary">
              {data.hint}
            </Text>
          ) : null}
        </LayerCard.Primary>
      </LayerCard>

      <LayerCard>
        <LayerCard.Secondary>密钥列表</LayerCard.Secondary>
        <LayerCard.Primary className="p-0">
          {keys.length === 0 ? (
            <div className="p-4">
              <Text variant="secondary">
                空池 · 点「加入密钥」添加 tvly- key 后可用 squirrel pool serve 暴露 /mcp
              </Text>
            </div>
          ) : (
            <Table>
              <Table.Header>
                <Table.Row>
                  <Table.Head>ID</Table.Head>
                  <Table.Head>Status</Table.Head>
                  <Table.Head>OK</Table.Head>
                  <Table.Head>Fail</Table.Head>
                  <Table.Head>Key</Table.Head>
                  <Table.Head>操作</Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {keys.map((k) => (
                  <Table.Row key={k.id}>
                    <Table.Cell>
                      <Text size="xs">{k.id}</Text>
                    </Table.Cell>
                    <Table.Cell>
                      <Badge
                        variant={k.status === "active" ? "primary" : "secondary"}
                      >
                        {k.status}
                      </Badge>
                    </Table.Cell>
                    <Table.Cell>
                      <Text size="sm">{k.success ?? 0}</Text>
                    </Table.Cell>
                    <Table.Cell>
                      <Text size="sm">{k.failure ?? 0}</Text>
                    </Table.Cell>
                    <Table.Cell>
                      <Text size="xs">{k.api_key}</Text>
                    </Table.Cell>
                    <Table.Cell>
                      {k.status === "active" ? (
                        <Button
                          size="sm"
                          variant="secondary"
                          loading={busy}
                          onClick={() => void setStatus(k.id, "disabled")}
                        >
                          禁用
                        </Button>
                      ) : (
                        <Button
                          size="sm"
                          variant="secondary"
                          loading={busy}
                          onClick={() => void setStatus(k.id, "active")}
                        >
                          启用
                        </Button>
                      )}
                    </Table.Cell>
                  </Table.Row>
                ))}
              </Table.Body>
            </Table>
          )}
        </LayerCard.Primary>
      </LayerCard>

      <Dialog.Root
        open={open}
        onOpenChange={(next) => {
          setOpen(!!next);
          if (!next) {
            setApiKey("");
            setNote("");
          }
        }}
      >
        <Dialog size="base" className="flex max-h-[min(90vh,40rem)] flex-col p-6">
          <div className="mb-4">
            <Dialog.Title className="text-xl font-semibold">加入 Tavily 密钥</Dialog.Title>
            <Dialog.Description className="mt-1 text-kumo-subtle">
              写入本地 tavily-pool，并同步统一号池索引
            </Dialog.Description>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto">
            <div className="flex flex-col gap-3">
              <Input
                label="api_key"
                value={apiKey}
                placeholder="tvly-..."
                onChange={(e) => setApiKey(e.target.value)}
              />
              <Input
                label="note"
                value={note}
                placeholder="可选备注"
                onChange={(e) => setNote(e.target.value)}
              />
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
            <Button size="sm" loading={busy} onClick={() => void addKey()}>
              加入池
            </Button>
          </div>
        </Dialog>
      </Dialog.Root>
    </AdminShell>
  );
}
