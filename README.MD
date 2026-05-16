# 派达 Pdai

派达（Pdai）是一个面向服务器运维和应用部署的 Web 管理面板，内置 Caddy 反向代理管理、Podman/Docker 容器管理、项目部署、数据库实例、文件管理、备份、监控、定时任务、进程守护、AI 助手和 MCP 集成等能力。

默认页面标题为 `派达 - PDai.TOP`，可在系统设置中修改站点名称。

## 主要功能

- 反向代理：管理 Caddy 站点、域名、上游、TLS、WebSocket、压缩和自定义配置。
- 容器管理：Podman-first，兼容 Docker CLI/Socket，支持容器、镜像、网络、卷和 Compose Stack。
- 项目部署：支持 Git 项目部署，自动识别 Node.js、Go、PHP、Python、Dockerfile、静态站点等项目类型。
- 应用商店：通过 Docker Compose 一键安装应用，并支持项目模板市场。
- 数据库管理：创建和管理 MySQL、PostgreSQL、MariaDB、Redis 实例，提供连接信息和查询工具。
- PHP 管理：管理 PHP-FPM、FrankenPHP 运行时，创建 PHP 网站并安装扩展。
- 文件管理：浏览、上传、编辑、压缩、解压文件，并提供 Web 终端。
- 备份恢复：基于 Kopia 备份面板数据、Docker 卷和数据库。
- 系统监控：查看 CPU、内存、磁盘、网络、容器指标，并配置告警规则。
- 防火墙：通过 Web UI 管理 iptables 端口规则。
- 定时任务：管理 Cron 任务，查看执行日志。
- 进程守护：管理长期运行进程，支持自启动、自动重启和日志查看。
- AI 助手：支持故障诊断、Dockerfile/Compose 生成、部署辅助和安全确认。
- MCP Server：为 Cursor、Windsurf、Claude Code 等 AI IDE 提供工具调用接口。

## 技术栈

- 后端：Go、Gin、GORM、SQLite
- 前端：React、Vite、Radix UI、Zustand、i18next
- 代理：Caddy
- 容器：Podman / Docker

## 运行环境

推荐环境：

- Linux amd64 / arm64
- Go 1.26+
- Node.js 20+
- Caddy 2.x
- Podman 或 Docker

已适配：

- systemd 系统：Debian、Ubuntu、Rocky、AlmaLinux、CentOS Stream、Fedora 等
- OpenRC / Alpine：支持基础运行、Podman 安装、OpenRC 服务管理、iptables 安装等

注意：部分第三方工具是否可用取决于系统软件源，例如 Alpine 上的 Kopia 包需要仓库提供对应包。

## 本地开发

安装前端依赖：

```bash
cd web
npm install
```

启动前端开发服务器：

```bash
cd web
npm run dev
```

启动后端：

```bash
go run .
```

默认监听端口：

```text
39921
```

首次启动会自动创建 `config.ini` 和数据目录。

## 构建生产版本

先构建前端：

```bash
cd web
npm run build
```

再编译后端嵌入版：

```bash
cd ..
go build -trimpath -ldflags "-s -w" -o dist/pdai .
```

交叉编译 Linux amd64：

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o dist/pdai-linux-amd64 .
```

Windows PowerShell 示例：

```powershell
$env:GOOS="linux"
$env:GOARCH="amd64"
$env:CGO_ENABLED="0"
go build -trimpath -ldflags "-s -w" -o dist\pdai-linux-amd64 .
```

## 配置说明

常用环境变量：

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `PDAI_PORT` | `39921` | 面板监听端口 |
| `PDAI_DATA_DIR` | `./data` | 数据目录 |
| `PDAI_DB_PATH` | `./data/Pdai.db` | SQLite 数据库路径 |
| `PDAI_JWT_SECRET` | 自动生成 | JWT 签名密钥 |
| `PDAI_CADDY_BIN` | `caddy` | Caddy 可执行文件路径 |
| `PDAI_CADDYFILE_PATH` | `./data/Caddyfile` | Caddyfile 路径 |
| `PDAI_LOG_DIR` | `./data/logs` | 日志目录 |
| `PDAI_ADMIN_API` | `http://localhost:2019` | Caddy Admin API |

也可以通过 `config.ini` 设置这些变量。

## 系统设置

系统设置包含：

- 基础设置：站点名称。设置后浏览器标题显示为 `站点名称 - 派达[pdai.top]`。
- 日志：查看应用、Caddy 和系统日志。
- DNS 提供商：配置 DNS API，用于 DNS Challenge。
- Caddyfile：查看和编辑 Caddy 配置。
- 启动脚本：配置面板启动后执行的自定义脚本。

开机自动启动面板默认关闭，用户可在系统设置中手动开启。

## Alpine / OpenRC 说明

项目已加入 Alpine 适配：

- shell 执行会优先使用 `bash`，没有 `bash` 时回退到 `/bin/sh`。
- 面板自启动支持 OpenRC，服务脚本写入 `/etc/init.d`。
- 裸机部署模式支持 OpenRC 服务管理。
- Podman 安装流程支持 `apk` 和 `rc-service`。
- 防火墙安装支持 `apk add --no-cache iptables`。
- 容器额外进程默认使用 `sh -c`，兼容 Alpine 基础镜像。

Alpine 上建议预装：

```bash
apk add --no-cache caddy podman podman-compose iptables curl tar
```

如需使用 Docker CLI 兼容入口，可安装 `podman-docker`，或让面板安装流程创建 `docker -> podman` 的兼容链接。

## 常用命令

运行测试：

```bash
go test ./...
```

重置管理员密码：

```bash
./pdai --reset-password
```

查看版本：

```bash
./pdai --version
```

## 目录结构

```text
.
├── internal/          # 后端核心模块
├── plugins/           # 插件功能模块
├── web/               # React 前端
├── frontend_embed.go  # 嵌入 web/dist 静态资源
├── main.go            # 程序入口
├── go.mod
└── go.sum
```

## 安全提示

- 面板可以直接操作服务器、容器、文件系统和部署流程，请只暴露在可信网络或反向代理认证之后。
- 生产环境务必设置强密码，并妥善保存 `PDAI_JWT_SECRET`。
- 使用 AI 助手执行操作前，请仔细检查确认内容。
- 删除文件、容器、数据库、备份等操作可能不可逆，请先做好备份。

## 许可证

本仓库基于 `web-casa/webcasa` 重构，使用 MIT License 发布。
项目维护信息：派达 `pdai.top`

- 请随仓库一并保留根目录 `LICENSE`
- 上游 `web-casa/webcasa` 的来源和版权声明已保留在 `NOTICE`
- 发布到 GitHub 时建议同时包含 `NOTICE` 和 `THIRD_PARTY_NOTICES.md`
- 第三方依赖、字体、图标和素材仍遵循各自许可证
