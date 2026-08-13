"use client";

import { useEffect, useState } from "react";
import { Button, Input, LayerCard, Select, Switch, Text } from "@cloudflare/kumo";
import { AdminShell } from "@/components/admin-shell";
import { PageHeader } from "@/components/page-header";
import { api, type PanelConfig } from "@/lib/api";
import { useTheme, type ThemeMode } from "@/lib/theme";

function encodeInfrastructure(value: Record<string, string | number>) {
  const bytes = new TextEncoder().encode(JSON.stringify(value));
  let binary = "";
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

function decodeInfrastructure(raw: string) {
  const base64 = raw.replace(/-/g, "+").replace(/_/g, "/");
  const padded = base64 + "=".repeat((4 - (base64.length % 4)) % 4);
  const binary = atob(padded);
  const bytes = Uint8Array.from(binary, (char) => char.charCodeAt(0));
  return JSON.parse(new TextDecoder().decode(bytes)) as Record<string, string | number>;
}

export default function SettingsPage() {
  const { theme, setTheme, resolved } = useTheme();
  const [cfg, setCfg] = useState<PanelConfig>({});
  const [cpaKey, setCpaKey] = useState("");
  const [resinToken, setResinToken] = useState("");
  const [mailRouterAPIKey, setMailRouterAPIKey] = useState("");
  const [importLink, setImportLink] = useState("");
  const [masterURL, setMasterURL] = useState("");
  const [masterToken, setMasterToken] = useState("");
  const [msg, setMsg] = useState("");
  const [busy, setBusy] = useState(false);

  async function load() {
    const d = await api<{ config: PanelConfig }>("/api/config");
    setCfg(d.config || {});
  }

  useEffect(() => {
    void load().catch((e: unknown) =>
      setMsg(e instanceof Error ? e.message : "加载失败"),
    );
  }, []);

  function setField(key: string, value: string | number | boolean) {
    setCfg((prev) => ({ ...prev, [key]: value }));
  }

  async function save() {
    setBusy(true);
    setMsg("");
    try {
      const body: Record<string, string | number | boolean> = {
        cpa_management_base: String(cfg.cpa_management_base || ""),
        cpa_upload_enabled: !!cfg.cpa_upload_enabled,
        register_proxy: String(cfg.register_proxy || ""),
        flaresolverr_url: String(cfg.flaresolverr_url || ""),
        email_mode: String(cfg.email_mode || "tempmail"),
        resin_proxy: String(cfg.resin_proxy || ""),
        resin_platform: String(cfg.resin_platform || "Default"),
        mail_router_url: String(cfg.mail_router_url || ""),
        mail_router_domain: String(cfg.mail_router_domain || ""),
        patrol_enabled: !!cfg.patrol_enabled,
        patrol_interval_min: Number(cfg.patrol_interval_min || 30),
        refill_enabled: !!cfg.refill_enabled,
        refill_min_healthy: Number(cfg.refill_min_healthy || 5),
        refill_batch: Number(cfg.refill_batch || 10),
        cleanup_quota_enabled: !!cfg.cleanup_quota_enabled,
        cleanup_on_patrol: cfg.cleanup_on_patrol !== false,
        cleanup_backup: cfg.cleanup_backup !== false,
        cleanup_dry_run: !!cfg.cleanup_dry_run,
      };
      if (cpaKey.trim()) body.cpa_management_key = cpaKey.trim();
      if (resinToken.trim()) body.resin_token = resinToken.trim();
      if (mailRouterAPIKey.trim()) body.mail_router_api_key = mailRouterAPIKey.trim();
      await api("/api/config", { method: "PUT", body: JSON.stringify(body) });
      setCpaKey("");
      setResinToken("");
      setMailRouterAPIKey("");
      setMsg("已保存");
      await load();
    } catch (e) {
      setMsg(e instanceof Error ? e.message : "保存失败");
    } finally {
      setBusy(false);
    }
  }

  async function importInfrastructure(source: "link" | "federation") {
    setBusy(true);
    setMsg("");
    try {
      if (source === "link") {
        const parsed = new URL(importLink.trim());
        const raw = parsed.searchParams.get("infra");
        if (!raw) throw new Error("导入链接缺少 infra 参数");
        const imported = decodeInfrastructure(raw);
        await api("/api/infrastructure/import", {
          method: "POST",
          body: JSON.stringify({ source: "link", import: imported }),
        });
      } else {
        await api("/api/infrastructure/import", {
          method: "POST",
          body: JSON.stringify({
            source: "federation",
            master_url: masterURL.trim(),
            cluster_token: masterToken.trim(),
          }),
        });
      }
      setImportLink("");
      setMasterToken("");
      setMsg("基础设施配置已导入");
      await load();
    } catch (e) {
      setMsg(e instanceof Error ? e.message : "导入失败");
    } finally {
      setBusy(false);
    }
  }

  function createImportLink() {
    const payload = {
      version: 1,
      resin_proxy: String(cfg.resin_proxy || ""),
      resin_platform: String(cfg.resin_platform || "Default"),
      mail_router_url: String(cfg.mail_router_url || ""),
      mail_router_domain: String(cfg.mail_router_domain || ""),
    };
    const encoded = encodeInfrastructure(payload);
    const link = `${window.location.origin}/settings/?infra=${encoded}`;
    setImportLink(link);
    void navigator.clipboard?.writeText(link);
    setMsg("已生成并复制无密钥导入链接");
  }

  useEffect(() => {
    const raw = new URLSearchParams(window.location.search).get("infra");
    if (raw) setImportLink(`${window.location.origin}/settings/?infra=${raw}`);
  }, []);
  async function testConn() {
    setBusy(true);
    try {
      await api("/api/pool/test-connection", { method: "POST", body: "{}" });
      setMsg("CPA 连接正常");
    } catch (e) {
      setMsg(e instanceof Error ? e.message : "连接失败");
    } finally {
      setBusy(false);
    }
  }

  return (
    <AdminShell>
      <PageHeader
        title="设置"
        description="外观 · CPA · 代理 · 巡检 · 通知入口"
        actions={
          <>
            <Button
              size="sm"
              variant="secondary"
              loading={busy}
              onClick={() => void testConn()}
            >
              测试 CPA
            </Button>
            <Button size="sm" loading={busy} onClick={() => void save()}>
              保存
            </Button>
          </>
        }
      />
      {msg ? (
        <div className="mb-3">
          <Text>{msg}</Text>
        </div>
      ) : null}

      <LayerCard className="mb-4">
        <LayerCard.Secondary>外观</LayerCard.Secondary>
        <LayerCard.Primary>
          <div className="flex flex-col gap-3 sm:max-w-md">
            <Select
              label="颜色主题"
              value={theme}
              onValueChange={(v) => {
                if (!v) return;
                setTheme(v as ThemeMode);
              }}
            >
              <Select.Option value="system">跟随系统</Select.Option>
              <Select.Option value="light">浅色</Select.Option>
              <Select.Option value="dark">深色</Select.Option>
            </Select>
            <Text size="xs" variant="secondary">
              当前生效：{resolved === "dark" ? "深色" : "浅色"}
              {theme === "system" ? "（跟随系统）" : ""}
            </Text>
          </div>
        </LayerCard.Primary>
      </LayerCard>

      <LayerCard className="mb-4">
        <LayerCard.Secondary>通知</LayerCard.Secondary>
        <LayerCard.Primary>
          <div className="flex flex-col gap-3 sm:max-w-xl">
            <Text>
              飞书机器人、SMTP 邮件、Webhook 等通知渠道统一在「系统 → 通知」管理，支持列表新建、测试与事件订阅。
            </Text>
            <Text size="xs" variant="secondary">
              典型用途：注册完成/失败推送、号池不足告警、巡检失败提醒。密钥仅存本机 notifications.json。
            </Text>
            <div>
              <Button
                size="sm"
                onClick={() => {
                  window.location.href = "/notifications/";
                }}
              >
                打开通知渠道
              </Button>
            </div>
          </div>
        </LayerCard.Primary>
      </LayerCard>

      <div className="grid gap-4 lg:grid-cols-2">
        <LayerCard>
          <LayerCard.Secondary>CPA Management</LayerCard.Secondary>
          <LayerCard.Primary>
            <div className="flex flex-col gap-3">
              <Input
                label="Base URL"
                value={String(cfg.cpa_management_base || "")}
                onChange={(e) => setField("cpa_management_base", e.target.value)}
                placeholder="http://127.0.0.1:8317/v0/management"
              />
              <Input
                label="Management Key"
                type="password"
                value={cpaKey}
                onChange={(e) => setCpaKey(e.target.value)}
                placeholder={
                  cfg.cpa_management_key_set
                    ? "已设置 · 留空不改"
                    : "Management Key"
                }
              />
              <Switch
                label="注册成功自动上传"
                checked={!!cfg.cpa_upload_enabled}
                onCheckedChange={(v) => setField("cpa_upload_enabled", !!v)}
              />
            </div>
          </LayerCard.Primary>
        </LayerCard>

        <LayerCard>
          <LayerCard.Secondary>代理 / 清障</LayerCard.Secondary>
          <LayerCard.Primary>
            <div className="flex flex-col gap-3">
              <Input
                label="REGISTER_PROXY"
                value={String(cfg.register_proxy || "")}
                onChange={(e) => setField("register_proxy", e.target.value)}
              />
              <Input
                label="FLARESOLVERR_URL"
                value={String(cfg.flaresolverr_url || "")}
                onChange={(e) => setField("flaresolverr_url", e.target.value)}
              />
              <Input
                label="EMAIL_MODE"
                value={String(cfg.email_mode || "tempmail")}
                onChange={(e) => setField("email_mode", e.target.value)}
              />
            </div>
          </LayerCard.Primary>
        </LayerCard>

        <LayerCard>
          <LayerCard.Secondary>Resin 粘性代理</LayerCard.Secondary>
          <LayerCard.Primary>
            <div className="flex flex-col gap-3">
              <Input
                label="Resin Proxy URL"
                value={String(cfg.resin_proxy || "")}
                onChange={(e) => setField("resin_proxy", e.target.value)}
                placeholder="http://127.0.0.1:2260"
              />
              <Input
                label="Resin Token"
                type="password"
                value={resinToken}
                onChange={(e) => setResinToken(e.target.value)}
                placeholder={cfg.resin_token_set ? "已设置 · 留空不改" : "RESIN_PROXY_TOKEN"}
              />
              <Input
                label="Platform"
                value={String(cfg.resin_platform || "Default")}
                onChange={(e) => setField("resin_platform", e.target.value)}
                placeholder="Default"
              />
            </div>
          </LayerCard.Primary>
        </LayerCard>

        <LayerCard>
          <LayerCard.Secondary>Touch Mail Router</LayerCard.Secondary>
          <LayerCard.Primary>
            <div className="flex flex-col gap-3">
              <Input
                label="API URL"
                value={String(cfg.mail_router_url || "")}
                onChange={(e) => setField("mail_router_url", e.target.value)}
                placeholder="https://mail.example.com"
              />
              <Input
                label="API Key"
                type="password"
                value={mailRouterAPIKey}
                onChange={(e) => setMailRouterAPIKey(e.target.value)}
                placeholder={cfg.mail_router_api_key_set ? "已设置 · 留空不改" : "DuckMail API Key"}
              />
              <Input
                label="邮箱域名"
                value={String(cfg.mail_router_domain || "")}
                onChange={(e) => setField("mail_router_domain", e.target.value)}
                placeholder="inbound.example.com"
              />
            </div>
          </LayerCard.Primary>
        </LayerCard>

        <LayerCard>
          <LayerCard.Secondary>快速导入</LayerCard.Secondary>
          <LayerCard.Primary>
            <div className="flex flex-col gap-3 sm:max-w-xl">
              <Input
                label="无密钥导入链接"
                value={importLink}
                onChange={(e) => setImportLink(e.target.value)}
                placeholder="粘贴由另一台面板生成的链接"
              />
              <div className="flex flex-wrap gap-2">
                <Button size="sm" variant="secondary" disabled={busy} onClick={createImportLink}>
                  生成链接
                </Button>
                <Button size="sm" loading={busy} disabled={!importLink.trim()} onClick={() => void importInfrastructure("link")}>
                  导入链接
                </Button>
              </div>
              <Text size="xs" variant="secondary">
                链接仅包含地址、域名和 Resin Platform，不包含任何 Token 或 API Key。
              </Text>
            </div>
          </LayerCard.Primary>
        </LayerCard>

        <LayerCard>
          <LayerCard.Secondary>从联邦主节点拉取</LayerCard.Secondary>
          <LayerCard.Primary>
            <div className="flex flex-col gap-3 sm:max-w-xl">
              <Input
                label="主节点 URL（留空使用主从配置首项）"
                value={masterURL}
                onChange={(e) => setMasterURL(e.target.value)}
                placeholder="https://master.example.com"
              />
              <Input
                label="联邦密钥（留空复用主从配置）"
                type="password"
                value={masterToken}
                onChange={(e) => setMasterToken(e.target.value)}
                placeholder="X-Cluster-Token"
              />
              <div>
                <Button size="sm" loading={busy} onClick={() => void importInfrastructure("federation")}>
                  拉取并导入
                </Button>
              </div>
              <Text size="xs" variant="secondary">
                主节点必须启用“共享基础设施配置”；凭据只经已鉴权联邦请求传输，不写入导入链接。
              </Text>
            </div>
          </LayerCard.Primary>
        </LayerCard>

        <LayerCard>
          <LayerCard.Secondary>巡检 / 补号</LayerCard.Secondary>
          <LayerCard.Primary>
            <div className="flex flex-col gap-3">
              <Switch
                label="启用定时巡检"
                checked={!!cfg.patrol_enabled}
                onCheckedChange={(v) => setField("patrol_enabled", !!v)}
              />
              <Input
                label="巡检间隔（分钟）"
                value={String(cfg.patrol_interval_min ?? 30)}
                onChange={(e) =>
                  setField(
                    "patrol_interval_min",
                    parseInt(e.target.value, 10) || 30,
                  )
                }
              />
              <Switch
                label="健康不足自动补号"
                checked={!!cfg.refill_enabled}
                onCheckedChange={(v) => setField("refill_enabled", !!v)}
              />
              <Input
                label="最低健康数"
                value={String(cfg.refill_min_healthy ?? 5)}
                onChange={(e) =>
                  setField(
                    "refill_min_healthy",
                    parseInt(e.target.value, 10) || 5,
                  )
                }
              />
            </div>
          </LayerCard.Primary>
        </LayerCard>

        <LayerCard>
          <LayerCard.Secondary>清理耗尽号</LayerCard.Secondary>
          <LayerCard.Primary>
            <div className="flex flex-col gap-3">
              <Switch
                label="允许定时清理"
                checked={!!cfg.cleanup_quota_enabled}
                onCheckedChange={(v) => setField("cleanup_quota_enabled", !!v)}
              />
              <Switch
                label="巡检后自动清理"
                checked={cfg.cleanup_on_patrol !== false}
                onCheckedChange={(v) => setField("cleanup_on_patrol", !!v)}
              />
              <Switch
                label="删除前备份"
                checked={cfg.cleanup_backup !== false}
                onCheckedChange={(v) => setField("cleanup_backup", !!v)}
              />
              <Switch
                label="演练模式（不真删）"
                checked={!!cfg.cleanup_dry_run}
                onCheckedChange={(v) => setField("cleanup_dry_run", !!v)}
              />
            </div>
          </LayerCard.Primary>
        </LayerCard>
      </div>
    </AdminShell>
  );
}
