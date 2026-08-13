"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import {
  ArrowClockwiseIcon,
  BroadcastIcon,
  CheckCircleIcon,
  CrosshairIcon,
  EnvelopeSimpleIcon,
  FileArrowUpIcon,
  MagnifyingGlassIcon,
  PaperPlaneTiltIcon,
  ShieldCheckIcon,
  XCircleIcon,
} from "@phosphor-icons/react";
import {
  Badge,
  Button,
  Input,
  LayerCard,
  Select,
  Switch,
  Table,
  Tabs,
  Text,
  Textarea,
} from "@cloudflare/kumo";
import { AdminShell } from "@/components/admin-shell";
import { PageHeader } from "@/components/page-header";
import { api } from "@/lib/api";

type Evidence = { kind: string; fingerprint: string; redacted: string };
type Finding = {
  id: string;
  url: string;
  host: string;
  source: string;
  query?: string;
  product?: string;
  title?: string;
  status: "new" | "confirmed" | "dismissed";
  in_scope: boolean;
  http_status?: number;
  evidence?: Evidence[];
  observed_at: string;
  probed_at?: string;
};
type Draft = {
  id: string;
  finding_id: string;
  channel_id?: string;
  to: string;
  subject: string;
  body: string;
  status: "pending" | "approved" | "sending" | "sent";
  approved_by?: string;
  send_error?: string;
  created_at: string;
  sent_at?: string;
};
type Audit = { id: string; action: string; target_id?: string; detail?: string; created_at: string };
type HunterConfig = {
  scopes: string[];
  fofa_email: string;
  fofa_key: string;
  fofa_queries: string[];
  shodan_key: string;
  shodan_queries: string[];
  probe_enabled: boolean;
  isolated_network: boolean;
  auto_discover_network: boolean;
  credential_audit_enabled: boolean;
  discovery_cidrs: string[];
  discovery_ports: number[];
  discovery_concurrency: number;
  discovery_timeout_ms: number;
  max_discovery_hosts: number;
  max_results: number;
  rate_per_minute: number;
};
type SMTPChannel = { id: string; name: string; enabled: boolean };
type SnapshotResponse = {
  ok: boolean;
  snapshot: { config: HunterConfig; findings: Finding[]; drafts: Draft[]; audit: Audit[] };
  smtp_channels: SMTPChannel[];
  store: string;
  local_networks: string[];
};
type TabKey = "findings" | "discover" | "policy" | "drafts" | "audit";

const emptyConfig: HunterConfig = {
  scopes: [],
  fofa_email: "",
  fofa_key: "",
  fofa_queries: [],
  shodan_key: "",
  shodan_queries: [],
  probe_enabled: true,
  isolated_network: true,
  auto_discover_network: true,
  credential_audit_enabled: true,
  discovery_cidrs: [],
  discovery_ports: [80, 443, 3000, 3001, 4000, 5000, 7860, 8000, 8080, 8081, 8317, 8443, 11434],
  discovery_concurrency: 64,
  discovery_timeout_ms: 500,
  max_discovery_hosts: 4096,
  max_results: 50,
  rate_per_minute: 60,
};

const statusLabel: Record<string, string> = {
  new: "待复核",
  confirmed: "已确认",
  dismissed: "已忽略",
  pending: "待审批",
  approved: "已审批",
  sending: "发送中",
  sent: "已发送",
};

function lines(value: string): string[] {
  return value.split(/\n+/).map((item) => item.trim()).filter(Boolean);
}

function ports(value: string): number[] {
  return value.split(/[,;\s]+/).map((item) => Number(item)).filter((item) => Number.isInteger(item) && item > 0 && item <= 65535);
}

