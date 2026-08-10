import { useCallback, useEffect, useMemo, useState } from "react";
import { ExternalLink, FolderOpen, Loader2, Play, RefreshCw, RotateCw } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { useToast } from "@/components/ui/toast";
import { api, type LocalMiniDramaServiceStatus } from "@/lib/api";

const DEFAULT_URL = "http://127.0.0.1:6201";

export function LocalMiniDramaPage() {
  const { showError, showSuccess } = useToast();
  const [status, setStatus] = useState<LocalMiniDramaServiceStatus | null>(null);
  const [busy, setBusy] = useState<"start" | "sync" | null>(null);
  const [reloadKey, setReloadKey] = useState(0);

  const reloadStatus = useCallback(async () => {
    try {
      setStatus(await api.localMiniDramaServiceStatus());
    } catch (error) {
      showError(`读取本地短剧状态失败：${errorMessage(error)}`);
    }
  }, [showError]);

  useEffect(() => {
    void reloadStatus();
    const timer = window.setInterval(() => {
      if (!document.hidden) void reloadStatus();
    }, 15_000);
    return () => window.clearInterval(timer);
  }, [reloadStatus]);

  const groupedCounts = useMemo(() => {
    const rows = status?.modelConfigs || [];
    const count = (provider: string, type: string) =>
      rows.find((row) => row.name.includes(provider) && row.service_type === type)?.model_count || 0;
    return {
      leonardoImage: count("Leonardo", "image"),
      leonardoVideo: count("Leonardo", "video"),
      adobeImage: count("Adobe", "image"),
      adobeVideo: count("Adobe", "video"),
    };
  }, [status]);

  const startOrRestart = async () => {
    setBusy("start");
    try {
      const wasRunning = Boolean(status?.running);
      const next = wasRunning
        ? await api.restartLocalMiniDramaService()
        : await api.startLocalMiniDramaService();
      setStatus(next);
      setReloadKey((value) => value + 1);
      showSuccess(wasRunning ? "本地短剧已重新启动" : "本地短剧已启动");
    } catch (error) {
      showError(errorMessage(error));
      await reloadStatus();
    } finally {
      setBusy(null);
    }
  };

  const syncModels = async () => {
    setBusy("sync");
    try {
      await api.syncLocalMiniDramaModels();
      await reloadStatus();
      showSuccess("Leonardo 与 Adobe 模型目录已同步");
    } catch (error) {
      showError(`模型同步失败：${errorMessage(error)}`);
    } finally {
      setBusy(null);
    }
  };

  const url = status?.url || DEFAULT_URL;

  return (
    <div className="flex h-full flex-col overflow-hidden bg-background">
      <div className="flex shrink-0 flex-wrap items-center justify-between gap-3 border-b border-border px-4 py-2">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-sm font-semibold">本地短剧 · LocalMiniDrama</span>
            <Badge tone={status?.running ? "success" : "danger"}>
              {status?.running ? "运行中" : "未运行"}
            </Badge>
            {status?.running ? (
              <span className="text-[10px] text-muted-foreground">
                Leo 图 {groupedCounts.leonardoImage} / 视频 {groupedCounts.leonardoVideo} · Adobe 图 {groupedCounts.adobeImage} / 视频 {groupedCounts.adobeVideo}
              </span>
            ) : null}
          </div>
          <div className="mt-0.5 text-[11px] text-muted-foreground">
            剧本、角色、场景、分镜与长视频任务统一保存在本机 · {url}
          </div>
          {status?.error ? <div className="mt-1 max-w-3xl truncate text-[11px] text-red-300">{status.error}</div> : null}
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Button variant="outline" size="sm" disabled={busy !== null} onClick={() => void startOrRestart()}>
            {busy === "start" ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : status?.running ? <RotateCw className="h-3.5 w-3.5" /> : <Play className="h-3.5 w-3.5" />}
            {status?.running ? "重启服务" : "启动服务"}
          </Button>
          <Button variant="outline" size="sm" disabled={!status?.running || busy !== null} onClick={() => void syncModels()}>
            {busy === "sync" ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <RefreshCw className="h-3.5 w-3.5" />}
            同步模型
          </Button>
          <Button variant="outline" size="sm" disabled={!status?.running} onClick={() => setReloadKey((value) => value + 1)}>
            <RefreshCw className="h-3.5 w-3.5" />刷新页面
          </Button>
          <Button variant="outline" size="sm" disabled={!status?.stateDir} onClick={() => status?.stateDir && void api.openInFileManager(status.stateDir)}>
            <FolderOpen className="h-3.5 w-3.5" />数据目录
          </Button>
          <Button variant="outline" size="sm" disabled={!status?.running} onClick={() => void api.openURL(url)}>
            <ExternalLink className="h-3.5 w-3.5" />浏览器打开
          </Button>
        </div>
      </div>

      {status?.running ? (
        <iframe
          key={reloadKey}
          title="本地短剧 LocalMiniDrama"
          src={url}
          allow="clipboard-read; clipboard-write; microphone"
          className="min-h-0 flex-1 border-0 bg-background"
        />
      ) : (
        <div className="flex min-h-0 flex-1 items-center justify-center p-8">
          <div className="max-w-xl border border-border bg-card p-6 text-center shadow-sm">
            <div className="text-base font-semibold">本地短剧尚未启动</div>
            <p className="mt-2 text-sm leading-6 text-muted-foreground">
              首次启动会在用户数据目录安装 Node.js 依赖并构建中文页面。项目、SQLite、素材和任务状态都与第三方源码隔离保存。
            </p>
            {status?.error ? <p className="mt-3 break-words text-xs text-red-300">{status.error}</p> : null}
            <Button className="mt-5" disabled={busy !== null} onClick={() => void startOrRestart()}>
              {busy === "start" ? <Loader2 className="h-4 w-4 animate-spin" /> : <Play className="h-4 w-4" />}
              启动本地短剧
            </Button>
          </div>
        </div>
      )}
    </div>
  );
}

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : String(error);
}
