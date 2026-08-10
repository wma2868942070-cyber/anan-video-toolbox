import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  Library as LibraryIcon,
  Clock,
  Wand2,
  ImageIcon,
  Repeat,
  Search,
  Play,
  Pause,
  Maximize2,
	RefreshCw,
	Download,
	Trash2,
	Loader2,
	CheckCircle2,
} from "lucide-react";
import { Card } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Lightbox } from "@/components/ui/lightbox";
import { useToast } from "@/components/ui/toast";
import { cn } from "@/lib/utils";
import { api, type GenerationLog } from "@/lib/api";
import { useWailsEvent } from "@/lib/events";
import { setReplay } from "@/lib/replay";
import type { NavId } from "@/components/sidebar";

type FilterMode = "all" | "image" | "video";

function isVideoLog(log: GenerationLog) {
  return log.mediaType === "video" || log.imageURLs.some((u) => /\.(mp4|webm|mov)(?:$|\?)/i.test(u));
}

export function LibraryPage({ onNavigate }: { onNavigate: (id: NavId) => void }) {
  const { showError, showSuccess } = useToast();
  const [logs, setLogs] = useState<GenerationLog[] | null>(null);
  const [previewURL, setPreviewURL] = useState<string | null>(null);
  const [search, setSearch] = useState("");
  const [filter, setFilter] = useState<FilterMode>("all");
	const [syncing, setSyncing] = useState(false);
	const [savingID, setSavingID] = useState<number | null>(null);
	const [deletingID, setDeletingID] = useState<number | null>(null);
	const didAutoSync = useRef(false);
	const syncingRef = useRef(false);

  const reload = useCallback(async () => {
    try {
      const list = await api.listGenerationLogs(200);
      setLogs(list);
    } catch (err) {
		showError(`加载素材库失败：${(err as Error).message}`);
    }
  }, [showError]);

	const syncRemote = useCallback(async (silent = false) => {
		if (syncingRef.current) return;
		syncingRef.current = true;
		setSyncing(true);
		try {
			const result = await api.syncLeonardoLibrary(100);
			await reload();
			if (!silent || result.added > 0 || result.updated > 0) {
				const summary = `已检查 ${result.accounts} 个账号，同步 ${result.synced_accounts} 个账号；新增 ${result.added}，更新 ${result.updated}`;
				if (result.failed > 0) {
					showError(`${summary}，失败 ${result.failed}。${result.errors[0] ?? ""}`);
				} else {
					showSuccess(summary);
				}
			}
		} catch (err) {
			if (!silent) {
				showError(`同步失败：${(err as Error).message}`);
			}
		} finally {
			syncingRef.current = false;
			setSyncing(false);
		}
	}, [reload, showError, showSuccess]);

  useEffect(() => {
    void reload();
  }, [reload]);

	// Load cached materials immediately, then sync website generations from
	// every account in the pool in the background.
	useEffect(() => {
		if (didAutoSync.current) return;
		didAutoSync.current = true;
		void syncRemote(true);
	}, [syncRemote]);

	// Keep website-created image/video materials fresh for every saved account
	// while the library is open. Hidden windows skip network work and resume on
	// the next interval or account-change event.
	useEffect(() => {
		const timer = window.setInterval(() => {
			if (document.hidden) return;
			void syncRemote(true);
		}, 120_000);
		return () => window.clearInterval(timer);
	}, [syncRemote]);

  // Account/session changes and completed generations can expose new remote
  // materials, so refresh all accounts rather than only reloading local rows.
  useWailsEvent("cookies:changed", () => {
    void syncRemote(true);
  });

  const onReplayLog = (log: GenerationLog) => {
    if (log.provider === "adobe") {
      showSuccess("Adobe 提示词和模型保留在素材记录中，请在无限画布选择 Adobe Firefly 渠道再次生成");
      onNavigate("canvas");
      return;
    }
    const target = isVideoLog(log) ? "video" : "image";
    setReplay({
      target,
      prompt: log.prompt,
      aspectRatio: log.aspectRatio || undefined,
      modelId: log.modelID,
    });
    showSuccess(
      target === "video"
			? "提示词已载入视频生成"
			: "提示词已载入图片生成"
    );
    onNavigate(target);
  };

	const onSaveLog = async (log: GenerationLog) => {
		setSavingID(log.id);
		try {
			const result = await api.saveGenerationLog(log.id);
			if (result.files.length > 0) {
				showSuccess(`已保存 ${result.files.length} 个文件${result.failed > 0 ? `，${result.failed} 个失败` : ""}`);
				await reload();
			}
		} catch (err) {
			showError(`保存失败：${(err as Error).message}`);
		} finally {
			setSavingID(null);
		}
	};

	const onDeleteLog = async (log: GenerationLog) => {
		if (!window.confirm("只从本地素材库移除，不会删除 Leonardo 网站上的原始作品。确定继续吗？")) {
			return;
		}
		setDeletingID(log.id);
		try {
			await api.deleteGenerationLog(log.id);
			setLogs((current) => current?.filter((item) => item.id !== log.id) ?? []);
			showSuccess("已从素材库移除");
		} catch (err) {
			showError(`删除失败：${(err as Error).message}`);
		} finally {
			setDeletingID(null);
		}
	};

  // Filter + search filter the local list so typing feels instant.
  const filtered = useMemo(() => {
    if (logs === null) return null;
    const q = search.trim().toLowerCase();
    return logs.filter((log) => {
      const isVid = isVideoLog(log);
      if (filter === "image" && isVid) return false;
      if (filter === "video" && !isVid) return false;
      if (!q) return true;
      return (
        log.prompt.toLowerCase().includes(q) ||
        log.modelID.toLowerCase().includes(q) ||
        log.providerGenerationID.toLowerCase().includes(q)
      );
    });
  }, [logs, search, filter]);

  if (logs === null) {
    return <LibrarySkeleton />;
  }

  return (
    <div className="h-full overflow-y-auto">
      <div className="space-y-4 p-6">
        <FilterBar
          search={search}
          onSearch={setSearch}
          filter={filter}
          onFilter={setFilter}
          counts={{
            all: logs.length,
            image: logs.filter((l) => !isVideoLog(l)).length,
            video: logs.filter((l) => isVideoLog(l)).length,
          }}
			syncing={syncing}
			onRefresh={() => void syncRemote(false)}
        />
        {filtered === null || filtered.length === 0 ? (
          <EmptyLibrary />
        ) : (
          <div className="space-y-3">
            {filtered.map((log) => (
              <LogCard
                key={log.id}
                log={log}
                onReplay={() => onReplayLog(log)}
                onPreview={(url) => setPreviewURL(url)}
				onSave={() => void onSaveLog(log)}
				onDelete={() => void onDeleteLog(log)}
				saving={savingID === log.id}
				deleting={deletingID === log.id}
              />
            ))}
          </div>
        )}
      </div>
      <Lightbox url={previewURL} onClose={() => setPreviewURL(null)} />
    </div>
  );
}

