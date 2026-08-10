import { useCallback, useEffect, useState } from "react";
import { ExternalLink, FolderOpen, Loader2, Play, RefreshCw } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { api, type VideoClawServiceStatus } from "@/lib/api";
import { useToast } from "@/components/ui/toast";

const DEFAULT_URL = "http://127.0.0.1:6102";

export function VideoClawPage() {
  const { showError, showSuccess } = useToast();
  const [status, setStatus] = useState<VideoClawServiceStatus | null>(null);
  const [busy, setBusy] = useState(false);
  const [reloadKey, setReloadKey] = useState(0);

  const reloadStatus = useCallback(async () => {
    try {
      setStatus(await api.videoClawServiceStatus());
    } catch (error) {
      showError(`读取 VideoClaw 状态失败：${errorMessage(error)}`);
    }
  }, [showError]);

  useEffect(() => {
    void reloadStatus();
    const timer = setInterval(() => {
      if (!document.hidden) void reloadStatus();
    }, 15_000);
    return () => clearInterval(timer);
  }, [reloadStatus]);

  const startOrRestart = async () => {
    setBusy(true);
    try {
      const next = status?.running
        ? await api.restartVideoClawService()
        : await api.startVideoClawService();
      setStatus(next);
      setReloadKey((value) => value + 1);
      showSuccess(status?.running ? "VideoClaw 已重新启动" : "VideoClaw 已启动");
    } catch (error) {
      showError(errorMessage(error));
      await reloadStatus();
    } finally {
      setBusy(false);
    }
  };

  const frontendURL = status?.frontendURL || DEFAULT_URL;

  return (
    <div className="flex h-full flex-col overflow-hidden bg-background">
      <div className="flex shrink-0 flex-wrap items-center justify-between gap-3 border-b border-border px-4 py-2">
        <div>
          <div className="flex items-center gap-2">
            <span className="text-sm font-semibold">VideoClaw AI 导演</span>
            <Badge tone={status?.running ? "success" : "danger"}>
              {status?.running ? "运行中" : "未运行"}
            </Badge>
            {status && !status.running ? (
              <span className="text-[10px] text-muted-foreground">
                后端 {status.backendRunning ? "正常" : "停止"} · 前端 {status.frontendRunning ? "正常" : "停止"}
              </span>
            ) : null}
          </div>
          <div className="mt-0.5 text-[11px] text-muted-foreground">
            一句话完成剧本、角色、场景、分镜、视频与后期 · 本地地址 {frontendURL}
          </div>
          {status?.error ? <div className="mt-1 max-w-3xl truncate text-[11px] text-red-300">{status.error}</div> : null}
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Button variant="outline" size="sm" disabled={busy} onClick={() => void startOrRestart()}>
            {busy ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : status?.running ? <RefreshCw className="h-3.5 w-3.5" /> : <Play className="h-3.5 w-3.5" />}
            {status?.running ? "重启服务" : "启动服务"}
          </Button>
          <Button variant="outline" size="sm" disabled={!status?.running} onClick={() => setReloadKey((value) => value + 1)}>
            <RefreshCw className="h-3.5 w-3.5" />刷新页面
          </Button>
          <Button variant="outline" size="sm" disabled={!status?.stateDir} onClick={() => status?.stateDir && void api.openInFileManager(status.stateDir)}>
            <FolderOpen className="h-3.5 w-3.5" />数据目录
          </Button>
          <Button variant="outline" size="sm" disabled={!status?.running} onClick={() => void api.openURL(frontendURL)}>
            <ExternalLink className="h-3.5 w-3.5" />浏览器打开
          </Button>
        </div>
      </div>

      {status?.running ? (
        <iframe
          key={reloadKey}
          title="VideoClaw AI 导演"
          src={frontendURL}
          allow="clipboard-read; clipboard-write; microphone"
          className="min-h-0 flex-1 border-0 bg-background"
        />
      ) : (
        <div className="flex min-h-0 flex-1 items-center justify-center p-8">
          <div className="max-w-xl rounded-xl border border-border bg-card p-6 text-center shadow-sm">
            <div className="text-base font-semibold">VideoClaw 尚未启动</div>
            <p className="mt-2 text-sm leading-6 text-muted-foreground">
              首次启动会自动创建独立 Python 3.11 环境、安装前后端依赖并构建页面，生成项目和配置保存在用户数据目录，不写入第三方源码。
            </p>
            {status?.error ? <p className="mt-3 break-words text-xs text-red-300">{status.error}</p> : null}
            <Button className="mt-5" disabled={busy} onClick={() => void startOrRestart()}>
              {busy ? <Loader2 className="h-4 w-4 animate-spin" /> : <Play className="h-4 w-4" />}
              启动 VideoClaw
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
