import { useEffect, useMemo, useState } from "react";
import { Sidebar, type NavId } from "@/components/sidebar";
import { Topbar } from "@/components/topbar";
import { AboutDialog } from "@/components/about-dialog";
import { BalancePill } from "@/components/balance-pill";
import { GenerateImagePage } from "@/pages/generate-image";
import { GenerateVideoPage } from "@/pages/generate-video";
import { LibraryPage } from "@/pages/library";
import { SettingsPage } from "@/pages/settings";
import { CanvasPage } from "@/pages/canvas";
import { AdobePage } from "@/pages/adobe";
import { VideoClawPage } from "@/pages/videoclaw";
import { LocalMiniDramaPage } from "@/pages/localminidrama";
import { ProvidersPage } from "@/pages/providers";

// Pages that should show the balance pill in the topbar — only the ones
// that consume credits.
const BALANCE_PAGES = new Set<NavId>(["image", "video", "canvas"]);

const PAGE_META: Record<NavId, { title: string; render: (ctx: PageContext) => JSX.Element }> = {
  image: {
    title: "图片生成",
    render: () => <GenerateImagePage />,
  },
  video: {
    title: "视频生成",
    render: () => <GenerateVideoPage />,
  },
  videoclaw: {
    title: "VideoClaw AI 导演",
    render: () => <VideoClawPage />,
  },
  localminidrama: {
    title: "本地短剧",
    render: () => <LocalMiniDramaPage />,
  },
  canvas: {
    title: "开源无限画布",
    render: () => <CanvasPage />,
  },
  library: {
    title: "素材库",
    render: (ctx) => <LibraryPage onNavigate={ctx.navigate} />,
  },
  adobe: {
    title: "Adobe Firefly",
    render: () => <AdobePage />,
  },
  providers: {
    title: "接口管理",
    render: () => <ProvidersPage />,
  },
  settings: {
    title: "设置",
    render: () => <SettingsPage />,
  },
};

type PageContext = {
  navigate: (id: NavId) => void;
};

const STORAGE_NAV = "anan-video-toolbox-nav";
const STORAGE_COLLAPSED = "anan-video-toolbox-collapsed";

export default function App() {
  const [page, setPage] = useState<NavId>(() => {
    const stored = localStorage.getItem(STORAGE_NAV) as NavId | null;
    return stored && stored in PAGE_META ? stored : "image";
  });
	// The canvas runs long-lived generation polling inside an iframe. Once the
	// user opens it, keep that iframe mounted while navigating to other desktop
	// pages so switching to the material library cannot abort an active job.
	const [canvasMounted, setCanvasMounted] = useState(page === "canvas");
	const [videoClawMounted, setVideoClawMounted] = useState(page === "videoclaw");
	const [localMiniDramaMounted, setLocalMiniDramaMounted] = useState(page === "localminidrama");
  const [collapsed, setCollapsed] = useState<boolean>(() => {
    return localStorage.getItem(STORAGE_COLLAPSED) === "1";
  });
  const [aboutOpen, setAboutOpen] = useState(false);

  useEffect(() => {
    localStorage.setItem(STORAGE_NAV, page);
  }, [page]);
	useEffect(() => {
		if (page === "canvas") setCanvasMounted(true);
	}, [page]);
	useEffect(() => {
		if (page === "videoclaw") setVideoClawMounted(true);
	}, [page]);
	useEffect(() => {
		if (page === "localminidrama") setLocalMiniDramaMounted(true);
	}, [page]);
  useEffect(() => {
    localStorage.setItem(STORAGE_COLLAPSED, collapsed ? "1" : "0");
  }, [collapsed]);

  const meta = useMemo(() => PAGE_META[page], [page]);

  return (
    <div className="flex h-screen w-screen overflow-hidden bg-background text-foreground">
      <Sidebar
        active={page}
        onChange={setPage}
        collapsed={collapsed}
        onToggleCollapsed={() => setCollapsed((v) => !v)}
        onAbout={() => setAboutOpen(true)}
      />
      <div className="flex flex-1 flex-col overflow-hidden">
        <Topbar
          title={meta.title}
          rightSlot={BALANCE_PAGES.has(page) ? <BalancePill /> : undefined}
        />
		<main className="relative min-h-0 flex-1 overflow-hidden bg-background">
		  {canvasMounted && (
			<section
				aria-hidden={page !== "canvas"}
				className={`absolute inset-0 ${page === "canvas" ? "visible z-10 animate-fade-in" : "invisible pointer-events-none z-0"}`}
			>
			  <CanvasPage />
			</section>
		  )}
		  {videoClawMounted && (
			<section
				aria-hidden={page !== "videoclaw"}
				className={`absolute inset-0 ${page === "videoclaw" ? "visible z-10 animate-fade-in" : "invisible pointer-events-none z-0"}`}
			>
			  <VideoClawPage />
			</section>
		  )}
		  {localMiniDramaMounted && (
			<section
				aria-hidden={page !== "localminidrama"}
				className={`absolute inset-0 ${page === "localminidrama" ? "visible z-10 animate-fade-in" : "invisible pointer-events-none z-0"}`}
			>
			  <LocalMiniDramaPage />
			</section>
		  )}
		  {page !== "canvas" && page !== "videoclaw" && page !== "localminidrama" && (
			<section key={page} className="h-full animate-fade-in overflow-hidden">
			  {meta.render({ navigate: setPage })}
			</section>
		  )}
        </main>
      </div>
      <AboutDialog open={aboutOpen} onClose={() => setAboutOpen(false)} />
    </div>
  );
}