function FilterBar({
  search,
  onSearch,
  filter,
  onFilter,
  counts,
	syncing,
	onRefresh,
}: {
  search: string;
  onSearch: (v: string) => void;
  filter: FilterMode;
  onFilter: (m: FilterMode) => void;
  counts: { all: number; image: number; video: number };
	syncing: boolean;
	onRefresh: () => void;
}) {
  const tabs: Array<{ id: FilterMode; label: string; count: number }> = [
		{ id: "all", label: "全部", count: counts.all },
		{ id: "image", label: "图片", count: counts.image },
		{ id: "video", label: "视频", count: counts.video },
  ];
  return (
    <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
      <div className="relative flex-1">
        <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
        <Input
          value={search}
          onChange={(e) => onSearch(e.target.value)}
			placeholder="搜索提示词、模型或作品 ID…"
          className="pl-9"
        />
      </div>
      <div className="flex items-center gap-1 rounded-md border border-border bg-card p-1">
        {tabs.map((t) => (
          <button
            key={t.id}
            onClick={() => onFilter(t.id)}
            className={cn(
              "inline-flex items-center gap-1.5 rounded px-3 py-1 text-xs transition",
              filter === t.id
                ? "bg-primary text-primary-foreground"
                : "text-muted-foreground hover:text-foreground"
            )}
          >
            <span>{t.label}</span>
            <span className="text-[10px] opacity-70">{t.count}</span>
          </button>
        ))}
      </div>
		<Button size="sm" variant="outline" onClick={onRefresh} disabled={syncing}>
			<RefreshCw className={cn("h-3.5 w-3.5", syncing && "animate-spin")} />
			{syncing ? "正在同步全部账号…" : "刷新并同步"}
		</Button>
    </div>
  );
}

