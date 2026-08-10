/* Simplified Chinese localisation layer for the upstream UI. */

const DICTIONARY: Record<string, string> = {
  "Generate Image": "图片生成",
  "Generate Video": "视频生成",
  Library: "作品库",
  Cookies: "账号池",
  Models: "模型管理",
  Settings: "设置",
  Workspace: "创作",
  Manage: "管理",
  About: "关于",
  LeoStudio: "anan视频工具箱",
  Author: "作者",
  License: "许可证",
  "Built with": "技术栈",
  Repository: "项目仓库",
  Close: "关闭",
  Light: "浅色",
  Dark: "深色",
  System: "跟随系统",
  "Light theme": "浅色主题",
  "Dark theme": "深色主题",
  "System theme": "跟随系统主题",
  credits: "积分",
  "Active balance": "可用积分",
  "Ready accounts": "可用账号",
  Ready: "可用",
  Depleted: "积分耗尽",
  Disabled: "已停用",
  "No cookies": "暂未导入账号",
  "Add cookie": "添加账号",
  "Edit cookie": "编辑账号",
  "Update cookie": "更新账号",
  "Delete cookie": "删除账号",
  "Refresh all": "刷新全部",
  Refresh: "刷新",
  "Cookie pool": "账号池",
  "Full cookie string": "完整 Cookie 字符串",
  "Compose image": "配置图片任务",
  "Compose video": "配置视频任务",
  Compose: "创作参数",
  Prompt: "提示词",
  "A cinematic shot of...": "电影感画面，例如……",
  "Cinematic footage of...": "电影感视频，例如……",
  Model: "模型",
  "Aspect ratio": "画面比例",
  Resolution: "分辨率",
  Duration: "时长",
  Quantity: "生成数量",
  "Native audio": "原生音频",
  "Start frame": "起始帧",
  "End frame": "结束帧",
  Reference: "参考图",
  "Reference image": "参考图片",
  "Remote URL": "远程地址",
  "Choose file": "选择文件",
  "Paste URL": "粘贴地址",
  Use: "使用",
  Cancel: "取消",
  "Drag image here": "将图片拖到这里",
  "Uploading…": "正在上传…",
  "Remove reference": "移除参考图",
  "Add to queue": "加入队列",
  Drafts: "草稿",
  Submit: "提交",
  Image: "图片",
  Video: "视频",
  All: "全部",
  success: "成功",
  "Use prompt": "使用提示词",
  Play: "播放",
  Pause: "暂停",
  Result: "生成结果",
  "Ready to generate": "等待开始生成",
  "Generated images appear here.": "生成的图片将在这里显示。",
  "Generated video appears here.": "生成的视频将在这里显示。",
  "No image models": "暂无图片模型",
  "No video model registered": "暂无视频模型",
  "No model": "暂无模型",
  "Model UUID": "模型 UUID",
  "Model UUID required": "请输入模型 UUID",
  "Add model": "添加模型",
  "Sync models": "同步模型",
  Sync: "同步",
  "Sync from Leonardo": "从 Leonardo 同步",
  Name: "名称",
  Add: "添加",
  "Model added": "模型已添加",
  Default: "默认",
  "Default aspect ratio": "默认画面比例",
  "Default image model": "默认图片模型",
  "Auto-save": "自动保存",
  "Save outputs to disk": "将生成结果保存到本地",
  Save: "保存",
  "Saving…": "正在保存…",
  "Choose folder": "选择文件夹",
  "Open folder": "打开文件夹",
  Search: "搜索",
  "Search prompt, model, gen id…": "搜索提示词、模型或任务 ID…",
  "Tidak ada hasil": "暂无结果",
  "Coba ganti filter atau bersihkan kotak pencarian.": "请更换筛选条件或清空搜索框。",
  Download: "下载",
  "Open fullscreen": "全屏查看",
  "Open fullscreen preview": "打开全屏预览",
  "Preview image": "预览图片",
  "Preview result": "预览结果",
  "Duplicate draft": "复制草稿",
  "Remove draft": "移除草稿",
  "Cancel job": "取消任务",
  "Retry job": "重试任务",
  "Clear finished": "清除已完成任务",
  pending: "等待中",
  running: "生成中",
  completed: "已完成",
  failed: "失败",
  canceled: "已取消",
  audio: "音频",
  "ref image": "参考图",
  "Antrian kosong": "队列为空",
  "Job yang di-submit tampil di sini.": "提交后的任务将在这里显示。",
  "Belum ada draft. Susun request lalu klik \"Add to queue\".": "暂无草稿，请配置任务后点击“加入队列”。",
  "Susun beberapa request di kiri, lalu Submit untuk menjalankannya di latar belakang.": "在左侧配置多个任务，然后点击提交以在后台运行。",
  "Prompt tidak boleh kosong.": "提示词不能为空。",
  "Pilih model dulu.": "请先选择模型。",
  "Paste full cookie string dulu sebelum simpan.": "请先粘贴完整 Cookie 字符串。",
  "Paste cookie baru dulu.": "请先粘贴新的 Cookie。",
  "URL kosong.": "请输入图片地址。",
  "Failed to read file": "读取文件失败",
  "(optional)": "（可选）",
  "jpg / png / webp, max 1 file": "jpg / png / webp，最多 1 个文件",
  "Collapse sidebar": "收起侧边栏",
  "Expand sidebar": "展开侧边栏",
};

