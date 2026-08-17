"use client";

import { useEffect, useState } from "react";
import {
  ArrowsClockwiseIcon,
  PlusIcon,
  TrashIcon,
} from "@phosphor-icons/react";
import {
  Badge,
  Button,
  Input,
  LayerCard,
  Select,
  Switch,
  Tabs,
  Text,
} from "@cloudflare/kumo";
import { AdminShell } from "@/components/admin-shell";
import { PageHeader } from "@/components/page-header";
import { resetOnboarding } from "@/components/onboarding";
import {
  api,
  type Health,
  type PanelConfig,
  type PluginRepository,
} from "@/lib/api";
import { useTheme, type ThemeMode } from "@/lib/theme";
import {
  COMMON_TIMEZONES,
  formatDateTime,
  timezoneOffsetLabel,
  useTimezone,
} from "@/lib/timezone";

type TabKey = "general" | "plugins" | "register" | "email" | "pool" | "patrol" | "import";

const TAB_ITEMS: { value: TabKey; label: string }[] = [
  { value: "general", label: "通用" },
  { value: "plugins", label: "插件源" },
  { value: "register", label: "注册执行" },
  { value: "email", label: "邮箱" },
  { value: "pool", label: "号池与上传" },
  { value: "patrol", label: "巡检清理" },
  { value: "import", label: "导入迁移" },
];

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

/**
 * 下拉项文案；未收录的时区（例如旧数据）直接显示 IANA 名称。
 *
 * `emptyLabel` is required because an empty value means different things in the
 * two selects ("follow the browser" vs "follow the server"), and `renderValue`
 * would otherwise paint the trigger blank instead of showing the placeholder.
 */
function timezoneOptionLabel(value: string, emptyLabel: string) {
  if (!value) return emptyLabel;
  const known = COMMON_TIMEZONES.find((option) => option.value === value);
  return known ? `${known.label}（${timezoneOffsetLabel(value)}）` : value;
}

function snapshotOf(cfg: PanelConfig) {
  return JSON.stringify(
    Object.keys(cfg)
      .sort()
      .map((k) => [k, cfg[k]]),
  );
}

