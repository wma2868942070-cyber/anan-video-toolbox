import { useCallback, useEffect, useState } from "react";
import { Activity, ExternalLink, KeyRound, Loader2, Save, Server } from "lucide-react";
import { Card, CardContent, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { useToast } from "@/components/ui/toast";
import { api, type ProviderConfig, type ProviderConnectionResult } from "@/lib/api";

type Draft = ProviderConfig & { apiKey: string };

const DEFAULT_LEO_ADMIN_URL = "http://127.0.0.1:8787/";

function adminURLFor(baseURL: string): string {
  try {
    const url = new URL(baseURL || DEFAULT_LEO_ADMIN_URL);
    url.pathname = "/";
    url.search = "";
    url.hash = "";
    return url.toString();
  } catch {
    return DEFAULT_LEO_ADMIN_URL;
  }
}

export function ProvidersPage() {
  const { showError } = useToast();
  const [drafts, setDrafts] = useState<Draft[] | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [checks, setChecks] = useState<Record<string, ProviderConnectionResult>>({});

  const reload = useCallback(async () => {
    try {
      const configs = await api.listProviderConfigs();
      setDrafts(configs.map((item) => ({ ...item, apiKey: "" })));
    } catch (err) {
      showError((err as Error).message);
    }
  }, [showError]);

  useEffect(() => {
    void reload();
  }, [reload]);

  const update = (provider: string, patch: Partial<Draft>) => {
    setDrafts((current) =>
      current?.map((item) =>
        item.provider === provider ? { ...item, ...patch } : item
      ) ?? null
    );
  };

  const save = async (draft: Draft) => {
    setBusy(`${draft.provider}:save`);
    try {
      const saved = await api.saveProviderConfig({
        provider: draft.provider,
        baseURL: draft.baseURL,
        apiKey: draft.apiKey,
        enabled: draft.enabled,
      });
      update(draft.provider, { ...saved, apiKey: "" });
    } catch (err) {
      showError((err as Error).message);
    } finally {
      setBusy(null);
    }
  };

  const test = async (draft: Draft) => {
    setBusy(`${draft.provider}:test`);
    try {
      const result = await api.testProviderConnection(draft.provider);
      setChecks((current) => ({ ...current, [draft.provider]: result }));
      if (!result.reachable) showError(result.message);
    } catch (err) {
      showError((err as Error).message);
    } finally {
      setBusy(null);
    }
  };

  if (drafts === null) {
    return (
      <div className="space-y-6 p-6">
        {[0, 1].map((item) => (
          <Card key={item}>
            <div className="space-y-3 p-5">
              <Skeleton className="h-5 w-40" />
              <Skeleton className="h-9 w-full" />
              <Skeleton className="h-9 w-full" />
            </div>
          </Card>
        ))}
      </div>
    );
  }

  return (
    <div className="h-full overflow-y-auto">
      <div className="space-y-6 p-6">
        <Card>
          <CardContent className="space-y-2 p-5">
            <CardTitle className="text-base">接口管理</CardTitle>
        <p className="text-xs leading-5 text-muted-foreground">
          账号池和可编辑模型已从桌面入口移除。API Key 只保存在本机 SQLite，界面只显示掩码。
        </p>
          </CardContent>
        </Card>
        {drafts.map((draft) => (
          <ProviderCard
            key={draft.provider}
            draft={draft}
            check={checks[draft.provider]}
            saving={busy === `${draft.provider}:save`}
            testing={busy === `${draft.provider}:test`}
            onChange={(patch) => update(draft.provider, patch)}
            onSave={() => void save(draft)}
            onTest={() => void test(draft)}
          />
        ))}
      </div>
    </div>
  );
}

function ProviderCard({
  draft,
  check,
  saving,
  testing,
  onChange,
  onSave,
  onTest,
}: {
  draft: Draft;
  check?: ProviderConnectionResult;
  saving: boolean;
  testing: boolean;
  onChange: (patch: Partial<Draft>) => void;
  onSave: () => void;
  onTest: () => void;
}) {
  const isLeo = draft.provider === "leo";
  return (
    <Card>
      <div className="flex items-start justify-between gap-4 p-5 pb-3">
        <div className="flex items-center gap-2">
          <Server className="h-4 w-4 text-primary" />
          <div>
            <CardTitle className="text-base">{draft.name}</CardTitle>
            <p className="mt-1 text-[11px] text-muted-foreground">
              {isLeo ? "图片/视频生成 · Leo2API" : "图片/视频生成 · Adobe2API"}
            </p>
          </div>
        </div>
        <Switch checked={draft.enabled} onCheckedChange={(enabled) => onChange({ enabled })} />
      </div>
      <CardContent className="space-y-4 pt-0">
        <label className="block space-y-1.5">
          <span className="text-xs font-medium">Base URL</span>
          <Input
            value={draft.baseURL}
            onChange={(e) => onChange({ baseURL: e.target.value })}
            placeholder={isLeo ? "http://127.0.0.1:8787" : "http://127.0.0.1:6001"}
            className="font-mono text-xs"
          />
        </label>
        <label className="block space-y-1.5">
          <span className="flex items-center gap-1 text-xs font-medium">
            <KeyRound className="h-3.5 w-3.5" /> API Key
          </span>
          <Input
            type="password"
            value={draft.apiKey}
            onChange={(e) => onChange({ apiKey: e.target.value })}
            placeholder={draft.apiKeyConfigured ? draft.apiKeyMasked : "粘贴 API Key"}
            className="font-mono text-xs"
          />
          <p className="text-[10px] text-muted-foreground">
            留空表示保留现有 Key；如需更换，直接粘贴新 Key。
          </p>
        </label>
        <div className="flex flex-wrap items-center gap-2">
          <Button onClick={onSave} disabled={saving || testing}>
            {saving ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
            保存
          </Button>
          <Button variant="outline" onClick={onTest} disabled={saving || testing}>
            {testing ? <Loader2 className="h-4 w-4 animate-spin" /> : <Activity className="h-4 w-4" />}
            连接测试
          </Button>
          {check ? (
            <Badge tone={check.reachable ? "success" : "danger"}>
              {check.reachable ? `正常 · ${check.modelCount} 个模型` : check.message}
            </Badge>
          ) : null}
          {isLeo ? (
            <Button
              variant="outline"
              onClick={() => void api.openURL(adminURLFor(draft.baseURL))}
              disabled={saving || testing}
              title="在系统默认浏览器中单独打开 Leo2API 管理页面"
            >
              <ExternalLink className="h-4 w-4" />
              打开 Leo 管理页
            </Button>
          ) : null}
        </div>
        {isLeo ? (
          <p className="text-[10px] leading-4 text-muted-foreground">
            管理页会在工具箱之外的独立浏览器窗口中打开，用于导入 Leo Cookie 或 Session Token。
          </p>
        ) : null}
        <div className="flex flex-wrap gap-1.5">
          {draft.capabilities.map((capability) => (
            <Badge key={capability} tone="neutral">{capability}</Badge>
          ))}
        </div>
      </CardContent>
    </Card>
  );
}