const PATTERNS: Array<[RegExp, string]> = [
	[/\(optional\)/gi, "（可选）"],
  [/(\d+) ready\s*·\s*(\d+) depleted/gi, "$1 个可用 · $2 个积分耗尽"],
  [/(\d+) running\s*·\s*(\d+) pending\s*·\s*(\d+) done\s*·\s*(\d+) failed/gi, "$1 个生成中 · $2 个等待中 · $3 个已完成 · $4 个失败"],
  [/Cookie #(\d+)/gi, "账号 #$1"],
  [/cookie #(\d+)/gi, "账号 #$1"],
  [/(\d+) job ditambahkan ke antrian/gi, "已添加 $1 个任务到队列"],
  [/Duration\s*·\s*(\d+)s/gi, "时长 · $1 秒"],
  [/Submit\s*\((\d+)\)/gi, "提交（$1）"],
  [/Gagal load /gi, "加载失败："],
  [/Gagal memuat /gi, "加载失败："],
  [/Upload gagal/gi, "上传失败"],
  [/Download gagal/gi, "下载失败"],
  [/Refresh gagal/gi, "刷新失败"],
  [/Generate sukses/gi, "生成成功"],
  [/Video selesai/gi, "视频生成完成"],
  [/Tersimpan/gi, "已保存"],
  [/Saved/gi, "已保存"],
  [/Uploaded/gi, "已上传"],
];

function translate(raw: string): string {
  const leading = raw.match(/^\s*/)?.[0] ?? "";
  const trailing = raw.match(/\s*$/)?.[0] ?? "";
  const value = raw.trim();
  if (!value) return raw;
  let translated = DICTIONARY[value] ?? value;
  for (const [pattern, replacement] of PATTERNS) translated = translated.replace(pattern, replacement);
  return translated === value ? raw : `${leading}${translated}${trailing}`;
}

function translateElement(element: Element): void {
  for (const attr of ["title", "aria-label", "placeholder"]) {
    const current = element.getAttribute(attr);
    if (!current) continue;
    const next = translate(current);
    if (next !== current) element.setAttribute(attr, next);
  }
}

function translateTree(root: Node): void {
  if (root.nodeType === Node.TEXT_NODE) {
    const parent = root.parentElement;
    if (parent && !["SCRIPT", "STYLE"].includes(parent.tagName)) {
      const current = root.nodeValue ?? "";
      const next = translate(current);
      if (next !== current) root.nodeValue = next;
    }
    return;
  }
  if (!(root instanceof Element)) return;
  translateElement(root);
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_ELEMENT | NodeFilter.SHOW_TEXT);
  let node: Node | null;
  while ((node = walker.nextNode())) {
    if (node.nodeType === Node.TEXT_NODE) {
      const parent = node.parentElement;
      if (parent && !["SCRIPT", "STYLE"].includes(parent.tagName)) {
        const current = node.nodeValue ?? "";
        const next = translate(current);
        if (next !== current) node.nodeValue = next;
      }
    } else if (node instanceof Element) {
      translateElement(node);
    }
  }
}

export function installChineseLocalization(): void {
  document.documentElement.lang = "zh-CN";
  document.title = "anan视频工具箱";
  const nativeConfirm = window.confirm.bind(window);
  window.confirm = (message?: string) => nativeConfirm(translate(message ?? ""));
  const observer = new MutationObserver((records) => {
    for (const record of records) {
      if (record.type === "attributes") translateElement(record.target as Element);
      if (record.type === "characterData") translateTree(record.target);
      for (const node of record.addedNodes) translateTree(node);
    }
  });
  observer.observe(document.documentElement, {
    childList: true,
    subtree: true,
    characterData: true,
    attributes: true,
    attributeFilter: ["title", "aria-label", "placeholder"],
  });
  queueMicrotask(() => translateTree(document.body));
}
