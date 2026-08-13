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
  const [duckMailKey, setDuckMailKey] = useState("");
  const [cloudflareKey, setCloudflareKey] = useState("");
  const [cloudflareCustomAuth, setCloudflareCustomAuth] = useState("");
  const [cloudMailPassword, setCloudMailPassword] = useState("");
  const [mailNestKey, setMailNestKey] = useState("");
  const [moeMailKey, setMoeMailKey] = useState("");
  const [yydsKey, setYYDSKey] = useState("");
  const [yydsJWT, setYYDSJWT] = useState("");
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
        cpa_upload_timeout_sec: Number(cfg.cpa_upload_timeout_sec || 30),
        cpa_upload_retries: Number(cfg.cpa_upload_retries || 2),
        cpa_upload_name_template: String(cfg.cpa_upload_name_template || "{email}.json"),
        cpa_upload_verify: cfg.cpa_upload_verify !== false,
        cpa_upload_mode: String(cfg.cpa_upload_mode || "multipart"),
        register_proxy: String(cfg.register_proxy || ""),
        http_proxy: String(cfg.http_proxy || ""),
        https_proxy: String(cfg.https_proxy || ""),
        no_proxy: String(cfg.no_proxy || ""),
        clearance_enabled: cfg.clearance_enabled !== false,
        clearance_proxy: String(cfg.clearance_proxy || ""),
        clearance_urls: String(cfg.clearance_urls || ""),
        flaresolverr_url: String(cfg.flaresolverr_url || ""),
        turnstile_provider: String(cfg.turnstile_provider || "browser"),
        lite_solver_url: String(cfg.lite_solver_url || ""),
        turnstile_chrome_path: String(cfg.turnstile_chrome_path || ""),
        turnstile_python: String(cfg.turnstile_python || ""),
        turnstile_script: String(cfg.turnstile_script || ""),
        turnstile_inject_clearance: !!cfg.turnstile_inject_clearance,
        protocol_http: cfg.protocol_http !== false,
        http_pool_size: Number(cfg.http_pool_size || 8),
        oauth_min_interval_sec: Number(cfg.oauth_min_interval_sec || 10),
        oauth_retry_sec: Number(cfg.oauth_retry_sec || 60),
        physical_cap: Number(cfg.physical_cap || 0),
        email_mode: String(cfg.email_mode || "tempmail"),
        email_domain: String(cfg.email_domain || ""),
        email_api: String(cfg.email_api || ""),
        email_default_domains: String(cfg.email_default_domains || ""),
        duckmail_base: String(cfg.duckmail_base || ""),
        cloudflare_base: String(cfg.cloudflare_base || ""),
        cloudflare_auth_mode: String(cfg.cloudflare_auth_mode || "none"),
        cloudflare_randomize_subdomain: cfg.cloudflare_randomize_subdomain !== false,
        cloudmail_url: String(cfg.cloudmail_url || ""),
        cloudmail_admin_email: String(cfg.cloudmail_admin_email || ""),
        mailnest_project_code: String(cfg.mailnest_project_code || "x-ai001"),
        moemail_base: String(cfg.moemail_base || ""),
        moemail_domain: String(cfg.moemail_domain || ""),
        moemail_expiry_ms: Number(cfg.moemail_expiry_ms || 3600000),
        yyds_domain: String(cfg.yyds_domain || ""),
        resin_proxy: String(cfg.resin_proxy || ""),
        resin_platform: String(cfg.resin_platform || "Default"),
        mail_router_url: String(cfg.mail_router_url || ""),
        mail_router_domain: String(cfg.mail_router_domain || ""),
        bridge_reg_factory_root: String(cfg.bridge_reg_factory_root || ""),
        bridge_grok_panel_root: String(cfg.bridge_grok_panel_root || ""),
        bridge_outlook_pool_dir: String(cfg.bridge_outlook_pool_dir || ""),
        bridge_python: String(cfg.bridge_python || ""),
        upload_concurrency: Number(cfg.upload_concurrency || 3),
        upload_batch_size: Number(cfg.upload_batch_size || 20),
        export_batch_size: Number(cfg.export_batch_size || 500),
        export_concurrency: Number(cfg.export_concurrency || 15),
        patrol_enabled: !!cfg.patrol_enabled,
        patrol_interval_min: Number(cfg.patrol_interval_min || 30),
        patrol_deep_probe: !!cfg.patrol_deep_probe,
        patrol_concurrency: Number(cfg.patrol_concurrency || 10),
        quota_per_account: Number(cfg.quota_per_account || 60),
        refill_enabled: !!cfg.refill_enabled,
        refill_min_healthy: Number(cfg.refill_min_healthy || 5),
        refill_batch: Number(cfg.refill_batch || 10),
        refill_cooldown_min: Number(cfg.refill_cooldown_min || 60),
        refill_daily_cap: Number(cfg.refill_daily_cap || 50),
        local_pool_auto_import: cfg.local_pool_auto_import !== false,
        local_pool_auto_sync: !!cfg.local_pool_auto_sync,
        cleanup_quota_enabled: !!cfg.cleanup_quota_enabled,
        cleanup_on_patrol: cfg.cleanup_on_patrol !== false,
        cleanup_backup: cfg.cleanup_backup !== false,
        cleanup_dry_run: !!cfg.cleanup_dry_run,
      };
      if (cpaKey.trim()) body.cpa_management_key = cpaKey.trim();
      if (resinToken.trim()) body.resin_token = resinToken.trim();
      if (mailRouterAPIKey.trim()) body.mail_router_api_key = mailRouterAPIKey.trim();
      if (duckMailKey.trim()) body.duckmail_key = duckMailKey.trim();
      if (cloudflareKey.trim()) body.cloudflare_key = cloudflareKey.trim();
      if (cloudflareCustomAuth.trim()) body.cloudflare_custom_auth = cloudflareCustomAuth.trim();
      if (cloudMailPassword.trim()) body.cloudmail_password = cloudMailPassword.trim();
      if (mailNestKey.trim()) body.mailnest_key = mailNestKey.trim();
      if (moeMailKey.trim()) body.moemail_key = moeMailKey.trim();
      if (yydsKey.trim()) body.yyds_key = yydsKey.trim();
      if (yydsJWT.trim()) body.yyds_jwt = yydsJWT.trim();
      await api("/api/config", { method: "PUT", body: JSON.stringify(body) });
      setCpaKey("");
      setResinToken("");
      setMailRouterAPIKey("");
      setDuckMailKey("");
      setCloudflareKey("");
      setCloudflareCustomAuth("");
      setCloudMailPassword("");
      setMailNestKey("");
      setMoeMailKey("");
      setYYDSKey("");
      setYYDSJWT("");
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
          <LayerCard.Secondary>注册网络与执行</LayerCard.Secondary>
          <LayerCard.Primary>
            <div className="flex flex-col gap-3">
              <Input label="注册 HTTP 代理" value={String(cfg.register_proxy || "")} onChange={(e) => setField("register_proxy", e.target.value)} />
              <Input label="通用 HTTP 代理" value={String(cfg.http_proxy || "")} onChange={(e) => setField("http_proxy", e.target.value)} />
              <Input label="通用 HTTPS 代理" value={String(cfg.https_proxy || "")} onChange={(e) => setField("https_proxy", e.target.value)} />
              <Input label="NO_PROXY" value={String(cfg.no_proxy || "")} onChange={(e) => setField("no_proxy", e.target.value)} />
              <Input label="FlareSolverr URL" value={String(cfg.flaresolverr_url || "")} onChange={(e) => setField("flaresolverr_url", e.target.value)} />
              <Input label="清障代理" value={String(cfg.clearance_proxy || "")} onChange={(e) => setField("clearance_proxy", e.target.value)} />
              <Input label="清障目标 URL（逗号分隔）" value={String(cfg.clearance_urls || "")} onChange={(e) => setField("clearance_urls", e.target.value)} />
              <Select label="Turnstile 方式" value={String(cfg.turnstile_provider || "browser")} onValueChange={(v) => setField("turnstile_provider", v || "browser")}>
                <Select.Option value="browser">本机浏览器</Select.Option>
                <Select.Option value="lite">外部 Solver</Select.Option>
              </Select>
              <Input label="Solver URL" value={String(cfg.lite_solver_url || "")} onChange={(e) => setField("lite_solver_url", e.target.value)} />
              <Input label="Turnstile Chrome 路径" value={String(cfg.turnstile_chrome_path || "")} onChange={(e) => setField("turnstile_chrome_path", e.target.value)} placeholder="留空自动检测" />
              <Input label="Turnstile Python" value={String(cfg.turnstile_python || "")} onChange={(e) => setField("turnstile_python", e.target.value)} placeholder="留空自动检测" />
              <Input label="Turnstile Mint 脚本" value={String(cfg.turnstile_script || "")} onChange={(e) => setField("turnstile_script", e.target.value)} placeholder="留空自动检测" />
              <div className="grid gap-3 sm:grid-cols-2">
                <Input label="HTTP 并发池" value={String(cfg.http_pool_size ?? 8)} onChange={(e) => setField("http_pool_size", Number(e.target.value) || 1)} />
                <Input label="物理并发上限（0=自动）" value={String(cfg.physical_cap ?? 0)} onChange={(e) => setField("physical_cap", Number(e.target.value) || 0)} />
                <Input label="OAuth 最小间隔（秒）" value={String(cfg.oauth_min_interval_sec ?? 10)} onChange={(e) => setField("oauth_min_interval_sec", Number(e.target.value) || 0)} />
                <Input label="OAuth 重试间隔（秒）" value={String(cfg.oauth_retry_sec ?? 60)} onChange={(e) => setField("oauth_retry_sec", Number(e.target.value) || 1)} />
              </div>
              <Switch label="启用清障" checked={cfg.clearance_enabled !== false} onCheckedChange={(v) => setField("clearance_enabled", !!v)} />
              <Switch label="注入 clearance 到 Turnstile Mint" checked={!!cfg.turnstile_inject_clearance} onCheckedChange={(v) => setField("turnstile_inject_clearance", !!v)} />
              <Switch label="优先 HTTP 注册协议" checked={cfg.protocol_http !== false} onCheckedChange={(v) => setField("protocol_http", !!v)} />
            </div>
          </LayerCard.Primary>
        </LayerCard>

        <LayerCard>
          <LayerCard.Secondary>邮箱 Provider</LayerCard.Secondary>
          <LayerCard.Primary>
            <div className="flex flex-col gap-3">
              <Select label="邮箱模式" value={String(cfg.email_mode || "tempmail")} onValueChange={(v) => setField("email_mode", v || "tempmail")}>
                <Select.Option value="tempmail">免费临时邮箱</Select.Option>
                <Select.Option value="custom">自建邮箱 Webhook</Select.Option>
                <Select.Option value="duckmail">DuckMail</Select.Option>
                <Select.Option value="cloudflare">Cloudflare Worker</Select.Option>
                <Select.Option value="cloudmail">CloudMail</Select.Option>
                <Select.Option value="mailnest">MailNest</Select.Option>
                <Select.Option value="moemail">MoeMail</Select.Option>
                <Select.Option value="yyds">YYDS Mail</Select.Option>
              </Select>
              <Input label="默认邮箱域名（逗号分隔）" value={String(cfg.email_default_domains || "")} onChange={(e) => setField("email_default_domains", e.target.value)} />
              <Input label="自建邮箱域名" value={String(cfg.email_domain || "")} onChange={(e) => setField("email_domain", e.target.value)} />
              <Input label="自建邮箱 API" value={String(cfg.email_api || "")} onChange={(e) => setField("email_api", e.target.value)} />
              <Input label="DuckMail API Base" value={String(cfg.duckmail_base || "")} onChange={(e) => setField("duckmail_base", e.target.value)} />
              <Input label="DuckMail API Key" type="password" value={duckMailKey} onChange={(e) => setDuckMailKey(e.target.value)} placeholder={cfg.duckmail_key_set ? "已设置 · 留空不改" : "可选"} />
              <Input label="Cloudflare 邮箱 API Base" value={String(cfg.cloudflare_base || "")} onChange={(e) => setField("cloudflare_base", e.target.value)} />
              <Input label="Cloudflare API Key" type="password" value={cloudflareKey} onChange={(e) => setCloudflareKey(e.target.value)} placeholder={cfg.cloudflare_key_set ? "已设置 · 留空不改" : "API Key"} />
              <Select label="Cloudflare 鉴权方式" value={String(cfg.cloudflare_auth_mode || "none")} onValueChange={(v) => setField("cloudflare_auth_mode", v || "none")}>
                <Select.Option value="none">无</Select.Option>
                <Select.Option value="x-api-key">X-API-Key</Select.Option>
                <Select.Option value="x-admin-auth">X-Admin-Auth</Select.Option>
                <Select.Option value="bearer">Bearer</Select.Option>
              </Select>
              <Input label="Cloudflare 自定义鉴权" type="password" value={cloudflareCustomAuth} onChange={(e) => setCloudflareCustomAuth(e.target.value)} placeholder={cfg.cloudflare_custom_auth_set ? "已设置 · 留空不改" : "可选"} />
              <Switch label="Cloudflare 随机子域名" checked={cfg.cloudflare_randomize_subdomain !== false} onCheckedChange={(v) => setField("cloudflare_randomize_subdomain", !!v)} />
              <Input label="CloudMail URL" value={String(cfg.cloudmail_url || "")} onChange={(e) => setField("cloudmail_url", e.target.value)} />
              <Input label="CloudMail 管理员邮箱" value={String(cfg.cloudmail_admin_email || "")} onChange={(e) => setField("cloudmail_admin_email", e.target.value)} />
              <Input label="CloudMail 密码" type="password" value={cloudMailPassword} onChange={(e) => setCloudMailPassword(e.target.value)} placeholder={cfg.cloudmail_password_set ? "已设置 · 留空不改" : "密码"} />
              <Input label="MailNest API Key" type="password" value={mailNestKey} onChange={(e) => setMailNestKey(e.target.value)} placeholder={cfg.mailnest_key_set ? "已设置 · 留空不改" : "API Key"} />
              <Input label="MailNest 项目编号" value={String(cfg.mailnest_project_code || "x-ai001")} onChange={(e) => setField("mailnest_project_code", e.target.value)} />
              <Input label="MoeMail API Base" value={String(cfg.moemail_base || "")} onChange={(e) => setField("moemail_base", e.target.value)} />
              <Input label="MoeMail API Key" type="password" value={moeMailKey} onChange={(e) => setMoeMailKey(e.target.value)} placeholder={cfg.moemail_key_set ? "已设置 · 留空不改" : "API Key"} />
              <Input label="MoeMail 域名" value={String(cfg.moemail_domain || "")} onChange={(e) => setField("moemail_domain", e.target.value)} />
              <Input label="MoeMail 过期时间（毫秒）" value={String(cfg.moemail_expiry_ms || 3600000)} onChange={(e) => setField("moemail_expiry_ms", Number(e.target.value) || 3600000)} />
              <Input label="YYDS API Key" type="password" value={yydsKey} onChange={(e) => setYYDSKey(e.target.value)} placeholder={cfg.yyds_key_set ? "已设置 · 留空不改" : "API Key"} />
              <Input label="YYDS JWT" type="password" value={yydsJWT} onChange={(e) => setYYDSJWT(e.target.value)} placeholder={cfg.yyds_jwt_set ? "已设置 · 留空不改" : "JWT"} />
              <Input label="YYDS 默认域名" value={String(cfg.yyds_domain || "")} onChange={(e) => setField("yyds_domain", e.target.value)} />
            </div>
          </LayerCard.Primary>
        </LayerCard>

        <LayerCard>
          <LayerCard.Secondary>Bridge 注册器</LayerCard.Secondary>
          <LayerCard.Primary>
            <div className="flex flex-col gap-3">
              <Input label="reg-factory 根目录" value={String(cfg.bridge_reg_factory_root || "")} onChange={(e) => setField("bridge_reg_factory_root", e.target.value)} placeholder="/opt/reg-factory" />
              <Input label="grok-register-panel 根目录" value={String(cfg.bridge_grok_panel_root || "")} onChange={(e) => setField("bridge_grok_panel_root", e.target.value)} placeholder="/opt/grok-register-panel" />
              <Input label="Outlook 邮箱池目录" value={String(cfg.bridge_outlook_pool_dir || "")} onChange={(e) => setField("bridge_outlook_pool_dir", e.target.value)} placeholder="/data/outlook-pool" />
              <Input label="Bridge Python" value={String(cfg.bridge_python || "")} onChange={(e) => setField("bridge_python", e.target.value)} placeholder="留空自动检测" />
            </div>
          </LayerCard.Primary>
        </LayerCard>

        <LayerCard>
          <LayerCard.Secondary>注册与上传细节</LayerCard.Secondary>
          <LayerCard.Primary>
            <div className="flex flex-col gap-3">
              <Input label="临时邮箱创建重试次数" value={String(cfg.tempmail_lol_retries || 30)} onChange={(e) => setField("tempmail_lol_retries", Number(e.target.value) || 1)} />
              <Input label="临时邮箱轮询间隔（毫秒）" value={String(cfg.tempmail_lol_min_interval_ms || 1500)} onChange={(e) => setField("tempmail_lol_min_interval_ms", Number(e.target.value) || 100)} />
              <Input label="CPA 上传超时（秒）" value={String(cfg.cpa_upload_timeout_sec || 30)} onChange={(e) => setField("cpa_upload_timeout_sec", Number(e.target.value) || 1)} />
              <Input label="CPA 上传重试次数" value={String(cfg.cpa_upload_retries || 2)} onChange={(e) => setField("cpa_upload_retries", Number(e.target.value) || 0)} />
              <Input label="CPA 文件名模板" value={String(cfg.cpa_upload_name_template || "{email}.json")} onChange={(e) => setField("cpa_upload_name_template", e.target.value)} />
              <Select label="CPA 上传格式" value={String(cfg.cpa_upload_mode || "multipart")} onValueChange={(v) => setField("cpa_upload_mode", v || "multipart")}>
                <Select.Option value="multipart">Multipart</Select.Option>
                <Select.Option value="json">JSON</Select.Option>
              </Select>
              <Switch label="CPA 上传后验证" checked={cfg.cpa_upload_verify !== false} onCheckedChange={(v) => setField("cpa_upload_verify", !!v)} />
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
          <LayerCard.Secondary>运行容量与号池</LayerCard.Secondary>
          <LayerCard.Primary>
            <div className="flex flex-col gap-3">
              <div className="grid gap-3 sm:grid-cols-2">
                <Input label="上传并发" value={String(cfg.upload_concurrency ?? 3)} onChange={(e) => setField("upload_concurrency", Number(e.target.value) || 1)} />
                <Input label="上传批次大小" value={String(cfg.upload_batch_size ?? 20)} onChange={(e) => setField("upload_batch_size", Number(e.target.value) || 1)} />
                <Input label="导出批次大小" value={String(cfg.export_batch_size ?? 500)} onChange={(e) => setField("export_batch_size", Number(e.target.value) || 1)} />
                <Input label="导出并发" value={String(cfg.export_concurrency ?? 15)} onChange={(e) => setField("export_concurrency", Number(e.target.value) || 1)} />
                <Input label="巡检并发" value={String(cfg.patrol_concurrency ?? 10)} onChange={(e) => setField("patrol_concurrency", Number(e.target.value) || 1)} />
                <Input label="单健康号额度估算" value={String(cfg.quota_per_account ?? 60)} onChange={(e) => setField("quota_per_account", Number(e.target.value) || 1)} />
                <Input label="补号冷却（分钟）" value={String(cfg.refill_cooldown_min ?? 60)} onChange={(e) => setField("refill_cooldown_min", Number(e.target.value) || 1)} />
                <Input label="每日补号上限" value={String(cfg.refill_daily_cap ?? 50)} onChange={(e) => setField("refill_daily_cap", Number(e.target.value) || 1)} />
              </div>
              <Switch label="深度巡检" checked={!!cfg.patrol_deep_probe} onCheckedChange={(v) => setField("patrol_deep_probe", !!v)} />
              <Switch label="注册产物自动进入本地号池" checked={cfg.local_pool_auto_import !== false} onCheckedChange={(v) => setField("local_pool_auto_import", !!v)} />
              <Switch label="本地号池自动同步 CPA" checked={!!cfg.local_pool_auto_sync} onCheckedChange={(v) => setField("local_pool_auto_sync", !!v)} />
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
