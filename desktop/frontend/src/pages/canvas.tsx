import { useState } from "react";
import { ExternalLink, RefreshCw } from "lucide-react";

import { Button } from "@/components/ui/button";
import { api } from "@/lib/api";

const INFINITE_CANVAS_URL = "http://127.0.0.1:8001/infinite-canvas/#/canvas";

/**
 * The upstream infinite-canvas project is kept as a separately served AGPL
 * frontend and embedded here rather than copied into the desktop frontend.
 * This keeps its license and update path intact while sharing our local API.
 */
export function CanvasPage() {
  const [reloadKey, setReloadKey] = useState(0);

  return (
    <div className="flex h-full flex-col overflow-hidden bg-background">
      <div className="flex shrink-0 items-center justify-between gap-3 border-b border-border px-4 py-2">
        <div>
          <div className="text-sm font-semibold">开源无限画布</div>
          <div className="text-[11px] text-muted-foreground">
            basketikun/infinite-canvas · 已连接 anan视频工具箱本地 API
          </div>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" onClick={() => setReloadKey((value) => value + 1)}>
            <RefreshCw className="h-3.5 w-3.5" />刷新
          </Button>
          <Button variant="outline" size="sm" onClick={() => void api.openURL(INFINITE_CANVAS_URL)}>
            <ExternalLink className="h-3.5 w-3.5" />浏览器打开
          </Button>
        </div>
      </div>
      <iframe
        key={reloadKey}
        title="开源无限画布"
        src={INFINITE_CANVAS_URL}
        allow="clipboard-read; clipboard-write"
        className="min-h-0 flex-1 border-0 bg-background"
      />
    </div>
  );
}
