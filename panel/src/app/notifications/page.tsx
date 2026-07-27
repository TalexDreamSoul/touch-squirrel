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
import { api } from "@/lib/api";

type Channel = {
  id: string;
  name: string;
  kind: string;
  enabled: boolean;
  events?: string[];
  config?: Record<string, string>;
  created_at?: string;
  updated_at?: string;
};

type ListResp = {
  ok: boolean;
  channels?: Channel[];
  events?: string[];
  kinds?: string[];
  store?: string;
};

const KIND_LABEL: Record<string, string> = {
  feishu: "飞书机器人",
  smtp: "邮件 SMTP",
  webhook: "Webhook",
};

const EMPTY_FORM = {
  name: "",
  kind: "feishu",
  enabled: true,
  events: "register.finished,register.failed,system.test",
  webhook_url: "",
  secret: "",
  host: "",
  port: "587",
  user: "",
  password: "",
  from: "",
  to: "",
  url: "",
  token: "",
};

export default function NotificationsPage() {
  const [data, setData] = useState<ListResp | null>(null);
  const [msg, setMsg] = useState("");
  const [busy, setBusy] = useState(false);
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<Channel | null>(null);
  const [form, setForm] = useState({ ...EMPTY_FORM });

  const load = useCallback(async () => {
    setBusy(true);
    try {
      const d = await api<ListResp>("/api/notifications");
      setData(d);
    } catch (e) {
      setMsg(e instanceof Error ? e.message : "加载失败");
    } finally {
      setBusy(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const rows = data?.channels || [];
  const knownEvents = data?.events || [];
  const eventHint = useMemo(
    () => (knownEvents.length ? knownEvents.join(" · ") : "register.* · system.test"),
    [knownEvents],
  );

  function openCreate() {
    setEditing(null);
    setForm({ ...EMPTY_FORM });
    setOpen(true);
  }

  function openEdit(c: Channel) {
    const cfg = c.config || {};
    setEditing(c);
    setForm({
      name: c.name,
      kind: c.kind,
      enabled: c.enabled,
      events: (c.events || []).join(","),
      webhook_url: cfg.webhook_url || "",
      secret: cfg.secret || "",
      host: cfg.host || "",
      port: cfg.port || "587",
      user: cfg.user || "",
      password: cfg.password || "",
      from: cfg.from || "",
      to: cfg.to || "",
      url: cfg.url || "",
      token: cfg.token || "",
    });
    setOpen(true);
  }

  function closeDialog() {
    setOpen(false);
    setEditing(null);
  }

  function buildConfig(): Record<string, string> {
    if (form.kind === "feishu") {
      return {
        webhook_url: form.webhook_url.trim(),
        secret: form.secret,
      };
    }
    if (form.kind === "smtp") {
      return {
        host: form.host.trim(),
        port: form.port.trim() || "587",
        user: form.user.trim(),
        password: form.password,
        from: form.from.trim(),
        to: form.to.trim(),
      };
    }
    return {
      url: form.url.trim(),
      token: form.token,
    };
  }

  async function save() {
    setBusy(true);
    setMsg("");
    try {
      const events = form.events
        .split(/[,;\s]+/)
        .map((s) => s.trim())
        .filter(Boolean);
      const body = {
        name: form.name.trim(),
        kind: form.kind,
        enabled: form.enabled,
        events,
        config: buildConfig(),
      };
      if (editing) {
        await api(`/api/notifications/${editing.id}`, {
          method: "PUT",
          body: JSON.stringify(body),
        });
        setMsg(`已更新 ${body.name}`);
      } else {
        await api("/api/notifications", {
          method: "POST",
          body: JSON.stringify(body),
        });
        setMsg(`已创建 ${body.name}`);
      }
      closeDialog();
      await load();
    } catch (e) {
      setMsg(e instanceof Error ? e.message : "保存失败");
    } finally {
      setBusy(false);
    }
  }

  async function remove(c: Channel) {
    if (!confirm(`删除通知渠道「${c.name}」？`)) return;
    setBusy(true);
    try {
      await api(`/api/notifications/${c.id}`, { method: "DELETE" });
      setMsg(`已删除 ${c.name}`);
      await load();
    } catch (e) {
      setMsg(e instanceof Error ? e.message : "删除失败");
    } finally {
      setBusy(false);
    }
  }

  async function testChannel(id: string, name: string) {
    setBusy(true);
    try {
      await api(`/api/notifications/${id}/test`, {
        method: "POST",
        body: "{}",
      });
      setMsg(`测试已发送 · ${name}`);
    } catch (e) {
      setMsg(e instanceof Error ? e.message : "测试失败");
    } finally {
      setBusy(false);
    }
  }

  async function testFromDialog() {
    if (!editing) {
      setMsg("请先保存渠道后再测试");
      return;
    }
    // save first so dialog edits take effect, then test
    setBusy(true);
    setMsg("");
    try {
      const events = form.events
        .split(/[,;\s]+/)
        .map((s) => s.trim())
        .filter(Boolean);
      const body = {
        name: form.name.trim(),
        kind: form.kind,
        enabled: form.enabled,
        events,
        config: buildConfig(),
      };
      await api(`/api/notifications/${editing.id}`, {
        method: "PUT",
        body: JSON.stringify(body),
      });
      await api(`/api/notifications/${editing.id}/test`, {
        method: "POST",
        body: "{}",
      });
      setMsg(`已保存并发送测试 · ${body.name}`);
      await load();
    } catch (e) {
      setMsg(e instanceof Error ? e.message : "测试失败");
    } finally {
      setBusy(false);
    }
  }

  async function toggle(c: Channel) {
    setBusy(true);
    try {
      await api(`/api/notifications/${c.id}`, {
        method: "PUT",
        body: JSON.stringify({
          name: c.name,
          kind: c.kind,
          enabled: !c.enabled,
          events: c.events || [],
          config: c.config || {},
        }),
      });
      await load();
    } catch (e) {
      setMsg(e instanceof Error ? e.message : "切换失败");
    } finally {
      setBusy(false);
    }
  }

  return (
    <AdminShell>
      <PageHeader
        title="通知"
        description="飞书机器人 · SMTP 邮件 · Webhook · 可订阅注册/巡检等事件"
        actions={
          <>
            <Button size="sm" variant="secondary" loading={busy} onClick={() => void load()}>
              刷新
            </Button>
            <Button size="sm" loading={busy} onClick={openCreate}>
              新建通知
            </Button>
          </>
        }
      />

      {msg ? (
        <div className="mb-3 rounded-md bg-kumo-contrast/5 px-3 py-2">
          <Text>{msg}</Text>
        </div>
      ) : null}

      <LayerCard>
        <LayerCard.Secondary>
          渠道列表 {rows.length ? `(${rows.length})` : ""}
          {data?.store ? (
            <Text size="xs" variant="secondary">
              {" "}
              · {data.store}
            </Text>
          ) : null}
        </LayerCard.Secondary>
        <LayerCard.Primary className="p-0">
          {rows.length === 0 ? (
            <div className="p-4">
              <Text variant="secondary">
                还没有通知渠道 — 点「新建通知」添加飞书机器人或 SMTP
              </Text>
            </div>
          ) : (
            <Table>
              <Table.Header>
                <Table.Row>
                  <Table.Head>名称</Table.Head>
                  <Table.Head>类型</Table.Head>
                  <Table.Head>状态</Table.Head>
                  <Table.Head>事件</Table.Head>
                  <Table.Head>更新</Table.Head>
                  <Table.Head>操作</Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {rows.map((c) => (
                  <Table.Row key={c.id}>
                    <Table.Cell>
                      <Text size="sm">{c.name}</Text>
                      <Text size="xs" variant="secondary">
                        {c.id}
                      </Text>
                    </Table.Cell>
                    <Table.Cell>
                      <Badge variant="secondary">{KIND_LABEL[c.kind] || c.kind}</Badge>
                    </Table.Cell>
                    <Table.Cell>
                      {c.enabled ? (
                        <Badge variant="primary">启用</Badge>
                      ) : (
                        <Badge variant="secondary">停用</Badge>
                      )}
                    </Table.Cell>
                    <Table.Cell>
                      <Text size="xs" variant="secondary">
                        {(c.events || []).join(", ") || "—"}
                      </Text>
                    </Table.Cell>
                    <Table.Cell>
                      <Text size="xs" variant="secondary">
                        {c.updated_at || c.created_at || "—"}
                      </Text>
                    </Table.Cell>
                    <Table.Cell>
                      <div className="flex flex-wrap gap-2">
                        <Button
                          size="sm"
                          variant="secondary"
                          loading={busy}
                          onClick={() => void testChannel(c.id, c.name)}
                        >
                          测试
                        </Button>
                        <Button
                          size="sm"
                          variant="secondary"
                          loading={busy}
                          onClick={() => void toggle(c)}
                        >
                          {c.enabled ? "停用" : "启用"}
                        </Button>
                        <Button size="sm" variant="secondary" onClick={() => openEdit(c)}>
                          编辑
                        </Button>
                        <Button
                          size="sm"
                          variant="secondary"
                          loading={busy}
                          onClick={() => void remove(c)}
                        >
                          删除
                        </Button>
                      </div>
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
          if (!next) closeDialog();
          else setOpen(true);
        }}
      >
        <Dialog size="lg" className="flex max-h-[min(90vh,56rem)] flex-col p-6">
          <div className="mb-4 flex items-start justify-between gap-4">
            <div className="min-w-0">
              <Dialog.Title className="text-xl font-semibold">
                {editing ? `编辑 · ${editing.name}` : "新建通知渠道"}
              </Dialog.Title>
              <Dialog.Description className="mt-1 text-kumo-subtle">
                飞书机器人 / SMTP / Webhook · 密钥本地保存
              </Dialog.Description>
            </div>
            <Dialog.Close
              render={(p) => (
                <Button {...p} variant="secondary" size="sm">
                  关闭
                </Button>
              )}
            />
          </div>

          <div className="min-h-0 flex-1 overflow-y-auto">
            <div className="grid gap-3 sm:grid-cols-2">
              <Input
                label="名称"
                value={form.name}
                onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))}
                placeholder="生产飞书告警"
              />
              <Select
                label="类型"
                value={form.kind}
                onValueChange={(v) => {
                  if (!v) return;
                  setForm((f) => ({ ...f, kind: v }));
                }}
              >
                <Select.Option value="feishu">飞书机器人</Select.Option>
                <Select.Option value="smtp">邮件 SMTP</Select.Option>
                <Select.Option value="webhook">Webhook</Select.Option>
              </Select>

              <div className="sm:col-span-2">
                <Input
                  label="订阅事件（逗号分隔）"
                  value={form.events}
                  onChange={(e) => setForm((f) => ({ ...f, events: e.target.value }))}
                  placeholder="register.finished,register.failed"
                />
                <Text size="xs" variant="secondary">
                  可选：{eventHint}
                </Text>
              </div>

              <div className="sm:col-span-2">
                <Switch
                  label="启用"
                  checked={form.enabled}
                  onCheckedChange={(v) => setForm((f) => ({ ...f, enabled: !!v }))}
                />
              </div>

              {form.kind === "feishu" ? (
                <>
                  <div className="sm:col-span-2">
                    <Input
                      label="Webhook URL"
                      value={form.webhook_url}
                      onChange={(e) =>
                        setForm((f) => ({ ...f, webhook_url: e.target.value }))
                      }
                      placeholder="https://open.feishu.cn/open-apis/bot/v2/hook/..."
                    />
                  </div>
                  <Input
                    label="签名密钥（可选）"
                    value={form.secret}
                    onChange={(e) => setForm((f) => ({ ...f, secret: e.target.value }))}
                    placeholder="留空=不签名；•••• 表示保留原值"
                  />
                </>
              ) : null}

              {form.kind === "smtp" ? (
                <>
                  <Input
                    label="SMTP Host"
                    value={form.host}
                    onChange={(e) => setForm((f) => ({ ...f, host: e.target.value }))}
                    placeholder="smtp.exmail.qq.com"
                  />
                  <Input
                    label="端口"
                    value={form.port}
                    onChange={(e) => setForm((f) => ({ ...f, port: e.target.value }))}
                  />
                  <Input
                    label="用户名"
                    value={form.user}
                    onChange={(e) => setForm((f) => ({ ...f, user: e.target.value }))}
                  />
                  <Input
                    label="密码"
                    type="password"
                    value={form.password}
                    onChange={(e) => setForm((f) => ({ ...f, password: e.target.value }))}
                    placeholder="•••• 表示保留原值"
                  />
                  <Input
                    label="发件人 From"
                    value={form.from}
                    onChange={(e) => setForm((f) => ({ ...f, from: e.target.value }))}
                  />
                  <Input
                    label="收件人 To（逗号分隔）"
                    value={form.to}
                    onChange={(e) => setForm((f) => ({ ...f, to: e.target.value }))}
                  />
                </>
              ) : null}

              {form.kind === "webhook" ? (
                <>
                  <div className="sm:col-span-2">
                    <Input
                      label="Webhook URL"
                      value={form.url}
                      onChange={(e) => setForm((f) => ({ ...f, url: e.target.value }))}
                      placeholder="https://example.com/hooks/squirrel"
                    />
                  </div>
                  <Input
                    label="Bearer Token（可选）"
                    value={form.token}
                    onChange={(e) => setForm((f) => ({ ...f, token: e.target.value }))}
                  />
                </>
              ) : null}
            </div>
          </div>

          <div className="mt-6 flex flex-wrap justify-end gap-2">
            {editing ? (
              <Button
                size="sm"
                variant="secondary"
                loading={busy}
                onClick={() => void testFromDialog()}
              >
                测试
              </Button>
            ) : null}
            <Dialog.Close
              render={(p) => (
                <Button {...p} size="sm" variant="secondary">
                  取消
                </Button>
              )}
            />
            <Button size="sm" loading={busy} onClick={() => void save()}>
              {editing ? "保存" : "创建"}
            </Button>
          </div>
        </Dialog>
      </Dialog.Root>
    </AdminShell>
  );
}