function LogCard({
  log,
  onReplay,
  onPreview,
	onSave,
	onDelete,
	saving,
	deleting,
}: {
  log: GenerationLog;
  onReplay: () => void;
  onPreview: (url: string) => void;
	onSave: () => void;
	onDelete: () => void;
	saving: boolean;
	deleting: boolean;
}) {
  const created = new Date(log.createdAt * 1000).toLocaleString();
  const isVideo = isVideoLog(log);
  return (
    <Card className="transition hover:border-primary/30">
      <div className="flex flex-col gap-4 p-4 md:flex-row">
        <div className="flex w-full shrink-0 flex-col gap-2 md:w-56">
          <ThumbnailGrid log={log} onPreview={onPreview} />
        </div>

        <div className="min-w-0 flex-1">
          <div className="mb-1 flex flex-wrap items-center gap-2">
            <Badge tone={log.provider === "adobe" ? "info" : "neutral"}>
              {log.provider === "adobe" ? "Adobe" : "Leonardo"}
            </Badge>
            <Badge tone={isVideo ? "info" : "neutral"}>
				{isVideo ? "视频" : "图片"}
            </Badge>
            <Badge tone={log.status === "success" ? "success" : "danger"}>
				{log.status === "success" ? "已完成" : log.status}
            </Badge>
			{log.savedFiles.length > 0 && (
				<Badge tone="success">
					<CheckCircle2 className="mr-1 inline h-3 w-3" />
					已保存 {log.savedFiles.length}
				</Badge>
			)}
            {log.aspectRatio && <Badge tone="neutral">{log.aspectRatio}</Badge>}
            <span className="text-[11px] text-muted-foreground">
              <Clock className="mr-1 inline h-3 w-3" />
              {created}
            </span>
          </div>
          <p className="line-clamp-3 text-sm">{log.prompt}</p>
          <p className="mt-1 truncate text-[11px] text-muted-foreground">
            <Wand2 className="mr-1 inline h-3 w-3" />
            {log.modelID}
			{" · "}{log.provider === "adobe" ? `账号 ${log.providerAccountID ? log.providerAccountID.slice(0, 12) : "未记录"}` : `账号 #${log.usedCookieID}`}
			{" · "}作品 {log.providerGenerationID.slice(0, 8)}
          </p>
          {log.errorMessage && (
            <p className="mt-1 text-[11px] text-red-300">{log.errorMessage}</p>
          )}
          <div className="mt-3 flex flex-wrap items-center gap-2">
            <Button size="sm" variant="outline" onClick={onReplay}>
              <Repeat className="h-3.5 w-3.5" />
				再次使用
            </Button>
			<Button size="sm" variant="outline" onClick={onSave} disabled={saving || deleting}>
				{saving ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Download className="h-3.5 w-3.5" />}
				{saving ? "保存中…" : "保存"}
			</Button>
			<Button size="sm" variant="outline" onClick={onDelete} disabled={saving || deleting} className="text-red-300 hover:text-red-200">
				{deleting ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Trash2 className="h-3.5 w-3.5" />}
				{deleting ? "删除中…" : "删除"}
			</Button>
          </div>
        </div>
      </div>
    </Card>
  );
}

