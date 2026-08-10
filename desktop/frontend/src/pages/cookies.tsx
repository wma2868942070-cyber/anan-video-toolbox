import { useCallback, useEffect, useMemo, useState } from "react";
import {
  KeyRound,
  Plus,
  Pencil,
  RefreshCw,
  ToggleLeft,
  ToggleRight,
  Trash2,
  Wallet,
  ShieldCheck,
  ShieldAlert,
  Power,
  Globe2,
  UserRoundPlus,
  ExternalLink,
  Loader2,
  Copy,
  Server,
} from "lucide-react";
import { Card, CardContent, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { Dialog } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { useToast } from "@/components/ui/toast";
import {
  api,
  type Cookie,
  type CookieHealth,
  type LeonardoBrowserWorkspace,
  type LeonardoServiceStatus,
} from "@/lib/api";
import { useWailsEvent } from "@/lib/events";

export function CookiesPage() {
  const { showSuccess, showError } = useToast();

  const [cookies, setCookies] = useState<Cookie[] | null>(null);
  const [health, setHealth] = useState<CookieHealth | null>(null);
  const [refreshing, setRefreshing] = useState(false);
  const [adding, setAdding] = useState(false);
  const [rawCookie, setRawCookie] = useState("");
  const [editing, setEditing] = useState<Cookie | null>(null);
  const [editValue, setEditValue] = useState("");
  const [editSaving, setEditSaving] = useState(false);
  const [workspaces, setWorkspaces] = useState<LeonardoBrowserWorkspace[] | null>(null);
  const [workspaceName, setWorkspaceName] = useState("");
  const [workspaceBusy, setWorkspaceBusy] = useState("");
  const [serviceStatus, setServiceStatus] = useState<LeonardoServiceStatus | null>(null);
  const [serviceBusy, setServiceBusy] = useState(false);

  const reload = useCallback(async () => {
    try {
      const [list, summary, browserWorkspaces, localService] = await Promise.all([
        api.listCookies(),
        api.cookieHealth(),
        api.listLeonardoBrowserWorkspaces(),
        api.leonardoServiceStatus(),
      ]);
      setCookies(list);
      setHealth(summary);
      setWorkspaces(browserWorkspaces);
      setServiceStatus(localService);
    } catch (err) {
      showError(`加载账号池失败：${errorMessage(err)}`);
    }
  }, [showError]);

  useEffect(() => {
    void reload();
  }, [reload]);

  // Auto-refetch when backend signals balance changed (after generate runs).
  useWailsEvent("cookies:changed", () => {
    void reload();
  });

  // The 8001 service owns proactive session refresh. The page only reloads
  // local metadata, avoiding a second round of get-session calls from Wails.
  useEffect(() => {
    const POLL_MS = 30_000;
    const timer = setInterval(() => {
      if (document.hidden) return;
      void reload();
    }, POLL_MS);
    return () => clearInterval(timer);
  }, [reload]);

  const onAdd = async () => {
    const value = rawCookie.trim();
    if (!value) {
      showError("请先粘贴 get-session 请求的 Copy as cURL 完整内容。");
      return;
    }
    setAdding(true);
    try {
      const res = await api.addCookie(value);
      if (res.updated_existing) {
        showSuccess(
          `识别为已有账号：${res.email || "未识别邮箱"}，已更新长期会话，未重复添加${
            res.merged_duplicates > 0 ? `；同时合并 ${res.merged_duplicates} 条旧重复记录` : ""
          }。`
        );
      } else {
        showSuccess(
          `账号已保存：${res.email || "未识别邮箱"} · 余额 ${res.balance.toLocaleString()}`
        );
      }
      setRawCookie("");
      await reload();
    } catch (err) {
      showError(errorMessage(err));
    } finally {
      setAdding(false);
    }
  };

  const onRefreshAll = async () => {
    setRefreshing(true);
    try {
	  const res = await api.refreshCookieSessions();
	  showSuccess(`刷新完成：${res.ok}/${res.checked} 个账号成功${res.reenabled > 0 ? `，自动恢复 ${res.reenabled} 个` : ""}${res.merged > 0 ? `，合并 ${res.merged} 条旧重复记录` : ""}`);
      await reload();
    } catch (err) {
      showError(`刷新失败：${errorMessage(err)}`);
    } finally {
      setRefreshing(false);
    }
  };

  const onToggle = async (cookie: Cookie) => {
    try {
      await api.toggleCookie(cookie.id, !cookie.is_active);
      await reload();
    } catch (err) {
      showError(errorMessage(err));
    }
  };

  const onDelete = async (cookie: Cookie) => {
    if (!confirm(`确定删除账号 #${cookie.id}（${cookie.email || "无邮箱"}）吗？`)) {
      return;
    }
    try {
      const result = await api.deleteCookie(cookie.id);
      showSuccess(
        result.workspaces_removed > 0
          ? `账号 #${cookie.id} 已删除，并同步移除 ${result.workspaces_removed} 个隔离工作区。`
          : `账号 #${cookie.id} 已删除。`
      );
      await reload();
    } catch (err) {
      showError(errorMessage(err));
    }
  };

  const onEdit = (cookie: Cookie) => {
    setEditing(cookie);
    setEditValue("");
  };

  const onSubmitEdit = async () => {
    if (!editing) return;
    const value = editValue.trim();
    if (!value) {
      showError("请先粘贴新的 get-session 请求 Copy as cURL 内容。");
      return;
    }
    setEditSaving(true);
    try {
      const res = await api.updateCookie(editing.id, value);
      showSuccess(
        `账号 #${editing.id} 已更新 · 余额 ${res.balance.toLocaleString()}`
      );
      setEditing(null);
      setEditValue("");
      await reload();
    } catch (err) {
      showError(errorMessage(err));
    } finally {
      setEditSaving(false);
    }
  };

  const onLaunchWorkspace = async (name: string) => {
    const key = `launch-${name.trim() || "new"}`;
    setWorkspaceBusy(key);
    try {
      const workspace = await api.launchLeonardoBrowserWorkspace(name.trim());
      setWorkspaceName("");
      showSuccess(
        `${workspace.name} 已用 Google Chrome 打开。请只登录一个 Leonardo 账号，登录完成后返回点击“一键读取 Cookie”；导入成功后可以关闭该窗口。`
      );
      await reload();
    } catch (err) {
      showError(`打开隔离账号工作区失败：${errorMessage(err)}`);
    } finally {
      setWorkspaceBusy("");
    }
  };

  const onReopenWorkspace = async (workspace: LeonardoBrowserWorkspace) => {
    const key = `reopen-${workspace.id}`;
    setWorkspaceBusy(key);
    try {
      await api.reopenLeonardoBrowserWorkspace(workspace.id);
      showSuccess(`${workspace.name} 已重新打开原有独立 Google Chrome 工作区。`);
      await reload();
    } catch (err) {
      showError(`重新打开隔离账号工作区失败：${errorMessage(err)}`);
    } finally {
      setWorkspaceBusy("");
    }
  };

  const onImportWorkspace = async (workspace: LeonardoBrowserWorkspace) => {
    const key = `import-${workspace.id}`;
    setWorkspaceBusy(key);
    try {
      const res = await api.importLeonardoBrowserWorkspace(workspace.id);
      if (res.warning) {
        showSuccess(`${workspace.name} 的 Cookie 已读取并更新已有账号。${res.warning}`);
        await reload();
        return;
      }
      if (res.updated_existing) {
        showSuccess(
          `${workspace.name} 已识别为已有账号 ${res.email || "未识别邮箱"}，长期会话已更新，不会重复添加。`
        );
      } else {
        showSuccess(
          `${workspace.name} 已导入：${res.email || "未识别邮箱"} · 余额 ${res.balance.toLocaleString()}`
        );
      }
      await reload();
    } catch (err) {
      showError(`读取或导入 Cookie 失败：${errorMessage(err)}`);
    } finally {
      setWorkspaceBusy("");
    }
  };

  const copyServiceURL = async () => {
    try {
      await navigator.clipboard.writeText(serviceStatus?.baseURL || "http://127.0.0.1:8001");
      showSuccess("Leonardo 本地反代地址已复制");
    } catch (err) {
      showError(`复制失败：${errorMessage(err)}`);
    }
  };

  const restartService = async () => {
    setServiceBusy(true);
    try {
      const status = await api.restartLeonardoService();
      setServiceStatus(status);
      showSuccess("Leonardo 8001 本地服务已重新启动");
      await reload();
    } catch (err) {
      showError(`重启失败：${errorMessage(err)}`);
    } finally {
      setServiceBusy(false);
    }
  };

  return (
    <div className="h-full overflow-y-auto">
      <div className="space-y-6 p-6">
        <Card>
          <div className="flex flex-col gap-4 p-5 md:flex-row md:items-center md:justify-between">
            <div className="min-w-0">
              <CardTitle className="flex flex-wrap items-center gap-2 text-base">
                <Server className="h-4 w-4 text-primary" />
                Leonardo 本地反代
                <Badge tone={serviceStatus?.running ? "success" : "danger"}>
                  {serviceStatus === null ? "检测中" : serviceStatus.running ? "运行中" : "未运行"}
                </Badge>
              </CardTitle>
              <p className="mt-1 text-xs text-muted-foreground">
                端点：{serviceStatus?.baseURL || "http://127.0.0.1:8001"}
                {serviceStatus?.running
                  ? ` · 账号 ${serviceStatus.total} · 可用 ${serviceStatus.ready} · 活动任务 ${serviceStatus.activeTasks}`
                  : " · 本地账号池与无限画布网关"}
              </p>
              {serviceStatus?.error ? <p className="mt-1 text-xs text-red-300">{serviceStatus.error}</p> : null}
            </div>
            <div className="flex flex-wrap gap-2">
              <Button
                variant="outline"
                size="sm"
                onClick={() => void restartService()}
                disabled={serviceBusy || (serviceStatus?.activeTasks || 0) > 0}
                title={(serviceStatus?.activeTasks || 0) > 0 ? "有生成任务运行时不能重启" : "重启 8001 本地服务"}
              >
                <RefreshCw className={`h-4 w-4 ${serviceBusy ? "animate-spin" : ""}`} />重启服务
              </Button>
              <Button variant="outline" size="sm" onClick={() => void reload()}>
                <RefreshCw className="h-4 w-4" />刷新状态
              </Button>
              <Button variant="outline" size="sm" onClick={() => void copyServiceURL()}>
                <Copy className="h-4 w-4" />复制接口地址
              </Button>
              <Button
                variant="outline"
                size="sm"
                onClick={() => void api.openURL(serviceStatus?.adminURL || "http://127.0.0.1:8001/admin")}
                disabled={!serviceStatus?.running}
              >
                <ExternalLink className="h-4 w-4" />高级后台
              </Button>
            </div>
          </div>
        </Card>
        <Stats health={health} />

      <Card>
        <div className="flex items-center justify-between p-5 pb-3">
          <div>
            <CardTitle className="flex items-center gap-2 text-base">
              <Globe2 className="h-4 w-4 text-primary" />
              Leonardo 隔离账号库
            </CardTitle>
            <p className="mt-1 text-xs text-muted-foreground">
              每个账号使用独立 Google Chrome 用户目录长期保留登录状态。导入后可以关闭 Chrome；账号续期失败时，后台会短暂无界面读取该工作区的新 Cookie。
            </p>
          </div>
        </div>
        <CardContent className="space-y-3 pt-0">
          <div className="flex gap-2">
            <Input
              value={workspaceName}
              onChange={(event) => setWorkspaceName(event.target.value)}
              placeholder="账号备注，例如 Leonardo-01"
            />
            <Button
              onClick={() => void onLaunchWorkspace(workspaceName)}
              disabled={workspaceBusy.startsWith("launch-")}
            >
              {workspaceBusy.startsWith("launch-") ? (
                <Loader2 className="h-4 w-4 animate-spin" />
              ) : (
                <UserRoundPlus className="h-4 w-4" />
              )}
              新建并登录
            </Button>
          </div>
          <div className="rounded-lg border border-primary/20 bg-primary/5 p-3 text-xs leading-5 text-muted-foreground">
            每次点击“新建并登录”都会创建全新的工作区 ID、Chrome 用户目录和调试端口，即使备注名相同也不会复用旧账号。登录完成后点击“一键读取 Cookie”；只有卡片内的“重新打开”会复用该卡片原有工作区。
          </div>
          {workspaces === null ? (
            <CookiesSkeleton />
          ) : workspaces.length === 0 ? (
            <div className="rounded-lg border border-dashed border-border p-6 text-center text-xs text-muted-foreground">
              尚未创建 Leonardo 隔离账号工作区
            </div>
          ) : (
            <div className="grid gap-2 lg:grid-cols-2">
              {workspaces.map((workspace) => (
                <div key={workspace.id} className="rounded-lg border border-border bg-background/40 p-3">
                  <div className="flex items-start justify-between gap-2">
                    <div className="min-w-0">
                      <div className="font-medium">{workspace.name}</div>
                      <div className="mt-1 truncate font-mono text-[10px] text-muted-foreground">
                        {workspace.profileDir}
                      </div>
                      <div className="mt-1 text-[10px] text-muted-foreground">
                        {workspace.lastOpenedAt
                          ? `最近打开：${new Date(workspace.lastOpenedAt * 1000).toLocaleString()}`
                          : "尚未打开"}
                      </div>
                    </div>
                    <div className="flex flex-wrap justify-end gap-1">
                      <Badge tone={workspace.bound ? "success" : "warning"}>
                        {workspace.bound ? "已绑定账号" : "待导入账号"}
                      </Badge>
                      {workspace.debugPort > 0 ? <Badge tone="info">{workspace.browser === "chrome" ? "Chrome" : "旧 Edge"} {workspace.debugPort}</Badge> : null}
                    </div>
                  </div>
                  <div className="mt-3 flex flex-wrap gap-2">
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => void onReopenWorkspace(workspace)}
                      disabled={workspaceBusy === `reopen-${workspace.id}`}
                    >
                      {workspaceBusy === `reopen-${workspace.id}` ? (
                        <Loader2 className="h-3.5 w-3.5 animate-spin" />
                      ) : (
                        <ExternalLink className="h-3.5 w-3.5" />
                      )}
                      重新打开
                    </Button>
                    <Button
                      size="sm"
                      onClick={() => void onImportWorkspace(workspace)}
                      disabled={workspaceBusy === `import-${workspace.id}`}
                    >
                      {workspaceBusy === `import-${workspace.id}` ? (
                        <Loader2 className="h-3.5 w-3.5 animate-spin" />
                      ) : (
                        <ShieldCheck className="h-3.5 w-3.5" />
                      )}
                      一键读取 Cookie
                    </Button>
                  </div>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      <Card>
        <div className="flex items-center justify-between p-5 pb-3">
          <div>
            <CardTitle className="text-base">添加账号</CardTitle>
            <p className="mt-1 text-xs text-muted-foreground">
              请从 Leonardo 的长期登录会话请求中复制完整 cURL，不要使用 GraphQL 的短期 Bearer Token。
            </p>
          </div>
        </div>
        <CardContent className="space-y-3 pt-0">
          <div className="rounded-lg border border-amber-500/30 bg-amber-500/5 p-3 text-xs leading-5">
            <p className="font-medium text-amber-200">查找可自动续期的长期 Cookie</p>
            <ol className="mt-1 list-decimal space-y-0.5 pl-4 text-muted-foreground">
              <li>登录 Leonardo，按 F12 打开开发者工具并进入 Network（网络）。</li>
              <li>刷新页面，在筛选框搜索 <code className="text-foreground">get-session</code>。</li>
              <li>
                选择请求地址：
                <code className="ml-1 break-all text-foreground">
                  https://app.leonardo.ai/api/auth/get-session
                </code>
              </li>
              <li>确认状态为 200，且请求头包含 Cookie；右键该请求选择 Copy as cURL。</li>
              <li>将复制的完整 cURL 粘贴到下方，然后点击“添加账号”。</li>
            </ol>
            <p className="mt-2 text-amber-300/90">
              注意：不要选择 api.leonardo.ai/v1/graphql；其中的 Authorization: Bearer 只是短期令牌，无法自动续期。
            </p>
            <p className="mt-1 text-amber-300/90">
              多账号必须分别使用不同的 Google Chrome 隔离工作区登录。不要在同一个网页会话中直接切换账号，否则 Leonardo 可能复用会话 Cookie，导致旧账号会话被远端切换。
            </p>
          </div>
          <Textarea
            value={rawCookie}
            onChange={(e) => setRawCookie(e.target.value)}
            placeholder="粘贴 get-session 请求的 Copy as cURL 完整内容"
            className="min-h-[120px] font-mono text-xs"
            spellCheck={false}
          />
          <div className="flex justify-end">
            <Button onClick={onAdd} disabled={adding || !rawCookie.trim()}>
              {adding ? (
                <RefreshCw className="h-4 w-4 animate-spin" />
              ) : (
                <Plus className="h-4 w-4" />
              )}
              {adding ? "正在验证" : "添加账号"}
            </Button>
          </div>
        </CardContent>
      </Card>

      <Card>
        <div className="flex items-center justify-between p-5 pb-2">
          <CardTitle className="text-base">
            账号池
            {cookies !== null ? (
              <span className="ml-2 text-xs font-normal text-muted-foreground">
                {cookies.length}
              </span>
            ) : null}
          </CardTitle>
          <Button
            variant="outline"
            size="sm"
            onClick={onRefreshAll}
            disabled={refreshing || (cookies?.length ?? 0) === 0}
          >
            <RefreshCw className={`h-4 w-4 ${refreshing ? "animate-spin" : ""}`} />
            刷新全部
          </Button>
        </div>
		<p className="px-5 pb-3 text-xs text-muted-foreground">
		  账号池已使用 leo2api 会话内核：保存 Leonardo 轮换后的 Cookie、缓存每账号 JWT，并在到期前自动刷新。429、网络错误和安全检查只会进入冷却，不会误删账号。
		</p>
        <CardContent className="pt-0">
          {cookies === null ? (
            <CookiesSkeleton />
          ) : cookies.length === 0 ? (
            <EmptyPool />
          ) : (
            <div className="space-y-2">
              {cookies.map((c) => (
                <CookieRow
                  key={c.id}
                  cookie={c}
                  onToggle={() => onToggle(c)}
                  onDelete={() => onDelete(c)}
                  onEdit={() => onEdit(c)}
                />
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      <Dialog
        open={editing !== null}
        onClose={() => setEditing(null)}
        title={editing ? `更新账号 #${editing.id}` : "更新账号"}
        description={editing?.email ? editing.email : undefined}
      >
        <div className="space-y-3">
          <p className="text-xs leading-5 text-muted-foreground">
            请粘贴 <code className="text-foreground">https://app.leonardo.ai/api/auth/get-session</code>
            请求的 Copy as cURL 完整内容，以便令牌失效后自动续期。
          </p>
          <Textarea
            value={editValue}
            onChange={(e) => setEditValue(e.target.value)}
            placeholder="粘贴新的 get-session 请求 Copy as cURL 内容"
            className="min-h-[140px] font-mono text-xs"
            spellCheck={false}
          />
          <div className="flex justify-end gap-2">
            <Button
              variant="outline"
              size="sm"
              onClick={() => setEditing(null)}
              disabled={editSaving}
            >
              取消
            </Button>
            <Button
              size="sm"
              onClick={onSubmitEdit}
              disabled={editSaving || !editValue.trim()}
            >
              {editSaving ? (
                <RefreshCw className="h-4 w-4 animate-spin" />
              ) : (
                <Pencil className="h-4 w-4" />
              )}
              保存更新
            </Button>
          </div>
        </div>
      </Dialog>
      </div>
    </div>
  );
}

function Stats({ health }: { health: CookieHealth | null }) {
  const cards = useMemo(
    () => [
      {
        label: "可用总余额",
        value: health ? health.active_balance.toLocaleString() : null,
        icon: Wallet,
        tint: "from-violet-500/30 to-violet-500/0 text-violet-300",
      },
      {
        label: "可用账号",
        value: health ? `${health.ready}` : null,
        icon: ShieldCheck,
        tint: "from-emerald-500/30 to-emerald-500/0 text-emerald-300",
      },
      {
        label: "余额耗尽",
        value: health ? `${health.depleted}` : null,
        icon: ShieldAlert,
        tint: "from-amber-500/30 to-amber-500/0 text-amber-300",
      },
      {
        label: "刷新冷却",
        value: health ? `${health.temporary}` : null,
        icon: RefreshCw,
        tint: "from-cyan-500/30 to-cyan-500/0 text-cyan-300",
      },
      {
        label: "已停用",
        value: health ? `${health.disabled}` : null,
        icon: Power,
        tint: "from-slate-500/30 to-slate-500/0 text-slate-300",
      },
    ],
    [health]
  );

  return (
    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-5">
      {cards.map(({ label, value, icon: Icon, tint }) => (
        <Card
          key={label}
          className="overflow-hidden transition hover:border-primary/30"
        >
          <div className="relative px-4 py-4">
            <div
              className={`absolute right-0 top-0 h-24 w-24 rounded-full bg-gradient-to-bl ${tint} blur-2xl`}
            />
            <div className="relative flex items-center justify-between">
              <div>
                <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
                  {label}
                </p>
                {value === null ? (
                  <Skeleton className="mt-2 h-7 w-20" />
                ) : (
                  <p className="mt-1 text-2xl font-semibold tracking-tight">
                    {value}
                  </p>
                )}
              </div>
              <Icon className="h-5 w-5 text-muted-foreground" />
            </div>
          </div>
        </Card>
      ))}
    </div>
  );
}

function StatusBadge({ status }: { status: Cookie["status"] }) {
  if (status === "READY") return <Badge tone="success">可用</Badge>;
  if (status === "TEMPORARY") return <Badge tone="warning">临时冷却</Badge>;
  if (status === "INVALID") return <Badge tone="danger">会话已失效</Badge>;
  if (status === "ABNORMAL") return <Badge tone="danger">会话异常</Badge>;
  if (status === "DEPLETED") return <Badge tone="warning">已耗尽</Badge>;
  return <Badge tone="neutral">已停用</Badge>;
}

function CookieRow({
  cookie,
  onToggle,
  onDelete,
  onEdit,
}: {
  cookie: Cookie;
  onToggle: () => void;
  onDelete: () => void;
  onEdit: () => void;
}) {
  const last = cookie.last_checked_at
    ? new Date(cookie.last_checked_at * 1000).toLocaleString()
    : "—";
  const jwtExpiry = cookie.jwt_expires_at
    ? new Date(cookie.jwt_expires_at * 1000).toLocaleString()
    : "—";
  const nextRetry = cookie.error_until && cookie.error_until * 1000 > Date.now()
    ? new Date(cookie.error_until * 1000).toLocaleTimeString()
    : "";
  return (
    <div className="flex flex-col gap-3 rounded-lg border border-border bg-background/40 px-4 py-3 transition hover:border-primary/30 sm:flex-row sm:items-center sm:justify-between">
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <span className="font-medium">
            {cookie.email || `Cookie #${cookie.id}`}
          </span>
          <StatusBadge status={cookie.status} />
          {cookie.disabled_reason ? (
            <Badge tone="danger">{cookie.disabled_reason}</Badge>
          ) : null}
        </div>
        <p className="mt-1 truncate text-xs text-muted-foreground">
          余额{" "}
          <span className="font-medium text-foreground">
            {cookie.last_balance.toLocaleString()}
          </span>
          {" · "}最近检查 {last}
          {cookie.account_id ? (
            <>
              {" · "}账号 ID {cookie.account_id.slice(0, 8)}…
            </>
          ) : null}
          {cookie.jwt_expires_at ? <> {" · "}JWT 到期 {jwtExpiry}</> : null}
          {nextRetry ? <> {" · "}下次重试 {nextRetry}</> : null}
          {cookie.refresh_fail_count > 0 ? <> {" · "}连续刷新失败 {cookie.refresh_fail_count} 次</> : null}
          {cookie.last_error ? (
            <>
              {" · "}
              <span className="text-red-300">{cookie.last_error}</span>
            </>
          ) : null}
        </p>
		{cookie.disabled_reason === "AUTH_EXPIRED" && !cookie.auto_recoverable ? (
		  <p className="mt-1 text-xs text-amber-300">
			当前仅保存了短期令牌，无法自动续期；请点击铅笔，更新 get-session 请求的 Copy as cURL。
		  </p>
		) : null}
		{cookie.disabled_reason === "ACCOUNT_CHANGED" ? (
		  <p className="mt-1 text-xs text-amber-300">
			检测到该浏览器会话被切换到了另一个 Leonardo 账号。原账号记录已保留；请在独立浏览器配置文件中重新登录此账号，再点击铅笔更新。
		  </p>
		) : null}
      </div>
      <div className="flex shrink-0 items-center gap-1">
        <Button
          variant="ghost"
          size="sm"
          onClick={onToggle}
          aria-label={cookie.is_active ? "停用账号" : "启用账号"}
        >
          {cookie.is_active ? (
            <ToggleRight className="h-4 w-4 text-emerald-400" />
          ) : (
            <ToggleLeft className="h-4 w-4" />
          )}
          <span className="text-xs">
            {cookie.is_active ? "已启用" : "已停用"}
          </span>
        </Button>
        <Button
          variant="ghost"
          size="icon"
          onClick={onEdit}
          aria-label="更新账号"
        >
          <Pencil className="h-4 w-4" />
        </Button>
        <Button
          variant="ghost"
          size="icon"
          onClick={onDelete}
          aria-label="删除账号"
        >
          <Trash2 className="h-4 w-4 text-red-300" />
        </Button>
      </div>
    </div>
  );
}

function CookiesSkeleton() {
  return (
    <div className="space-y-2">
      {[0, 1, 2].map((i) => (
        <div
          key={i}
          className="flex items-center justify-between rounded-lg border border-border bg-background/40 px-4 py-3"
        >
          <div className="flex flex-1 flex-col gap-2">
            <Skeleton className="h-4 w-40" />
            <Skeleton className="h-3 w-72" />
          </div>
          <Skeleton className="h-7 w-16" />
        </div>
      ))}
    </div>
  );
}

function EmptyPool() {
  return (
    <div className="flex flex-col items-center justify-center gap-2 py-10 text-center">
      <div className="flex h-12 w-12 items-center justify-center rounded-xl bg-accent text-accent-foreground">
        <KeyRound className="h-5 w-5" />
      </div>
      <p className="text-sm font-medium">暂无账号</p>
      <p className="text-xs text-muted-foreground">在上方添加你有权使用的 Leonardo 账号会话。</p>
    </div>
  );
}

function errorMessage(error: unknown) {
  if (error instanceof Error && error.message.trim()) return error.message;
  if (typeof error === "string" && error.trim()) return error;
  if (error && typeof error === "object") {
    const value = error as { message?: unknown; error?: unknown };
    if (typeof value.message === "string" && value.message.trim()) return value.message;
    if (typeof value.error === "string" && value.error.trim()) return value.error;
  }
  return "未知错误，请重新打开对应 Chrome 工作区后再试";
}