export default function SettingsPage() {
  const { theme, setTheme, resolved } = useTheme();
  const { timezone, override: timezoneOverride, setOverride: setTimezoneOverride, browser: browserTimezone } =
    useTimezone();
  const [tab, setTab] = useState<TabKey>("general");
  const [cfg, setCfg] = useState<PanelConfig>({});
  const [savedSnap, setSavedSnap] = useState("");
  const [cpaKey, setCpaKey] = useState("");
  const [savedCpaKey, setSavedCpaKey] = useState("");
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
  const [repositories, setRepositories] = useState<PluginRepository[]>([]);
  const [repositoryName, setRepositoryName] = useState("");
  const [repositoryURL, setRepositoryURL] = useState("");
  const [repositoryBusy, setRepositoryBusy] = useState("");
  const [msg, setMsg] = useState("");
  const [busy, setBusy] = useState(false);
  const [cpaTestBusy, setCpaTestBusy] = useState(false);
  const [cpaTestResult, setCpaTestResult] = useState<string | null>(null);
  const [buildInfo, setBuildInfo] = useState<NonNullable<Health["build"]> | null>(null);

  const secrets = [
    resinToken,
    mailRouterAPIKey,
    duckMailKey,
    cloudflareKey,
    cloudflareCustomAuth,
    cloudMailPassword,
    mailNestKey,
    moeMailKey,
    yydsKey,
    yydsJWT,
  ];
  const dirty =
    cpaKey.trim() !== savedCpaKey ||
    secrets.some((s) => s.trim() !== "") ||
    (savedSnap !== "" && snapshotOf(cfg) !== savedSnap);
  const emailMode = String(cfg.email_mode || "tempmail");
  const buildRepository = (buildInfo?.repository || "").replace(/\/$/, "");
  const buildCommit = buildInfo?.commit || "";
  const buildCommitURL =
    buildRepository && buildCommit && buildCommit !== "unknown"
      ? `${buildRepository}/commit/${encodeURIComponent(buildCommit)}`
      : "";
  const buildCommitLabel =
    buildCommit && buildCommit !== "unknown" ? buildCommit.slice(0, 12) : "未注入";
  const buildCommitTime = buildInfo?.commit_time
    ? formatDateTime(buildInfo.commit_time, timezone)
    : "";
  const serverTimezone = String(cfg.display_timezone || "");

  async function load() {
    const d = await api<{ config: PanelConfig }>("/api/config");
    const next = { ...(d.config || {}) };
    const revealedCpaKey = String(next.cpa_management_key || "");
    delete next.cpa_management_key;
    setCpaKey(revealedCpaKey);
    setSavedCpaKey(revealedCpaKey);
    setCfg(next);
    setSavedSnap(snapshotOf(next));
  }

  async function loadRepositories() {
    const response = await api<{
      repositories?: PluginRepository[];
    }>("/api/plugin-repositories");
    setRepositories(response.repositories || []);
  }

  async function loadBuildInfo() {
    const response = await api<Health>("/api/health");
    setBuildInfo(response.build || null);
  }

  useEffect(() => {
    void Promise.all([load(), loadRepositories(), loadBuildInfo()]).catch((e: unknown) =>
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
        display_timezone: String(cfg.display_timezone || ""),
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
        tempmail_lol_retries: Number(cfg.tempmail_lol_retries || 30),
        tempmail_lol_min_interval_ms: Number(cfg.tempmail_lol_min_interval_ms || 1500),
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
      if (cpaKey.trim() && cpaKey.trim() !== savedCpaKey) body.cpa_management_key = cpaKey.trim();
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

  async function createImportLink() {
    setBusy(true);
    setMsg("");
    try {
      const exported = await api<{ infrastructure: Record<string, string | number> }>(
        "/api/infrastructure/export",
      );
      const encoded = encodeInfrastructure(exported.infrastructure);
      const link = `${window.location.origin}/settings/?infra=${encoded}`;
      setImportLink(link);
      await navigator.clipboard?.writeText(link);
      setMsg("已生成并复制无密钥配置链接");
    } catch (e) {
      setMsg(e instanceof Error ? e.message : "生成链接失败");
    } finally {
      setBusy(false);
    }
  }

  useEffect(() => {
    const raw = new URLSearchParams(window.location.search).get("infra");
    if (raw) {
      setImportLink(`${window.location.origin}/settings/?infra=${raw}`);
      setTab("import");
    }
  }, []);

  async function addRepository() {
    if (!repositoryURL.trim()) return;
    setRepositoryBusy("add");
    setMsg("");
    try {
      await api("/api/plugin-repositories", {
        method: "POST",
        body: JSON.stringify({
          name: repositoryName.trim(),
          url: repositoryURL.trim(),
        }),
      });
      setRepositoryName("");
      setRepositoryURL("");
      await loadRepositories();
      setMsg("镜像仓库已添加");
    } catch (e) {
      setMsg(e instanceof Error ? e.message : "添加仓库失败");
    } finally {
      setRepositoryBusy("");
    }
  }

  async function syncRepository(repository: PluginRepository) {
    setRepositoryBusy(`sync:${repository.id}`);
    setMsg("");
    try {
      await api(`/api/plugin-repositories/${encodeURIComponent(repository.id)}/sync`, {
        method: "POST",
        body: "{}",
      });
      await loadRepositories();
      setMsg(`${repository.name} 同步完成`);
    } catch (e) {
      await loadRepositories().catch(() => undefined);
      setMsg(e instanceof Error ? e.message : "同步仓库失败");
    } finally {
      setRepositoryBusy("");
    }
  }

  async function removeRepository(repository: PluginRepository) {
    if (repository.official) return;
    if (!window.confirm(`删除镜像仓库“${repository.name}”？已安装插件不会被删除。`)) {
      return;
    }
    setRepositoryBusy(`delete:${repository.id}`);
    setMsg("");
    try {
      await api(`/api/plugin-repositories/${encodeURIComponent(repository.id)}`, {
        method: "DELETE",
      });
      await loadRepositories();
      setMsg("镜像仓库已删除");
    } catch (e) {
      setMsg(e instanceof Error ? e.message : "删除仓库失败");
    } finally {
      setRepositoryBusy("");
    }
  }

  async function testConn() {
    setCpaTestBusy(true);
    setCpaTestResult(null);
    try {
      const response = await api<{ ok: boolean; error?: string }>(
        "/api/pool/test-connection",
        {
          method: "POST",
          body: JSON.stringify({
            baseUrl: String(cfg.cpa_management_base || "").trim(),
            managementKey: cpaKey.trim() || undefined,
          }),
        },
      );
      setCpaTestResult(response.ok ? "CPA 连接正常" : `测试失败：${response.error || "连接失败"}`);
    } catch (e) {
      setCpaTestResult(`测试失败：${e instanceof Error ? e.message : "连接失败"}`);
    } finally {
      setCpaTestBusy(false);
    }
  }

  function providerTitle(title: string, mode: string) {
    return (
      <span className="flex items-center gap-2">
        {title}
        {emailMode === mode ? <Badge variant="primary">当前模式</Badge> : null}
      </span>
    );
  }

  return (
    <AdminShell>
      <PageHeader
        title="设置"
        description="通用 · 插件源 · 注册执行 · 邮箱 · 号池 · 巡检 · 导入"
        actions={
          tab === "plugins" ? undefined : (
            <>
              {dirty ? <Badge variant="primary">未保存</Badge> : null}
              <Button size="sm" loading={busy} onClick={() => void save()}>
                保存
              </Button>
            </>
          )
        }
      />

      {dirty ? (
        <div className="mb-3 rounded-md border border-amber-400/50 bg-amber-500/10 px-3 py-2">
          <Text size="sm">
            有未保存的更改 — 切换标签不会丢失，但离开页面前请点「保存」
          </Text>
        </div>
      ) : null}
      {msg ? (
        <div className="mb-3 rounded-md bg-kumo-contrast/5 px-3 py-2">
          <Text>{msg}</Text>
        </div>
      ) : null}

      <div className="mb-4">
        <Tabs
          variant="segmented"
          tabs={TAB_ITEMS}
          value={tab}
          onValueChange={(v) => {
            if (!v) return;
            setTab(v as TabKey);
          }}
        />
      </div>

      {tab === "general" ? (
        <div className="grid items-start gap-4 lg:grid-cols-2">
          <LayerCard>
            <LayerCard.Secondary>外观</LayerCard.Secondary>
            <LayerCard.Primary>
              <div className="flex flex-col gap-3">
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
                  ，主题即时生效，无需保存。
                </Text>
              </div>
            </LayerCard.Primary>
          </LayerCard>

          <LayerCard>
            <LayerCard.Secondary>时区</LayerCard.Secondary>
            <LayerCard.Primary>
              <div className="flex flex-col gap-3">
                <Select
                  label="显示时区"
                  value={timezoneOverride}
                  placeholder={`跟随浏览器（${browserTimezone}）`}
                  renderValue={(value) =>
                    timezoneOptionLabel(String(value), `跟随浏览器（${browserTimezone}）`)
                  }
                  onValueChange={(value) => setTimezoneOverride(value ?? "")}
                >
                  <Select.Option value="">
                    跟随浏览器（{browserTimezone}）
                  </Select.Option>
                  {COMMON_TIMEZONES.map((option) => (
                    <Select.Option key={option.value} value={option.value}>
                      {timezoneOptionLabel(option.value, "")}
                    </Select.Option>
                  ))}
                </Select>
                <Text size="xs" variant="secondary">
                  只影响当前浏览器上时间的展示方式，不会改变服务端存储的数据，也无需保存。
                </Text>

                <Select
                  label="服务端默认时区"
                  value={serverTimezone}
                  placeholder="跟随服务器系统时区"
                  renderValue={(value) =>
                    timezoneOptionLabel(String(value), "跟随服务器系统时区")
                  }
                  onValueChange={(value) => setField("display_timezone", value ?? "")}
                >
                  <Select.Option value="">跟随服务器系统时区</Select.Option>
                  {COMMON_TIMEZONES.map((option) => (
                    <Select.Option key={option.value} value={option.value}>
                      {timezoneOptionLabel(option.value, "")}
                    </Select.Option>
                  ))}
                </Select>
                <Text size="xs" variant="secondary">
                  仅在客户端未指定时区时作为兜底，例如通知推送与服务端渲染的报表；改动后需要点「保存」。
                </Text>

                <Text size="xs" variant="secondary">
                  当前生效：{timezone}（{timezoneOffsetLabel(timezone)}）
                </Text>
              </div>
            </LayerCard.Primary>
          </LayerCard>

          <LayerCard>
            <LayerCard.Secondary>通知</LayerCard.Secondary>
            <LayerCard.Primary>
              <div className="flex flex-col gap-3">
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

          <LayerCard>
            <LayerCard.Secondary>初次引导</LayerCard.Secondary>
            <LayerCard.Primary>
              <div className="flex flex-col gap-3">
                <Text>首次进入面板时出现的欢迎引导，看过一次后就不再自动弹出。</Text>
                <Text size="xs" variant="secondary">
                  记录只存在本浏览器，换浏览器或清缓存后会重新出现。
                </Text>
                <div>
                  <Button
                    size="sm"
                    variant="secondary"
                    onClick={() => {
                      resetOnboarding();
                      window.location.reload();
                    }}
                  >
                    重新查看引导
                  </Button>
                </div>
              </div>
            </LayerCard.Primary>
          </LayerCard>

          <LayerCard>
            <LayerCard.Secondary>当前版本</LayerCard.Secondary>
            <LayerCard.Primary>
              <div className="grid grid-cols-[auto_minmax(0,1fr)] items-baseline gap-x-4 gap-y-3">
                <Text size="xs" variant="secondary">版本</Text>
                <Text>{buildInfo?.version || "读取中…"}</Text>

                <Text size="xs" variant="secondary">GitHub 仓库</Text>
                {buildRepository ? (
                  <a
                    className="break-all text-sm underline underline-offset-2"
                    href={buildRepository}
                    rel="noreferrer"
                    target="_blank"
                  >
                    {buildRepository.replace(/^https?:\/\/github\.com\//, "")}
                  </a>
                ) : (
                  <Text>读取中…</Text>
                )}

                <Text size="xs" variant="secondary">Commit</Text>
                <div className="flex min-w-0 flex-wrap items-center gap-2">
                  {buildCommitURL ? (
                    <a
                      className="font-mono text-sm underline underline-offset-2"
                      href={buildCommitURL}
                      rel="noreferrer"
                      target="_blank"
                      title={buildCommit}
                    >
                      {buildCommitLabel}
                    </a>
                  ) : (
                    <code className="text-sm">{buildCommitLabel}</code>
                  )}
                  {buildInfo?.dirty ? <Badge variant="secondary">含本地修改</Badge> : null}
                </div>

                {buildCommitTime ? (
                  <>
                    <Text size="xs" variant="secondary">提交时间</Text>
                    <Text>{buildCommitTime}</Text>
                  </>
                ) : null}
              </div>
            </LayerCard.Primary>
          </LayerCard>
        </div>
      ) : null}

      {tab === "plugins" ? (
        <div className="flex flex-col gap-4">
          <LayerCard>
            <LayerCard.Secondary>添加镜像仓库</LayerCard.Secondary>
            <LayerCard.Primary>
              <div className="grid items-end gap-3 lg:grid-cols-[minmax(0,0.7fr)_minmax(0,1.3fr)_auto]">
                <Input
                  label="仓库名称（可选）"
                  value={repositoryName}
                  onChange={(event) => setRepositoryName(event.target.value)}
                  placeholder="团队插件镜像"
                />
                <Input
                  label="GitHub 仓库 URL"
                  value={repositoryURL}
                  onChange={(event) => setRepositoryURL(event.target.value)}
                  placeholder="https://github.com/owner/repository"
                />
                <Button
                  size="sm"
                  loading={repositoryBusy === "add"}
                  disabled={!repositoryURL.trim() || repositoryBusy !== "" && repositoryBusy !== "add"}
                  onClick={() => void addRepository()}
                >
                  <PlusIcon size={16} /> 添加
                </Button>
              </div>
              <Text size="xs" variant="secondary">
                仓库内可包含一个或多个 plugin.json。添加后需要先同步，插件才会出现在市场中。
              </Text>
            </LayerCard.Primary>
          </LayerCard>

          <LayerCard>
            <LayerCard.Secondary>插件仓库 · {repositories.length}</LayerCard.Secondary>
            <LayerCard.Primary>
              <div className="flex flex-col gap-3">
                {repositories.map((repository) => (
                  <div
                    key={repository.id}
                    className="flex flex-wrap items-center justify-between gap-3 border-b border-kumo-hairline pb-3 last:border-0 last:pb-0"
                  >
                    <div className="min-w-0 flex-1">
                      <div className="flex flex-wrap items-center gap-2">
                        <Text size="sm">{repository.name}</Text>
                        {repository.official ? <Badge variant="primary">官方</Badge> : null}
                        <Badge variant="secondary">{repository.plugin_count} 个插件</Badge>
                      </div>
                      <Text size="xs" variant="secondary">
                        {repository.url}
                      </Text>
                      <Text size="xs" variant="secondary">
                        {repository.synced_at
                          ? `上次同步：${formatDateTime(repository.synced_at, timezone)}`
                          : "尚未同步"}
                      </Text>
                      {repository.last_error ? (
                        <Text size="xs">上次同步失败：{repository.last_error}</Text>
                      ) : null}
                    </div>
                    <div className="flex items-center gap-2">
                      <Button
                        size="sm"
                        variant="secondary"
                        loading={repositoryBusy === `sync:${repository.id}`}
                        disabled={repositoryBusy !== "" && repositoryBusy !== `sync:${repository.id}`}
                        onClick={() => void syncRepository(repository)}
                      >
                        <ArrowsClockwiseIcon size={16} /> 同步
                      </Button>
                      {!repository.official ? (
                        <Button
                          size="sm"
                          variant="secondary"
                          title={`删除 ${repository.name}`}
                          aria-label={`删除 ${repository.name}`}
                          loading={repositoryBusy === `delete:${repository.id}`}
                          disabled={repositoryBusy !== "" && repositoryBusy !== `delete:${repository.id}`}
                          onClick={() => void removeRepository(repository)}
                        >
                          <TrashIcon size={16} />
                        </Button>
                      ) : null}
                    </div>
                  </div>
                ))}
              </div>
            </LayerCard.Primary>
          </LayerCard>
        </div>
      ) : null}

      {tab === "register" ? (
        <div className="grid items-start gap-4 lg:grid-cols-2">
          <LayerCard>
            <LayerCard.Secondary>代理</LayerCard.Secondary>
            <LayerCard.Primary>
              <div className="flex flex-col gap-3">
                <Input label="注册 HTTP 代理" value={String(cfg.register_proxy || "")} onChange={(e) => setField("register_proxy", e.target.value)} />
                <Input label="通用 HTTP 代理" value={String(cfg.http_proxy || "")} onChange={(e) => setField("http_proxy", e.target.value)} />
                <Input label="通用 HTTPS 代理" value={String(cfg.https_proxy || "")} onChange={(e) => setField("https_proxy", e.target.value)} />
                <Input label="NO_PROXY" value={String(cfg.no_proxy || "")} onChange={(e) => setField("no_proxy", e.target.value)} />
              </div>
            </LayerCard.Primary>
          </LayerCard>

          <LayerCard>
            <LayerCard.Secondary>清障 Clearance</LayerCard.Secondary>
            <LayerCard.Primary>
              <div className="flex flex-col gap-3">
                <Switch label="启用清障" checked={cfg.clearance_enabled !== false} onCheckedChange={(v) => setField("clearance_enabled", !!v)} />
                <Input label="清障代理" value={String(cfg.clearance_proxy || "")} onChange={(e) => setField("clearance_proxy", e.target.value)} placeholder="http://privoxy:8118" />
                <Input label="清障目标 URL（逗号分隔）" value={String(cfg.clearance_urls || "")} onChange={(e) => setField("clearance_urls", e.target.value)} />
                <Input label="FlareSolverr URL" value={String(cfg.flaresolverr_url || "")} onChange={(e) => setField("flaresolverr_url", e.target.value)} placeholder="http://127.0.0.1:8191" />
              </div>
            </LayerCard.Primary>
          </LayerCard>

          <LayerCard>
            <LayerCard.Secondary>Turnstile</LayerCard.Secondary>
            <LayerCard.Primary>
              <div className="flex flex-col gap-3">
                <Select label="Turnstile 方式" value={String(cfg.turnstile_provider || "browser")} onValueChange={(v) => setField("turnstile_provider", v || "browser")}>
                  <Select.Option value="browser">本机浏览器</Select.Option>
                  <Select.Option value="lite">外部 Solver</Select.Option>
                </Select>
                <Input label="Solver URL" value={String(cfg.lite_solver_url || "")} onChange={(e) => setField("lite_solver_url", e.target.value)} />
                <Input label="Turnstile Chrome 路径" value={String(cfg.turnstile_chrome_path || "")} onChange={(e) => setField("turnstile_chrome_path", e.target.value)} placeholder="留空自动检测" />
                <Input label="Turnstile Python" value={String(cfg.turnstile_python || "")} onChange={(e) => setField("turnstile_python", e.target.value)} placeholder="留空自动检测" />
                <Input label="Turnstile Mint 脚本" value={String(cfg.turnstile_script || "")} onChange={(e) => setField("turnstile_script", e.target.value)} placeholder="留空自动检测" />
                <Switch label="注入 clearance 到 Turnstile Mint" checked={!!cfg.turnstile_inject_clearance} onCheckedChange={(v) => setField("turnstile_inject_clearance", !!v)} />
              </div>
            </LayerCard.Primary>
          </LayerCard>

          <LayerCard>
            <LayerCard.Secondary>协议与并发</LayerCard.Secondary>
            <LayerCard.Primary>
              <div className="flex flex-col gap-3">
                <div className="grid gap-3 sm:grid-cols-2">
                  <Input label="HTTP 并发池" value={String(cfg.http_pool_size ?? 8)} onChange={(e) => setField("http_pool_size", Number(e.target.value) || 1)} />
                  <Input label="物理并发上限（0=自动）" value={String(cfg.physical_cap ?? 0)} onChange={(e) => setField("physical_cap", Number(e.target.value) || 0)} />
                  <Input label="OAuth 最小间隔（秒）" value={String(cfg.oauth_min_interval_sec ?? 10)} onChange={(e) => setField("oauth_min_interval_sec", Number(e.target.value) || 0)} />
                  <Input label="OAuth 重试间隔（秒）" value={String(cfg.oauth_retry_sec ?? 60)} onChange={(e) => setField("oauth_retry_sec", Number(e.target.value) || 1)} />
                </div>
                <Switch label="优先 HTTP 注册协议" checked={cfg.protocol_http !== false} onCheckedChange={(v) => setField("protocol_http", !!v)} />
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
        </div>
      ) : null}

      {tab === "email" ? (
        <div className="grid items-start gap-4 lg:grid-cols-2">
          <LayerCard>
            <LayerCard.Secondary>邮箱模式</LayerCard.Secondary>
            <LayerCard.Primary>
              <div className="flex flex-col gap-3">
                <Select label="邮箱模式" value={emailMode} onValueChange={(v) => setField("email_mode", v || "tempmail")}>
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
                <Text size="xs" variant="secondary">
                  仅所选模式的凭据会被注册流程使用，其余 Provider 卡片可提前填好备用。
                </Text>
              </div>
            </LayerCard.Primary>
          </LayerCard>

          <LayerCard>
            <LayerCard.Secondary>{providerTitle("免费临时邮箱", "tempmail")}</LayerCard.Secondary>
            <LayerCard.Primary>
              <div className="flex flex-col gap-3">
                <Input label="临时邮箱创建重试次数" value={String(cfg.tempmail_lol_retries ?? 30)} onChange={(e) => setField("tempmail_lol_retries", Number(e.target.value) || 1)} />
                <Input label="临时邮箱轮询间隔（毫秒）" value={String(cfg.tempmail_lol_min_interval_ms ?? 1500)} onChange={(e) => setField("tempmail_lol_min_interval_ms", Number(e.target.value) || 100)} />
              </div>
            </LayerCard.Primary>
          </LayerCard>

          <LayerCard>
            <LayerCard.Secondary>{providerTitle("自建邮箱 Webhook", "custom")}</LayerCard.Secondary>
            <LayerCard.Primary>
              <div className="flex flex-col gap-3">
                <Input label="自建邮箱域名" value={String(cfg.email_domain || "")} onChange={(e) => setField("email_domain", e.target.value)} />
                <Input label="自建邮箱 API" value={String(cfg.email_api || "")} onChange={(e) => setField("email_api", e.target.value)} />
              </div>
            </LayerCard.Primary>
          </LayerCard>

          <LayerCard>
            <LayerCard.Secondary>{providerTitle("DuckMail", "duckmail")}</LayerCard.Secondary>
            <LayerCard.Primary>
              <div className="flex flex-col gap-3">
                <Input label="DuckMail API Base" value={String(cfg.duckmail_base || "")} onChange={(e) => setField("duckmail_base", e.target.value)} />
                <Input label="DuckMail API Key" type="password" value={duckMailKey} onChange={(e) => setDuckMailKey(e.target.value)} placeholder={cfg.duckmail_key_set ? "已设置 · 留空不改" : "可选"} />
              </div>
            </LayerCard.Primary>
          </LayerCard>

          <LayerCard>
            <LayerCard.Secondary>{providerTitle("Cloudflare Worker", "cloudflare")}</LayerCard.Secondary>
            <LayerCard.Primary>
              <div className="flex flex-col gap-3">
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
              </div>
            </LayerCard.Primary>
          </LayerCard>

          <LayerCard>
            <LayerCard.Secondary>{providerTitle("CloudMail", "cloudmail")}</LayerCard.Secondary>
            <LayerCard.Primary>
              <div className="flex flex-col gap-3">
                <Input label="CloudMail URL" value={String(cfg.cloudmail_url || "")} onChange={(e) => setField("cloudmail_url", e.target.value)} />
                <Input label="CloudMail 管理员邮箱" value={String(cfg.cloudmail_admin_email || "")} onChange={(e) => setField("cloudmail_admin_email", e.target.value)} />
                <Input label="CloudMail 密码" type="password" value={cloudMailPassword} onChange={(e) => setCloudMailPassword(e.target.value)} placeholder={cfg.cloudmail_password_set ? "已设置 · 留空不改" : "密码"} />
              </div>
            </LayerCard.Primary>
          </LayerCard>

          <LayerCard>
            <LayerCard.Secondary>{providerTitle("MailNest", "mailnest")}</LayerCard.Secondary>
            <LayerCard.Primary>
              <div className="flex flex-col gap-3">
                <Input label="MailNest API Key" type="password" value={mailNestKey} onChange={(e) => setMailNestKey(e.target.value)} placeholder={cfg.mailnest_key_set ? "已设置 · 留空不改" : "API Key"} />
                <Input label="MailNest 项目编号" value={String(cfg.mailnest_project_code || "x-ai001")} onChange={(e) => setField("mailnest_project_code", e.target.value)} />
              </div>
            </LayerCard.Primary>
          </LayerCard>

          <LayerCard>
            <LayerCard.Secondary>{providerTitle("MoeMail", "moemail")}</LayerCard.Secondary>
            <LayerCard.Primary>
              <div className="flex flex-col gap-3">
                <Input label="MoeMail API Base" value={String(cfg.moemail_base || "")} onChange={(e) => setField("moemail_base", e.target.value)} />
                <Input label="MoeMail API Key" type="password" value={moeMailKey} onChange={(e) => setMoeMailKey(e.target.value)} placeholder={cfg.moemail_key_set ? "已设置 · 留空不改" : "API Key"} />
                <Input label="MoeMail 域名" value={String(cfg.moemail_domain || "")} onChange={(e) => setField("moemail_domain", e.target.value)} />
                <Input label="MoeMail 过期时间（毫秒）" value={String(cfg.moemail_expiry_ms || 3600000)} onChange={(e) => setField("moemail_expiry_ms", Number(e.target.value) || 3600000)} />
              </div>
            </LayerCard.Primary>
          </LayerCard>

          <LayerCard>
            <LayerCard.Secondary>{providerTitle("YYDS Mail", "yyds")}</LayerCard.Secondary>
            <LayerCard.Primary>
              <div className="flex flex-col gap-3">
                <Input label="YYDS API Key" type="password" value={yydsKey} onChange={(e) => setYYDSKey(e.target.value)} placeholder={cfg.yyds_key_set ? "已设置 · 留空不改" : "API Key"} />
                <Input label="YYDS JWT" type="password" value={yydsJWT} onChange={(e) => setYYDSJWT(e.target.value)} placeholder={cfg.yyds_jwt_set ? "已设置 · 留空不改" : "JWT"} />
                <Input label="YYDS 默认域名" value={String(cfg.yyds_domain || "")} onChange={(e) => setField("yyds_domain", e.target.value)} />
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
        </div>
      ) : null}

      {tab === "pool" ? (
        <div className="grid items-start gap-4 lg:grid-cols-2">
          <LayerCard>
            <LayerCard.Secondary>CPA Management</LayerCard.Secondary>
            <LayerCard.Primary>
              <div className="flex flex-col gap-3">
                <Input
                  label="Base URL"
                  value={String(cfg.cpa_management_base || "")}
                  onChange={(e) => setField("cpa_management_base", e.target.value)}
                  placeholder="http://127.0.0.1:8317"
                />
                <Input
                  label="Management Key"
                  type="text"
                  value={cpaKey}
                  onChange={(e) => setCpaKey(e.target.value)}
                  placeholder="Management Key"
                />
                <Switch
                  label="注册成功自动上传"
                  checked={!!cfg.cpa_upload_enabled}
                  onCheckedChange={(v) => setField("cpa_upload_enabled", !!v)}
                />
                <div className="flex items-center justify-between gap-3">
                  <div>
                    {cpaTestResult ? (
                      <Text size="xs">{cpaTestResult}</Text>
                    ) : (
                      <Text size="xs" variant="secondary">
                        完整 Key 仅在本机地址返显；根地址自动补 /v0/management。
                      </Text>
                    )}
                  </div>
                  <Button
                    size="sm"
                    variant="secondary"
                    loading={cpaTestBusy}
                    onClick={() => void testConn()}
                  >
                    测试 CPA
                  </Button>
                </div>
              </div>
            </LayerCard.Primary>
          </LayerCard>

          <LayerCard>
            <LayerCard.Secondary>CPA 上传细节</LayerCard.Secondary>
            <LayerCard.Primary>
              <div className="flex flex-col gap-3">
                <div className="grid gap-3 sm:grid-cols-2">
                  <Input label="上传超时（秒）" value={String(cfg.cpa_upload_timeout_sec ?? 30)} onChange={(e) => setField("cpa_upload_timeout_sec", Number(e.target.value) || 1)} />
                  <Input label="上传重试次数" value={String(cfg.cpa_upload_retries ?? 2)} onChange={(e) => setField("cpa_upload_retries", Number(e.target.value) || 0)} />
                </div>
                <Input label="文件名模板" value={String(cfg.cpa_upload_name_template || "{email}.json")} onChange={(e) => setField("cpa_upload_name_template", e.target.value)} />
                <Select label="上传格式" value={String(cfg.cpa_upload_mode || "multipart")} onValueChange={(v) => setField("cpa_upload_mode", v || "multipart")}>
                  <Select.Option value="multipart">Multipart</Select.Option>
                  <Select.Option value="json">JSON</Select.Option>
                </Select>
                <Switch label="上传后验证" checked={cfg.cpa_upload_verify !== false} onCheckedChange={(v) => setField("cpa_upload_verify", !!v)} />
              </div>
            </LayerCard.Primary>
          </LayerCard>

          <LayerCard>
            <LayerCard.Secondary>并发与批次</LayerCard.Secondary>
            <LayerCard.Primary>
              <div className="grid gap-3 sm:grid-cols-2">
                <Input label="上传并发" value={String(cfg.upload_concurrency ?? 3)} onChange={(e) => setField("upload_concurrency", Number(e.target.value) || 1)} />
                <Input label="上传批次大小" value={String(cfg.upload_batch_size ?? 20)} onChange={(e) => setField("upload_batch_size", Number(e.target.value) || 1)} />
                <Input label="导出并发" value={String(cfg.export_concurrency ?? 15)} onChange={(e) => setField("export_concurrency", Number(e.target.value) || 1)} />
                <Input label="导出批次大小" value={String(cfg.export_batch_size ?? 500)} onChange={(e) => setField("export_batch_size", Number(e.target.value) || 1)} />
              </div>
            </LayerCard.Primary>
          </LayerCard>

          <LayerCard>
            <LayerCard.Secondary>本地号池</LayerCard.Secondary>
            <LayerCard.Primary>
              <div className="flex flex-col gap-3">
                <Switch label="注册产物自动进入本地号池" checked={cfg.local_pool_auto_import !== false} onCheckedChange={(v) => setField("local_pool_auto_import", !!v)} />
                <Switch label="本地号池自动同步 CPA" checked={!!cfg.local_pool_auto_sync} onCheckedChange={(v) => setField("local_pool_auto_sync", !!v)} />
              </div>
            </LayerCard.Primary>
          </LayerCard>
        </div>
      ) : null}

      {tab === "patrol" ? (
        <div className="grid items-start gap-4 lg:grid-cols-2">
          <LayerCard>
            <LayerCard.Secondary>定时巡检</LayerCard.Secondary>
            <LayerCard.Primary>
              <div className="flex flex-col gap-3">
                <Switch
                  label="启用定时巡检"
                  checked={!!cfg.patrol_enabled}
                  onCheckedChange={(v) => setField("patrol_enabled", !!v)}
                />
                <div className="grid gap-3 sm:grid-cols-2">
                  <Input
                    label="巡检间隔（分钟）"
                    value={String(cfg.patrol_interval_min ?? 30)}
                    onChange={(e) =>
                      setField("patrol_interval_min", parseInt(e.target.value, 10) || 30)
                    }
                  />
                  <Input
                    label="巡检并发"
                    value={String(cfg.patrol_concurrency ?? 10)}
                    onChange={(e) => setField("patrol_concurrency", Number(e.target.value) || 1)}
                  />
                </div>
                <Switch label="深度巡检" checked={!!cfg.patrol_deep_probe} onCheckedChange={(v) => setField("patrol_deep_probe", !!v)} />
              </div>
            </LayerCard.Primary>
          </LayerCard>

          <LayerCard>
            <LayerCard.Secondary>自动补号</LayerCard.Secondary>
            <LayerCard.Primary>
              <div className="flex flex-col gap-3">
                <Switch
                  label="健康不足自动补号"
                  checked={!!cfg.refill_enabled}
                  onCheckedChange={(v) => setField("refill_enabled", !!v)}
                />
                <div className="grid gap-3 sm:grid-cols-2">
                  <Input
                    label="最低健康数"
                    value={String(cfg.refill_min_healthy ?? 5)}
                    onChange={(e) =>
                      setField("refill_min_healthy", parseInt(e.target.value, 10) || 5)
                    }
                  />
                  <Input
                    label="单次补号数量"
                    value={String(cfg.refill_batch ?? 10)}
                    onChange={(e) => setField("refill_batch", Number(e.target.value) || 1)}
                  />
                  <Input
                    label="补号冷却（分钟）"
                    value={String(cfg.refill_cooldown_min ?? 60)}
                    onChange={(e) => setField("refill_cooldown_min", Number(e.target.value) || 1)}
                  />
                  <Input
                    label="每日补号上限"
                    value={String(cfg.refill_daily_cap ?? 50)}
                    onChange={(e) => setField("refill_daily_cap", Number(e.target.value) || 1)}
                  />
                </div>
                <Input
                  label="单健康号额度估算"
                  value={String(cfg.quota_per_account ?? 60)}
                  onChange={(e) => setField("quota_per_account", Number(e.target.value) || 1)}
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
      ) : null}

      {tab === "import" ? (
        <div className="grid items-start gap-4 lg:grid-cols-2">
          <LayerCard>
            <LayerCard.Secondary>分享配置链接</LayerCard.Secondary>
            <LayerCard.Primary>
              <div className="flex flex-col gap-3">
                <Input
                  label="无密钥配置链接"
                  value={importLink}
                  onChange={(e) => setImportLink(e.target.value)}
                  placeholder="粘贴由另一台面板生成的链接"
                />
                <div className="flex flex-wrap gap-2">
                  <Button size="sm" variant="secondary" disabled={busy} onClick={() => void createImportLink()}>
                    生成链接
                  </Button>
                  <Button size="sm" loading={busy} disabled={!importLink.trim()} onClick={() => void importInfrastructure("link")}>
                    导入链接
                  </Button>
                </div>
                <Text size="xs" variant="secondary">
                  链接包含邮箱、TouchMail、代理与 Clearance 等普通设置；不包含 Token、API Key、密码或机器路径。导入仅覆盖链接明确给出的非空字段。
                </Text>
              </div>
            </LayerCard.Primary>
          </LayerCard>

          <LayerCard>
            <LayerCard.Secondary>从联邦主节点拉取完整设置</LayerCard.Secondary>
            <LayerCard.Primary>
              <div className="flex flex-col gap-3">
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
                  主节点必须启用“共享基础设施配置”并设置联邦密钥。邮件服务凭据仅通过已鉴权的联邦请求传输，绝不写进链接。
                </Text>
              </div>
            </LayerCard.Primary>
          </LayerCard>
        </div>
      ) : null}
    </AdminShell>
  );
}