function ThumbnailGrid({
  log,
  onPreview,
}: {
  log: GenerationLog;
  onPreview: (url: string) => void;
}) {
  const items = log.imageURLs.slice(0, 4);
  if (items.length === 0) {
    return (
      <div className="flex h-32 w-full items-center justify-center rounded bg-accent text-muted-foreground">
        <ImageIcon className="h-6 w-6" />
      </div>
    );
  }
  return (
    <div className="grid w-full grid-cols-2 gap-1">
      {items.map((u, i) =>
        /\.(mp4|webm|mov)(?:$|\?)/i.test(u) ? (
          <InlineVideoTile key={i} url={u} onPreview={() => onPreview(u)} />
        ) : (
          <button
            key={i}
            type="button"
            onClick={() => onPreview(u)}
            className="overflow-hidden rounded transition hover:ring-2 hover:ring-primary"
			aria-label="预览图片"
          >
            <img
              src={u}
              alt=""
              className="aspect-square w-full object-cover"
              loading="lazy"
            />
          </button>
        )
      )}
    </div>
  );
}

// Inline video tile with click-to-play. Mute by default so multiple cards
// can autoplay without audio chaos. Hover reveals overlay buttons.
function InlineVideoTile({
  url,
  onPreview,
}: {
  url: string;
  onPreview: () => void;
}) {
  const [playing, setPlaying] = useState(false);
  const videoRef = useMemo(() => ({ current: null as HTMLVideoElement | null }), []);

  const togglePlay = (e: React.MouseEvent) => {
    e.stopPropagation();
    const v = videoRef.current;
    if (!v) return;
    if (v.paused) {
      void v.play();
      setPlaying(true);
    } else {
      v.pause();
      setPlaying(false);
    }
  };

  return (
    <div className="group relative overflow-hidden rounded">
      <video
        ref={(el) => (videoRef.current = el)}
        src={url}
        className="aspect-square w-full object-cover"
        muted
        loop
        playsInline
        preload="metadata"
        onPlay={() => setPlaying(true)}
        onPause={() => setPlaying(false)}
      />
      <div className="absolute inset-0 flex items-center justify-center gap-1 bg-black/30 opacity-0 transition group-hover:opacity-100">
        <button
          type="button"
          onClick={togglePlay}
          className="rounded-full bg-background/90 p-2 text-foreground shadow-lg transition hover:scale-105"
		  aria-label={playing ? "暂停" : "播放"}
        >
          {playing ? (
            <Pause className="h-3.5 w-3.5" />
          ) : (
            <Play className="h-3.5 w-3.5" />
          )}
        </button>
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation();
            onPreview();
          }}
          className="rounded-full bg-background/90 p-2 text-foreground shadow-lg transition hover:scale-105"
		  aria-label="全屏预览"
        >
          <Maximize2 className="h-3.5 w-3.5" />
        </button>
      </div>
    </div>
  );
}

function LibrarySkeleton() {
  return (
    <div className="h-full overflow-y-auto">
      <div className="space-y-3 p-6">
        <Skeleton className="h-9 w-full max-w-sm" />
        {[0, 1, 2].map((i) => (
          <Card key={i}>
            <div className="flex gap-4 p-4">
              <Skeleton className="h-32 w-32 shrink-0 rounded" />
              <div className="flex flex-1 flex-col gap-2">
                <Skeleton className="h-4 w-24" />
                <Skeleton className="h-3 w-full" />
                <Skeleton className="h-3 w-3/4" />
                <Skeleton className="mt-auto h-3 w-1/2" />
              </div>
            </div>
          </Card>
        ))}
      </div>
    </div>
  );
}

function EmptyLibrary() {
  return (
    <div className="flex flex-col items-center justify-center gap-2 p-10 text-center">
      <div className="flex h-14 w-14 items-center justify-center rounded-2xl bg-accent text-accent-foreground">
        <LibraryIcon className="h-7 w-7" />
      </div>
		<h2 className="text-lg font-semibold">素材库暂无内容</h2>
      <p className="max-w-md text-sm text-muted-foreground">
		点击“刷新并同步”可从账号池中的全部 Leonardo 账号同步网页端作品。
      </p>
    </div>
  );
}