function disclosureBody(finding: Finding): string {
  const evidence = (finding.evidence || [])
    .map((item) => `- ${item.kind}: ${item.redacted} (fingerprint ${item.fingerprint})`)
    .join("\n");
  return [
    "您好，",
    "",
    "我们在公开可访问的服务响应中发现疑似密钥泄露。以下内容已经脱敏，未保存或验证完整密钥：",
    "",
    `资产：${finding.url}`,
    `产品识别：${finding.product || "unknown"}`,
    evidence || "- 未发现可记录的密钥模式；请复核公开访问状态。",
    "",
    "建议尽快撤销相关密钥、检查访问日志，并限制该接口的公开访问。",
  ].join("\n");
}

export default function HunterPage() {
  const [tab, setTab] = useState<TabKey>("findings");
  const [data, setData] = useState<SnapshotResponse | null>(null);
  const [cfg, setCfg] = useState<HunterConfig>(emptyConfig);
  const [scopeText, setScopeText] = useState("");
  const [fofaQueries, setFofaQueries] = useState("");
  const [shodanQueries, setShodanQueries] = useState("");
  const [discoveryCIDRs, setDiscoveryCIDRs] = useState("");
  const [discoveryPorts, setDiscoveryPorts] = useState("");
  const [importText, setImportText] = useState("");
  const [msg, setMsg] = useState("");
  const [busy, setBusy] = useState(false);
  const [composerFinding, setComposerFinding] = useState<Finding | null>(null);
  const [draftForm, setDraftForm] = useState({ channel_id: "", to: "", subject: "", body: "" });
  const [operator, setOperator] = useState("");

  const load = useCallback(async () => {
    setBusy(true);
    try {
      const next = await api<SnapshotResponse>("/api/hunter");
      setData(next);
      setCfg({ ...emptyConfig, ...next.snapshot.config });
      setScopeText((next.snapshot.config.scopes || []).join("\n"));
      setFofaQueries((next.snapshot.config.fofa_queries || []).join("\n"));
      setShodanQueries((next.snapshot.config.shodan_queries || []).join("\n"));
      setDiscoveryCIDRs((next.snapshot.config.discovery_cidrs || []).join("\n"));
      setDiscoveryPorts((next.snapshot.config.discovery_ports || []).join(","));
    } catch (error) {
      setMsg(error instanceof Error ? error.message : "加载失败");
    } finally {
      setBusy(false);
    }
  }, []);

  useEffect(() => { void load(); }, [load]);

  const findings = data?.snapshot.findings || [];
  const drafts = data?.snapshot.drafts || [];
  const audit = data?.snapshot.audit || [];
  const counts = useMemo(() => ({
    total: findings.length,
    inScope: findings.filter((f) => f.in_scope).length,
    confirmed: findings.filter((f) => f.status === "confirmed").length,
    evidence: findings.filter((f) => (f.evidence || []).length > 0).length,
  }), [findings]);

  async function run(action: () => Promise<unknown>, success: string) {
    setBusy(true);
    setMsg("");
    try {
      await action();
      setMsg(success);
      await load();
    } catch (error) {
      setMsg(error instanceof Error ? error.message : "操作失败");
      setBusy(false);
    }
  }

  async function savePolicy() {
    const body: HunterConfig = {
      ...cfg,
      scopes: lines(scopeText),
      fofa_queries: lines(fofaQueries),
      shodan_queries: lines(shodanQueries),
      discovery_cidrs: lines(discoveryCIDRs),
      discovery_ports: ports(discoveryPorts),
      discovery_concurrency: Number(cfg.discovery_concurrency) || 64,
      discovery_timeout_ms: Number(cfg.discovery_timeout_ms) || 500,
      max_discovery_hosts: Number(cfg.max_discovery_hosts) || 4096,
      max_results: Number(cfg.max_results) || 50,
      rate_per_minute: Number(cfg.rate_per_minute) || 60,
    };
    await run(() => api("/api/hunter/config", { method: "PUT", body: JSON.stringify(body) }), "策略已保存");
  }

  async function discover(sources: string[]) {
    setBusy(true);
    setMsg("");
    try {
      const response = await api<{ ok: boolean; report: { imported: number; errors?: string[] } }>("/api/hunter/discover", {
        method: "POST",
        body: JSON.stringify({ sources }),
      });
      const errors = response.report.errors || [];
      setMsg(errors.length ? `已导入 ${response.report.imported} 条；${errors.join("；")}` : `被动发现已完成，导入 ${response.report.imported} 条`);
      await load();
    } catch (error) {
      setMsg(error instanceof Error ? error.message : "发现失败");
      setBusy(false);
    }
  }

  async function discoverNetwork() {
    setBusy(true);
    setMsg("");
    try {
      const response = await api<{ ok: boolean; report: { networks: string[]; scanned_hosts: number; scanned_ports: number; open_ports: number; imported: number; errors?: string[] } }>("/api/hunter/discover-network", {
        method: "POST",
        body: "{}",
      });
      const report = response.report;
      const suffix = report.errors?.length ? `；${report.errors.join("；")}` : "";
      setMsg(`内网扫描完成：${report.scanned_hosts} 主机 · ${report.open_ports} 开放端口 · ${report.imported} 个 HTTP 服务${suffix}`);
      await load();
    } catch (error) {
      setMsg(error instanceof Error ? error.message : "内网扫描失败");
      setBusy(false);
    }
  }

  async function importURLs() {
    const items = lines(importText).map((url) => ({ url }));
    await run(() => api("/api/hunter/import", { method: "POST", body: JSON.stringify({ items }) }), `已导入 ${items.length} 条资产`);
    setImportText("");
  }

  async function setFindingStatus(id: string, status: Finding["status"]) {
    await run(() => api(`/api/hunter/findings/${id}/status`, { method: "PUT", body: JSON.stringify({ status }) }), "发现状态已更新");
  }

  async function probe(id: string) {
    await run(() => api(`/api/hunter/findings/${id}/probe`, { method: "POST", body: "{}" }), "只读探测已完成");
  }

  function openComposer(finding: Finding) {
    const channel = (data?.smtp_channels || []).find((item) => item.enabled);
    setComposerFinding(finding);
    setDraftForm({
      channel_id: channel?.id || "",
      to: "",
      subject: `安全提醒：${finding.host} 疑似存在密钥泄露`,
      body: disclosureBody(finding),
    });
    setTab("drafts");
  }

  async function createDraft() {
    if (!composerFinding) return;
    await run(() => api("/api/hunter/drafts", {
      method: "POST",
      body: JSON.stringify({ finding_id: composerFinding.id, ...draftForm }),
    }), "邮件草稿已创建");
    setComposerFinding(null);
  }

  async function approveDraft(id: string) {
    await run(() => api(`/api/hunter/drafts/${id}/approve`, {
      method: "POST",
      body: JSON.stringify({ operator }),
    }), "草稿已审批");
  }

  async function sendDraft(draft: Draft) {
    if (!confirm(`向 ${draft.to} 发送已审批邮件？`)) return;
    await run(() => api(`/api/hunter/drafts/${draft.id}/send`, { method: "POST", body: "{}" }), "邮件已发送");
  }

  const tabs = [
    { value: "findings", label: `发现 (${findings.length})` },
    { value: "discover", label: "数据源" },
    { value: "policy", label: "授权策略" },
    { value: "drafts", label: `邮件审批 (${drafts.filter((d) => d.status !== "sent").length})` },
    { value: "audit", label: "审计" },
  ];

  return (
    <AdminShell>
      <PageHeader
        title="泄露巡检"
        description="隔离网络服务发现与泄露审计"
        actions={
          <Button size="sm" variant="secondary" loading={busy} onClick={() => void load()}>
            <ArrowClockwiseIcon size={16} /> 刷新
          </Button>
        }
      />

      <div className="mb-4 flex flex-wrap items-center gap-2">
        <Badge variant={cfg.probe_enabled ? "primary" : "secondary"}>
          {cfg.probe_enabled ? "主动探测已启用" : "仅被动发现"}
        </Badge>
        <Badge variant={cfg.isolated_network ? "primary" : "secondary"}>
          {cfg.isolated_network ? "隔离网络模式" : "公网模式"}
        </Badge>
        <Badge variant="secondary">范围 {cfg.scopes?.length || 0}</Badge>
        <Text size="xs" variant="secondary">{data?.store || ""}</Text>
      </div>
      {msg ? <div className="mb-4 rounded-md bg-kumo-contrast/5 px-3 py-2"><Text size="sm">{msg}</Text></div> : null}
      <div className="mb-4 overflow-x-auto">
        <Tabs variant="segmented" tabs={tabs} value={tab} onValueChange={(value) => value && setTab(value as TabKey)} />
      </div>

      {tab === "findings" ? (
        <div className="flex flex-col gap-4">
          <LayerCard>
            <LayerCard.Secondary>队列状态</LayerCard.Secondary>
            <LayerCard.Primary>
              <div className="flex flex-wrap gap-x-8 gap-y-3">
                {[["全部", counts.total], ["授权范围内", counts.inScope], ["已确认", counts.confirmed], ["含脱敏证据", counts.evidence]].map(([label, value]) => (
                  <div key={String(label)} className="min-w-24"><Text size="xs" variant="secondary">{label}</Text><Text variant="heading3" as="span">{value}</Text></div>
                ))}
              </div>
            </LayerCard.Primary>
          </LayerCard>
          <LayerCard>
            <LayerCard.Secondary>发现队列</LayerCard.Secondary>
            <LayerCard.Primary className="p-0">
              {findings.length === 0 ? <div className="p-4"><Text variant="secondary">暂无发现</Text></div> : (
                <div className="overflow-x-auto"><Table>
                  <Table.Header><Table.Row><Table.Head>目标</Table.Head><Table.Head>识别</Table.Head><Table.Head>范围</Table.Head><Table.Head>证据</Table.Head><Table.Head>状态</Table.Head><Table.Head>操作</Table.Head></Table.Row></Table.Header>
                  <Table.Body>{findings.map((finding) => (
                    <Table.Row key={finding.id}>
                      <Table.Cell><div className="max-w-72"><Text size="sm">{finding.host}</Text><Text size="xs" variant="secondary">{finding.url}</Text></div></Table.Cell>
                      <Table.Cell><Text size="sm">{finding.product || "unknown"}</Text><Text size="xs" variant="secondary">{finding.source}{finding.http_status ? ` · HTTP ${finding.http_status}` : ""}</Text></Table.Cell>
                      <Table.Cell><Badge variant={finding.in_scope ? "primary" : "secondary"}>{finding.in_scope ? "已授权" : "仅被动"}</Badge></Table.Cell>
                      <Table.Cell><Text size="sm">{finding.evidence?.length || 0}</Text>{finding.evidence?.[0] ? <Text size="xs" variant="secondary">{finding.evidence[0].redacted}</Text> : null}</Table.Cell>
                      <Table.Cell><Badge variant={finding.status === "confirmed" ? "primary" : "secondary"}>{statusLabel[finding.status]}</Badge></Table.Cell>
                      <Table.Cell><div className="flex min-w-max gap-1">
                        {finding.status === "new" ? <Button size="xs" variant="secondary" disabled={busy} onClick={() => void setFindingStatus(finding.id, "confirmed")} title="确认发现"><CheckCircleIcon size={14} />确认</Button> : null}
                        {finding.status === "new" ? <Button size="xs" variant="ghost" disabled={busy} onClick={() => void setFindingStatus(finding.id, "dismissed")} title="忽略发现"><XCircleIcon size={14} /></Button> : null}
                        {finding.in_scope && cfg.probe_enabled ? <Button size="xs" variant="secondary" disabled={busy} onClick={() => void probe(finding.id)} title="执行固定只读探测"><CrosshairIcon size={14} />探测</Button> : null}
                        {finding.status === "confirmed" ? <Button size="xs" disabled={busy} onClick={() => openComposer(finding)}><EnvelopeSimpleIcon size={14} />草稿</Button> : null}
                      </div></Table.Cell>
                    </Table.Row>
                  ))}</Table.Body>
                </Table></div>
              )}
            </LayerCard.Primary>
          </LayerCard>
        </div>
      ) : null}

      {tab === "discover" ? (
        <div className="grid gap-4 lg:grid-cols-2">
          <LayerCard className="lg:col-span-2">
            <LayerCard.Secondary>隔离网络</LayerCard.Secondary>
            <LayerCard.Primary><div className="flex flex-wrap items-center justify-between gap-3">
              <div><Text size="sm">{(data?.local_networks || []).join(" · ") || "127.0.0.1/32"}</Text><Text size="xs" variant="secondary">端口 {cfg.discovery_ports?.join(", ")}</Text></div>
              <Button size="sm" loading={busy} disabled={!cfg.isolated_network} onClick={() => void discoverNetwork()}><BroadcastIcon size={16} />扫描内网服务</Button>
            </div></LayerCard.Primary>
          </LayerCard>
          <LayerCard>
            <LayerCard.Secondary>公开情报</LayerCard.Secondary>
            <LayerCard.Primary><div className="flex flex-wrap gap-2">
              <Button size="sm" loading={busy} onClick={() => void discover(["fofa", "shodan"])}><MagnifyingGlassIcon size={16} />FOFA + Shodan</Button>
              <Button size="sm" variant="secondary" disabled={busy} onClick={() => void discover(["fofa"])}>FOFA</Button>
              <Button size="sm" variant="secondary" disabled={busy} onClick={() => void discover(["shodan"])}>Shodan</Button>
            </div></LayerCard.Primary>
          </LayerCard>
          <LayerCard>
            <LayerCard.Secondary>本地资产</LayerCard.Secondary>
            <LayerCard.Primary><div className="flex flex-col gap-3">
              <Textarea label="资产 URL" description="每行一个 http(s) URL" rows={7} value={importText} onChange={(e) => setImportText(e.target.value)} />
              <div><Button size="sm" variant="secondary" disabled={busy || lines(importText).length === 0} onClick={() => void importURLs()}><FileArrowUpIcon size={16} />导入</Button></div>
            </div></LayerCard.Primary>
          </LayerCard>
        </div>
      ) : null}

      {tab === "policy" ? (
        <div className="flex flex-col gap-4">
          <LayerCard>
            <LayerCard.Secondary>隔离网络发现</LayerCard.Secondary>
            <LayerCard.Primary><div className="flex max-w-2xl flex-col gap-3">
              <Switch label="允许私网、环回和链路本地服务" checked={cfg.isolated_network} onCheckedChange={(checked) => setCfg((prev) => ({ ...prev, isolated_network: checked }))} />
              <Switch label="自动读取本机网卡网段" checked={cfg.auto_discover_network} onCheckedChange={(checked) => setCfg((prev) => ({ ...prev, auto_discover_network: checked }))} />
              <Switch label="探测 Sub2API 官方示例默认口令" checked={cfg.credential_audit_enabled} onCheckedChange={(checked) => setCfg((prev) => ({ ...prev, credential_audit_enabled: checked }))} />
              <Textarea label="附加扫描 CIDR" description="每行一个 IP/CIDR；显式配置可包含任何当前网络可达地址" rows={4} value={discoveryCIDRs} onChange={(e) => setDiscoveryCIDRs(e.target.value)} />
              <Input label="扫描端口" value={discoveryPorts} onChange={(e) => setDiscoveryPorts(e.target.value)} />
              <div className="grid gap-3 sm:grid-cols-3">
                <Input label="并发" type="number" min={1} max={256} value={String(cfg.discovery_concurrency)} onChange={(e) => setCfg((prev) => ({ ...prev, discovery_concurrency: Number(e.target.value) }))} />
                <Input label="连接超时（ms）" type="number" min={100} max={10000} value={String(cfg.discovery_timeout_ms)} onChange={(e) => setCfg((prev) => ({ ...prev, discovery_timeout_ms: Number(e.target.value) }))} />
                <Input label="最多主机" type="number" min={1} max={65536} value={String(cfg.max_discovery_hosts)} onChange={(e) => setCfg((prev) => ({ ...prev, max_discovery_hosts: Number(e.target.value) }))} />
              </div>
            </div></LayerCard.Primary>
          </LayerCard>
          <LayerCard>
            <LayerCard.Secondary>授权范围</LayerCard.Secondary>
            <LayerCard.Primary><div className="flex max-w-2xl flex-col gap-3">
              <Textarea label="主动探测范围" description="域名、*.example.com、IP 或 CIDR；隔离网络自动发现的网段会自动加入" rows={7} value={scopeText} onChange={(e) => setScopeText(e.target.value)} />
              <Switch label="启用产品接口与泄露检查" checked={cfg.probe_enabled} onCheckedChange={(checked) => setCfg((prev) => ({ ...prev, probe_enabled: checked }))} />
              <div className="grid gap-3 sm:grid-cols-2">
                <Input label="每分钟最多探测" type="number" min={1} max={600} value={String(cfg.rate_per_minute)} onChange={(e) => setCfg((prev) => ({ ...prev, rate_per_minute: Number(e.target.value) }))} />
                <Input label="每条查询最多结果" type="number" min={1} max={1000} value={String(cfg.max_results)} onChange={(e) => setCfg((prev) => ({ ...prev, max_results: Number(e.target.value) }))} />
              </div>
            </div></LayerCard.Primary>
          </LayerCard>
          <div className="grid gap-4 lg:grid-cols-2">
            <LayerCard><LayerCard.Secondary>FOFA</LayerCard.Secondary><LayerCard.Primary><div className="flex flex-col gap-3">
              <Input label="账号邮箱（或 FOFA_EMAIL）" value={cfg.fofa_email} onChange={(e) => setCfg((prev) => ({ ...prev, fofa_email: e.target.value }))} />
              <Input label="API Key（或 FOFA_API_KEY）" type="password" value={cfg.fofa_key} onChange={(e) => setCfg((prev) => ({ ...prev, fofa_key: e.target.value }))} />
              <Textarea label="查询语句" rows={6} value={fofaQueries} onChange={(e) => setFofaQueries(e.target.value)} />
            </div></LayerCard.Primary></LayerCard>
            <LayerCard><LayerCard.Secondary>Shodan</LayerCard.Secondary><LayerCard.Primary><div className="flex flex-col gap-3">
              <Input label="API Key（或 SHODAN_API_KEY）" type="password" value={cfg.shodan_key} onChange={(e) => setCfg((prev) => ({ ...prev, shodan_key: e.target.value }))} />
              <Textarea label="查询语句" rows={9} value={shodanQueries} onChange={(e) => setShodanQueries(e.target.value)} />
            </div></LayerCard.Primary></LayerCard>
          </div>
          <div><Button loading={busy} onClick={() => void savePolicy()}><ShieldCheckIcon size={16} />保存策略</Button></div>
        </div>
      ) : null}

      {tab === "drafts" ? (
        <div className="flex flex-col gap-4">
          {composerFinding ? <LayerCard><LayerCard.Secondary>新建披露邮件</LayerCard.Secondary><LayerCard.Primary><div className="flex max-w-2xl flex-col gap-3">
            <Select label="SMTP 渠道" value={draftForm.channel_id} onValueChange={(value) => value && setDraftForm((prev) => ({ ...prev, channel_id: value }))}>
              {(data?.smtp_channels || []).map((channel) => <Select.Option key={channel.id} value={channel.id}>{channel.name}{channel.enabled ? "" : "（已停用）"}</Select.Option>)}
            </Select>
            <Input label="收件人" type="email" value={draftForm.to} onChange={(e) => setDraftForm((prev) => ({ ...prev, to: e.target.value }))} />
            <Input label="主题" value={draftForm.subject} onChange={(e) => setDraftForm((prev) => ({ ...prev, subject: e.target.value }))} />
            <Textarea label="正文" rows={14} value={draftForm.body} onChange={(e) => setDraftForm((prev) => ({ ...prev, body: e.target.value }))} />
            <div className="flex gap-2"><Button size="sm" disabled={busy || !draftForm.channel_id || !draftForm.to || !draftForm.subject || !draftForm.body} onClick={() => void createDraft()}><EnvelopeSimpleIcon size={16} />保存草稿</Button><Button size="sm" variant="ghost" onClick={() => setComposerFinding(null)}>取消</Button></div>
          </div></LayerCard.Primary></LayerCard> : null}
          <LayerCard><LayerCard.Secondary>审批与发送</LayerCard.Secondary><LayerCard.Primary>
            <div className="mb-4 max-w-sm"><Input label="审批人" value={operator} onChange={(e) => setOperator(e.target.value)} /></div>
            {drafts.length === 0 ? <Text variant="secondary">暂无邮件草稿</Text> : <div className="overflow-x-auto"><Table>
              <Table.Header><Table.Row><Table.Head>收件人</Table.Head><Table.Head>主题</Table.Head><Table.Head>状态</Table.Head><Table.Head>审批</Table.Head><Table.Head>操作</Table.Head></Table.Row></Table.Header>
              <Table.Body>{drafts.map((draft) => <Table.Row key={draft.id}>
                <Table.Cell><Text size="sm">{draft.to}</Text><Text size="xs" variant="secondary">{draft.channel_id || "未选渠道"}</Text></Table.Cell>
                <Table.Cell><div className="max-w-80"><Text size="sm">{draft.subject}</Text>{draft.send_error ? <Text size="xs" variant="error">{draft.send_error}</Text> : null}</div></Table.Cell>
                <Table.Cell><Badge variant={draft.status === "approved" || draft.status === "sent" ? "primary" : "secondary"}>{statusLabel[draft.status]}</Badge></Table.Cell>
                <Table.Cell><Text size="xs" variant="secondary">{draft.approved_by || "—"}</Text></Table.Cell>
                <Table.Cell>{draft.status === "pending" ? <Button size="xs" variant="secondary" disabled={busy || !operator.trim()} onClick={() => void approveDraft(draft.id)}><CheckCircleIcon size={14} />审批</Button> : null}{draft.status === "approved" ? <Button size="xs" disabled={busy} onClick={() => void sendDraft(draft)}><PaperPlaneTiltIcon size={14} />发送</Button> : null}</Table.Cell>
              </Table.Row>)}</Table.Body>
            </Table></div>}
          </LayerCard.Primary></LayerCard>
        </div>
      ) : null}

      {tab === "audit" ? (
        <LayerCard><LayerCard.Secondary>审计记录</LayerCard.Secondary><LayerCard.Primary className="p-0">
          {audit.length === 0 ? <div className="p-4"><Text variant="secondary">暂无审计记录</Text></div> : <div className="overflow-x-auto"><Table>
            <Table.Header><Table.Row><Table.Head>时间</Table.Head><Table.Head>动作</Table.Head><Table.Head>对象</Table.Head><Table.Head>详情</Table.Head></Table.Row></Table.Header>
            <Table.Body>{[...audit].reverse().map((item) => <Table.Row key={item.id}><Table.Cell><Text size="xs">{item.created_at}</Text></Table.Cell><Table.Cell><Text size="sm">{item.action}</Text></Table.Cell><Table.Cell><Text size="xs" variant="secondary">{item.target_id || "—"}</Text></Table.Cell><Table.Cell><Text size="xs" variant="secondary">{item.detail || "—"}</Text></Table.Cell></Table.Row>)}</Table.Body>
          </Table></div>}
        </LayerCard.Primary></LayerCard>
      ) : null}
    </AdminShell>
  );
}
