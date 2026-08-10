import { useCallback, useEffect, useMemo, useState } from "react";
import {
  Box,
  CheckCircle2,
  Copy,
  ExternalLink,
  FolderInput,
  Globe2,
  KeyRound,
  Loader2,
  Play,
  RefreshCw,
  Save,
  ShieldCheck,
  Trash2,
  UserRoundPlus,
  WalletCards,
} from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Select } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { useToast } from "@/components/ui/toast";
import {
  api,
  type AdobeBrowserWorkspace,
  type AdobeProfile,
  type AdobeServiceStatus,
  type AdobeToken,
} from "@/lib/api";

type AdobeSettings = {
  proxy: string;
  use_proxy: boolean;
  refresh_interval_hours: number;
  retry_enabled: boolean;
  retry_max_attempts: number;
  token_rotation_strategy: string;
  batch_concurrency: number;
};

const DEFAULT_SETTINGS: AdobeSettings = {
  proxy: "",
  use_proxy: false,
  refresh_interval_hours: 15,
  retry_enabled: true,
  retry_max_attempts: 3,
  token_rotation_strategy: "round_robin",
  batch_concurrency: 5,
};

export function AdobePage() {
  const { showError, showSuccess } = useToast();
  const [status, setStatus] = useState<AdobeServiceStatus | null>(null);
  const [profiles, setProfiles] = useState<AdobeProfile[]>([]);
  const [tokens, setTokens] = useState<AdobeToken[]>([]);
  const [workspaces, setWorkspaces] = useState<AdobeBrowserWorkspace[]>([]);
  const [settings, setSettings] = useState<AdobeSettings>(DEFAULT_SETTINGS);
  const [cookieName, setCookieName] = useState("");
  const [cookieValue, setCookieValue] = useState("");
  const [workspaceName, setWorkspaceName] = useState("");
  const [busy, setBusy] = useState("");
  const [modelCount, setModelCount] = useState(0);

  const reload = useCallback(async () => {
    try {
      const serviceStatus = await api.adobeServiceStatus();
      setStatus(serviceStatus);
      const workspaceRows = await api.listAdobeBrowserWorkspaces();
      setWorkspaces(workspaceRows || []);
      if (!serviceStatus.running) return;
      const [profilePayload, tokenPayload, configPayload] = await Promise.all([
        api.listAdobeProfiles(),
        api.listAdobeTokens(),
        api.getAdobeConfig(),
      ]);
      setProfiles(profilePayload.profiles || []);
      setTokens(tokenPayload.tokens || tokenPayload.data || []);
      setSettings({
        ...DEFAULT_SETTINGS,
        ...(configPayload as Partial<AdobeSettings>),
      });
    } catch (error) {
      showError(`加载 Adobe Firefly 失败：${errorMessage(error)}`);
    }
  }, [showError]);

  useEffect(() => {
    void reload();
  }, [reload]);

  useEffect(() => {
    const timer = setInterval(() => {
      if (!document.hidden) void reload();
    }, 30_000);
    return () => clearInterval(timer);
  }, [reload]);

  const run = async (key: string, action: () => Promise<unknown>, success: string) => {
    setBusy(key);
    try {
      await action();
      showSuccess(success);
      await reload();
    } catch (error) {
      showError(errorMessage(error));
    } finally {
      setBusy("");
    }
  };

  const tokenByProfile = useMemo(() => {
    const map = new Map<string, AdobeToken>();
    tokens.forEach((token) => {
      if (token.refresh_profile_id) map.set(token.refresh_profile_id, token);
    });
    return map;
  }, [tokens]);

  const startService = () => run("service", api.startAdobeService, "Adobe2API 已启动");
  const restartService = () => run("service", api.restartAdobeService, "Adobe2API 已重新启动");

  const copyAdminPassword = async () => {
    try {
      const password = await api.adobeAdminPassword();
      await navigator.clipboard.writeText(password);
      showSuccess("高级后台密码已复制");
    } catch (error) {
      showError(errorMessage(error));
    }
  };

  const resetAdminPassword = async () => {
    if (!confirm("确定重置 Adobe 高级后台密码吗？旧密码会立即失效。")) return;
    setBusy("admin-password");
    try {
      const password = await api.resetAdobeAdminPassword();
      await navigator.clipboard.writeText(password);
      showSuccess("高级后台密码已重置并复制到剪贴板");
    } catch (error) {
      showError(errorMessage(error));
    } finally {
      setBusy("");
    }
  };

  const importCookie = async () => {
    if (!cookieValue.trim()) {
      showError("请先粘贴 Adobe Cookie 或 Cookie Exporter JSON");
      return;
    }
    await run(
      "cookie",
      () => api.importAdobeCookie(cookieName.trim(), cookieValue.trim()),
      "Adobe 账号已导入并尝试刷新令牌"
    );
    setCookieName("");
    setCookieValue("");
  };

  const launchWorkspace = async (name: string) => {
    await run(
      `launch-${name}`,
      () => api.launchAdobeBrowserWorkspace(name),
      "已打开隔离 Google Chrome；登录 Adobe 后返回这里点击“读取并导入”"
    );
    setWorkspaceName("");
  };

  const saveSettings = () =>
    run("settings", () => api.updateAdobeConfig(settings), "Adobe2API 配置已保存");

  const syncModels = async () => {
    setBusy("models");
    try {
      const result = await api.syncAdobeModels();
      const count = result.data?.length || 0;
      setModelCount(count);
      showSuccess(`Adobe 模型同步完成：${count} 个模型`);
    } catch (error) {
      showError(errorMessage(error));
    } finally {
      setBusy("");
    }
  };

  return (
    <div className="h-full overflow-y-auto">
      <div className="space-y-6 p-6">
        <Card>
          <div className="flex flex-wrap items-start justify-between gap-3 p-5">
            <div>
              <div className="flex items-center gap-2">
                <Box className="h-5 w-5 text-primary" />
                <CardTitle className="text-base">Adobe Firefly 本地反代</CardTitle>
                <Badge tone={status?.running ? "success" : "danger"}>
                  {status?.running ? "运行中" : "未运行"}
                </Badge>
              </div>
              <p className="mt-2 text-xs text-muted-foreground">
                adobe2api Sidecar · {status?.baseURL || "http://127.0.0.1:6001"} · 账号池 {status?.poolSize || 0}
              </p>
              {status?.error && <p className="mt-1 text-xs text-red-300">{status.error}</p>}
            </div>
            <div className="flex flex-wrap gap-2">
              <Button variant="outline" size="sm" onClick={status?.running ? restartService : startService} disabled={busy === "service"}>
                {busy === "service" ? <Loader2 className="h-4 w-4 animate-spin" /> : status?.running ? <RefreshCw className="h-4 w-4" /> : <Play className="h-4 w-4" />}
                {status?.running ? "重启服务" : "启动服务"}
              </Button>
              <Button variant="outline" size="sm" onClick={copyAdminPassword} disabled={!status?.running}>
                <Copy className="h-4 w-4" />复制后台密码
              </Button>
              <Button variant="outline" size="sm" onClick={() => void resetAdminPassword()} disabled={!status?.running || busy === "admin-password"}>
                {busy === "admin-password" ? <Loader2 className="h-4 w-4 animate-spin" /> : <KeyRound className="h-4 w-4" />}重置后台密码
              </Button>
              <Button variant="outline" size="sm" onClick={() => void api.openURL(status?.adminURL || "http://127.0.0.1:6001/")} disabled={!status?.running}>
                <ExternalLink className="h-4 w-4" />高级后台
              </Button>
            </div>
          </div>
          <CardContent className="grid gap-2 border-t border-border pt-4 text-[11px] text-muted-foreground md:grid-cols-2">
            <div className="truncate">状态目录：{status?.stateDir || "—"}</div>
            <div className="truncate">Python：{status?.pythonPath || "—"}</div>
            <div className="truncate">源码目录：{status?.sourceDir || "—"}</div>
            <div className="truncate">Cookie 扩展：{status?.cookiePlugin || "—"}</div>
          </CardContent>
        </Card>

        <Card>
          <div className="flex items-center justify-between p-5 pb-3">
            <div>
              <CardTitle className="flex items-center gap-2 text-base"><Globe2 className="h-4 w-4 text-primary" />隔离账号库</CardTitle>
              <p className="mt-1 text-xs text-muted-foreground">每个账号使用独立 Google Chrome 用户目录，可长期保留会话，无需安装多个浏览器。</p>
            </div>
          </div>
          <CardContent className="space-y-3 pt-0">
            <div className="flex gap-2">
              <Input value={workspaceName} onChange={(event) => setWorkspaceName(event.target.value)} placeholder="账号备注，例如 Adobe-01" />
              <Button onClick={() => void launchWorkspace(workspaceName)} disabled={busy.startsWith("launch-")}>
                <UserRoundPlus className="h-4 w-4" />新建并登录
              </Button>
            </div>
            {workspaces.length === 0 ? (
              <Empty text="尚未创建隔离账号工作区" />
            ) : (
              <div className="grid gap-2 lg:grid-cols-2">
                {workspaces.map((workspace) => (
                  <div key={workspace.id} className="rounded-lg border border-border bg-background/40 p-3">
                    <div className="flex items-start justify-between gap-2">
                      <div className="min-w-0">
                        <div className="font-medium">{workspace.name}</div>
                        <div className="mt-1 truncate font-mono text-[10px] text-muted-foreground">{workspace.profileDir}</div>
                      </div>
                      {workspace.debugPort > 0 && <Badge tone="info">Chrome {workspace.debugPort}</Badge>}
                    </div>
                    <div className="mt-3 flex gap-2">
                      <Button variant="outline" size="sm" onClick={() => void launchWorkspace(workspace.name)} disabled={busy === `launch-${workspace.name}`}>
                        <ExternalLink className="h-3.5 w-3.5" />重新打开
                      </Button>
                      <Button size="sm" onClick={() => void run(`capture-${workspace.id}`, () => api.importAdobeBrowserWorkspace(workspace.id), `${workspace.name} Cookie 已读取并导入`)} disabled={busy === `capture-${workspace.id}`}>
                        {busy === `capture-${workspace.id}` ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <ShieldCheck className="h-3.5 w-3.5" />}
                        读取并导入
                      </Button>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </CardContent>
        </Card>

        <div className="grid gap-6 xl:grid-cols-2">
          <Card>
            <div className="p-5 pb-3">
              <CardTitle className="text-base">Cookie / JSON 导入</CardTitle>
              <p className="mt-1 text-xs text-muted-foreground">支持 Cookie 字符串、扩展 JSON，以及一次选择多个 JSON 文件。</p>
            </div>
            <CardContent className="space-y-3 pt-0">
              <Input value={cookieName} onChange={(event) => setCookieName(event.target.value)} placeholder="账号备注（可选）" />
              <Textarea value={cookieValue} onChange={(event) => setCookieValue(event.target.value)} className="min-h-[130px] font-mono text-xs" placeholder="粘贴 Adobe Cookie 或 cookie_*.json 内容" spellCheck={false} />
              <div className="flex justify-end gap-2">
                <Button variant="outline" onClick={() => void run("files", api.importAdobeCookieFiles, "Cookie JSON 批量导入完成")} disabled={busy === "files" || !status?.running}>
                  <FolderInput className="h-4 w-4" />选择多个 JSON
                </Button>
                <Button onClick={() => void importCookie()} disabled={busy === "cookie" || !cookieValue.trim() || !status?.running}>
                  {busy === "cookie" ? <Loader2 className="h-4 w-4 animate-spin" /> : <CheckCircle2 className="h-4 w-4" />}导入并换令牌
                </Button>
              </div>
            </CardContent>
          </Card>

          <Card>
            <div className="flex items-center justify-between p-5 pb-3">
              <div>
                <CardTitle className="text-base">运行配置</CardTitle>
                <p className="mt-1 text-xs text-muted-foreground">保存后 Sidecar 会立即应用代理、刷新和轮询策略。</p>
              </div>
              <Button variant="outline" size="sm" onClick={saveSettings} disabled={busy === "settings" || !status?.running}>
                <Save className="h-4 w-4" />保存
              </Button>
            </div>
            <CardContent className="grid gap-3 pt-0 sm:grid-cols-2">
              <Field label="令牌刷新间隔（小时）"><Input type="number" min={1} max={24} value={settings.refresh_interval_hours} onChange={(event) => setSettings((value) => ({ ...value, refresh_interval_hours: Number(event.target.value) }))} /></Field>
              <Field label="请求重试次数"><Input type="number" min={1} max={10} value={settings.retry_max_attempts} onChange={(event) => setSettings((value) => ({ ...value, retry_max_attempts: Number(event.target.value) }))} /></Field>
              <Field label="账号轮询策略"><Select value={settings.token_rotation_strategy} onChange={(event) => setSettings((value) => ({ ...value, token_rotation_strategy: event.target.value }))}><option value="round_robin">顺序轮询</option><option value="random">随机</option></Select></Field>
              <Field label="批量并发数"><Input type="number" min={1} max={100} value={settings.batch_concurrency} onChange={(event) => setSettings((value) => ({ ...value, batch_concurrency: Number(event.target.value) }))} /></Field>
              <Field label="代理地址" className="sm:col-span-2"><Input value={settings.proxy} onChange={(event) => setSettings((value) => ({ ...value, proxy: event.target.value }))} placeholder="http://127.0.0.1:7890" /></Field>
              <ToggleField label="启用代理" checked={settings.use_proxy} onChange={(checked) => setSettings((value) => ({ ...value, use_proxy: checked }))} />
              <ToggleField label="启用失败重试" checked={settings.retry_enabled} onChange={(checked) => setSettings((value) => ({ ...value, retry_enabled: checked }))} />
            </CardContent>
          </Card>
        </div>

        <Card>
          <div className="flex flex-wrap items-center justify-between gap-3 p-5 pb-3">
            <div>
              <CardTitle className="flex items-center gap-2 text-base"><WalletCards className="h-4 w-4 text-primary" />Adobe 账号池</CardTitle>
              <p className="mt-1 text-xs text-muted-foreground">稳定 Adobe 账号 ID 相同时自动保留最新 Cookie，不会重复占用轮询槽位。</p>
            </div>
            <div className="flex gap-2">
              <Button variant="outline" size="sm" onClick={() => void syncModels()} disabled={busy === "models" || !status?.running}>
                <RefreshCw className={`h-4 w-4 ${busy === "models" ? "animate-spin" : ""}`} />同步模型{modelCount > 0 ? ` ${modelCount}` : ""}
              </Button>
              <Button variant="outline" size="sm" onClick={() => void run("refresh-all", api.refreshAllAdobeProfiles, "Adobe 账号刷新完成")} disabled={busy === "refresh-all" || !status?.running}>
                <RefreshCw className={`h-4 w-4 ${busy === "refresh-all" ? "animate-spin" : ""}`} />刷新全部
              </Button>
            </div>
          </div>
          <CardContent className="space-y-2 pt-0">
            {profiles.length === 0 ? <Empty text="尚未导入 Adobe 账号" /> : profiles.map((profile) => {
              const token = tokenByProfile.get(profile.id);
              return (
                <div key={profile.id} className="rounded-lg border border-border bg-background/40 p-4">
                  <div className="flex flex-wrap items-start justify-between gap-3">
                    <div className="min-w-0 flex-1">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="font-medium">{profile.account?.display_name || profile.name || profile.account?.email || profile.id}</span>
                        <Badge tone={profile.enabled ? "success" : "neutral"}>{profile.enabled ? "启用" : "停用"}</Badge>
                        {token?.status && <Badge tone={token.status === "active" ? "info" : "danger"}>{token.status}</Badge>}
                        <CreditsBadge token={token} />
                      </div>
                      <div className="mt-1 text-xs text-muted-foreground">{profile.account?.email || "未读取邮箱"}</div>
                      <div className="mt-1 truncate font-mono text-[10px] text-muted-foreground">账号 ID：{profile.account?.user_id || token?.account_id || "等待刷新"}</div>
                      {profile.state?.last_success_at_text && <div className="mt-1 text-[10px] text-muted-foreground">上次成功：{profile.state.last_success_at_text} · 下次刷新：{profile.state.next_refresh_at_text || "—"}</div>}
                      {(profile.state?.last_error || token?.credits_error) && <div className="mt-2 text-xs text-red-300">{profile.state?.last_error || token?.credits_error}</div>}
                    </div>
                    <div className="flex gap-1">
                      <Button variant="ghost" size="sm" onClick={() => void run(`refresh-${profile.id}`, () => api.refreshAdobeProfile(profile.id), `${profile.name} 已刷新`)} disabled={busy === `refresh-${profile.id}`}><RefreshCw className={`h-3.5 w-3.5 ${busy === `refresh-${profile.id}` ? "animate-spin" : ""}`} />刷新</Button>
                      <Button variant="ghost" size="sm" onClick={() => void run(`toggle-${profile.id}`, () => api.toggleAdobeProfile(profile.id, !profile.enabled), profile.enabled ? "账号已停用" : "账号已启用")}>{profile.enabled ? "停用" : "启用"}</Button>
                      <Button variant="ghost" size="icon" onClick={() => { if (confirm(`确定删除 Adobe 账号 ${profile.name || profile.id} 吗？`)) void run(`delete-${profile.id}`, () => api.deleteAdobeProfile(profile.id), "Adobe 账号已删除"); }}><Trash2 className="h-4 w-4 text-red-300" /></Button>
                    </div>
                  </div>
                </div>
              );
            })}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}

function Field({ label, className = "", children }: { label: string; className?: string; children: React.ReactNode }) {
  return <label className={`space-y-1 ${className}`}><span className="text-xs text-muted-foreground">{label}</span>{children}</label>;
}

function ToggleField({ label, checked, onChange }: { label: string; checked: boolean; onChange: (value: boolean) => void }) {
  return <div className="flex items-center justify-between rounded-md border border-border px-3 py-2"><span className="text-xs">{label}</span><Switch checked={checked} onCheckedChange={onChange} /></div>;
}

function Empty({ text }: { text: string }) {
  return <div className="rounded-lg border border-dashed border-border p-6 text-center text-xs text-muted-foreground">{text}</div>;
}

function CreditsBadge({ token }: { token?: AdobeToken }) {
  if (!token) return null;
  const raw = token.credits;
  const remaining = typeof raw === "number" ? raw : raw?.remaining;
  if (typeof remaining !== "number") return null;
  return <Badge tone={remaining > 0 ? "success" : "danger"}>额度 {remaining.toLocaleString()}</Badge>;
}

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : String(error);
}
