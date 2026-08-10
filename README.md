# anan视频工具箱

一个面向个人非商业使用的 Leonardo 图片/视频桌面工具。它使用你本人有权使用的 Leonardo 网页会话 Cookie，提供桌面账号池、图片生成、视频生成、模型管理、素材库，以及内置无限画布和 OpenAI 兼容本地 API。

> **禁止商业使用。** 本项目自有代码采用 [PolyForm Noncommercial License 1.0.0](LICENSE)。第三方组件继续适用各自原始许可证，详见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。

## 核心功能

- 桌面账号池：添加、更新、启停、删除、刷新余额和会话状态；
- 多账号轮换：自动跳过失效或余额耗尽的账号；
- 图片生成：同步 Leonardo 图片模型并按官方名称显示；
- 视频生成：30 个 Leonardo 视频模型，支持文生视频、图生视频、参考素材、声音和动态积分消耗；
- 无限画布：图片和视频模型从本地 `/v1/models` 动态同步，显示名称与 Leonardo 对齐；
- 素材库：查看、下载和管理生成结果；
- 本地 API：供其他画布或工作流统一调用。

## 已精简功能

- 移除重复的“任务队列”页面，图片、视频和无限画布直接完成创作流程；
- 移除网页账号池及 `/admin` 管理页面，账号凭据只在桌面软件中管理；
- HTTP 服务仅保留健康检查、模型发现、图片生成和视频生成接口。

## 账号池使用

打开桌面软件中的“账号池”，粘贴以下任一种内容：

1. Leonardo 完整 Cookie；
2. 浏览器开发者工具中 `api.leonardo.ai/v1/graphql` 请求的 **Copy as cURL** 内容。

程序只提取生成请求需要的会话信息。Cookie 等同登录凭据，请勿上传、分享或提交到 Git。

默认数据目录：

```text
Windows: %APPDATA%\anan-video-toolbox\app.db
Linux/macOS: ~/.config/anan-video-toolbox/app.db
```

首次启动会尝试迁移旧的 `leostudio/app.db`；如果旧数据库正被占用，会先继续使用旧目录并在下次启动重试。

## 本地 API

默认地址：

```text
http://127.0.0.1:8001
```

主要接口：

```text
GET  /health
GET  /v1/models
POST /v1/images/generations
POST /v1/videos/generations
POST /v1/videos
GET  /v1/videos/{id}
GET  /v1/videos/{id}/content
```

环境变量使用 `ANAN_VIDEO_TOOLBOX_` 前缀，并兼容旧的 `LEOSTUDIO_` 前缀：

```powershell
$env:ANAN_VIDEO_TOOLBOX_HOST="127.0.0.1"
$env:ANAN_VIDEO_TOOLBOX_PORT="8001"
$env:ANAN_VIDEO_TOOLBOX_API_KEY="请替换为高强度随机密钥"
$env:ANAN_VIDEO_TOOLBOX_CORS_ORIGINS="*"
go run ./cmd/server
```

默认图片模型别名为 `anan-default`；旧的 `leostudio-default` 仍可调用，但不会在无限画布中重复显示。

## 开发与构建

```powershell
go test ./...

cd third_party/infinite-canvas/web
npm ci
npm run build

cd ../../../desktop/frontend
npm ci
npm run build

cd ..
wails build
```

桌面产物：

```text
desktop/build/bin/anan-video-toolbox.exe
```

## 许可证与上游

- 本项目自有代码：PolyForm Noncommercial License 1.0.0；
- 上游 LeoStudio：MIT License；
- Infinite Canvas：GNU Affero General Public License v3.0；
- 完整第三方声明：[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。

本项目与 Leonardo.Ai 无官方隶属关系。请仅使用你本人有权使用的账号，并遵守相关服务条款。