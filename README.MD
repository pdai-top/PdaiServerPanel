# 派达 PdaiPanel

派达 PdaiPanel 是面向 Linux 服务器的轻量 Web 管理面板，定位为“下载一个二进制文件即可运行”的服务器运维与应用管理工具，所有产生文件仅存在于二进制所在目录。

项目重点适配 Alpine、Debian、Ubuntu、OpenWrt、Rocky、AlmaLinux、Fedora 等常见发行版，并对 Alpine、Debian、OpenWrt 等运行环境做了针对性处理。

默认端口为 `39921`，首次访问会引导创建管理员账号。

## 主要功能

- **面板自更新**：面板内检测 GitHub Releases 新版本，查看更新说明，下载校验更新包，并通过临时 helper 进程延迟替换当前二进制后重启面板。
- **站点与反向代理管理**：基于 Caddy 管理反向代理、静态站点、PHP 站点、重定向、多域名绑定、WebSocket、压缩、缓存、访问控制、自定义响应头和完整 Caddy 配置。
- **HTTPS 与证书管理**：支持自动 HTTPS、自定义证书、证书上传、证书删除、证书到期信息读取，以及 DNS API 凭据管理。
- **DNS 提供商管理**：支持 Cloudflare、阿里云 DNS、腾讯云 DNS、AWS Route 53 等 DNS 凭据配置，用于 DNS 验证和证书签发。
- **Caddy 管理**：查看 Caddy 状态、启动、停止、重载、重启、查看和编辑 Caddyfile、格式化与校验配置。
- **容器管理**：支持 Podman / Docker 环境检测、容器列表、创建容器、启动、停止、重启、删除、日志、实时资源统计、端口、卷、环境变量和网络管理。
- **镜像管理**：支持镜像列表、拉取镜像、搜索镜像、删除镜像、清理未使用镜像。
- **容器运行时设置**：支持 Docker / Podman 连接状态、镜像源、daemon 配置、Podman 切换和运行时安装引导。
- **应用商店**：支持应用列表、分类、来源管理、同步应用源、安装应用、已安装应用管理、应用域名更新、启动、停止、更新和卸载。
- **模板市场**：内置常见站点模板，支持通过模板快速创建反向代理、静态站点、PHP-FPM、WebSocket 应用等站点配置。
- **项目部署**：支持从 Git 仓库创建项目、自动识别框架、配置安装 / 构建 / 启动命令、环境变量、端口、健康检查、自动部署和预览相关配置。
- **数据库管理**：支持 MySQL、MariaDB、PostgreSQL、Redis 实例管理，可管理本地容器实例和远程数据库实例。
- **数据库操作**：支持数据库、用户、权限、连接信息、状态查看，以及 SQL 查询入口。
- **SQLite 浏览器**：提供 SQLite 数据库浏览和查询能力。
- **文件管理器**：支持服务器文件浏览、上传、下载、删除、重命名、移动、复制、在线编辑、压缩和解压。
- **Web 终端**：提供浏览器内终端入口，方便直接处理服务器运维任务。
- **备份管理**：支持本地、S3、WebDAV、SFTP 等备份目标，按范围选择面板、Docker / Podman、数据库等内容进行备份。
- **系统监控**：展示 CPU、内存、磁盘、网络等服务器运行指标，支持监控告警规则管理。
- **防火墙管理**：通过 Web UI 管理 iptables 规则，支持端口规则和常见服务规则。
- **定时任务**：支持创建、编辑、启停、手动触发 cron 任务，并查看任务执行日志。
- **进程守护**：支持长期运行进程管理、自动重启和日志查看。
- **AI 助手**：支持在面板内分析服务器状态、查看日志、辅助诊断部署、容器、数据库、监控和防火墙问题，并通过确认机制执行部分受控操作。
- **通知与告警**：支持通知渠道、监控告警、测试通知和告警摘要等能力。
- **插件管理**：内置 Docker、应用商店、文件管理、数据库、监控、防火墙、定时任务、进程守护、AI 助手等模块，可在面板中启用、禁用和控制侧边栏显示。
- **用户与权限**：支持管理员资料修改、角色控制和受保护 API 访问。
- **安全入口**：支持安全入口路径配置，降低默认面板入口暴露风险。
- **日志管理**：支持系统日志、访问日志、日志文件列表和日志下载。
- **单文件运行体验**：发布包内以单个 `pdai` 二进制作为主程序，首次运行自动生成默认配置文件和数据目录。

## 与上游差异

本仓库基于 `web-casa/webcasa` 重构，但当前定位、功能组合、发布方式和运行环境适配已经与上游不同。主要差异如下：

- **项目定位不同**：派达面板 主要面向资源更紧缺的服务器用户，重点是轻量、少依赖、直接运行、随时停止、随时干净移除。
- **发行版适配不同**：增加了 Alpine、Debian、Ubuntu、OpenWrt 等系统（感觉有docker的Linux应该都支持吧）。
- **更新机制不同**：新增面板内版本检测、更新说明弹窗、更新包下载校验、准备更新和临时 helper 延迟替换重启流程。
- **容器策略不同**：更强调 Podman ，去除 stack 概念，同时兼容 Docker CLI / Socket，适合无 Docker Desktop 的 Linux 服务器环境。
- **部署体验不同**：去除了 Git 项目部署、PHP、站点模板 等不常用的功能。
- **数据库能力不同**：不仅支持容器化数据库实例，也支持纳管远程 MySQL、MariaDB、PostgreSQL、Redis。
- **运维能力不同**：增加 进程守护、数据库定时备份、容器UI管理。

## 快速使用

前往 GitHub Releases 下载对应架构的 Linux 版本：

https://github.com/pdai-top/PdaiServerPanel/releases

下载后解压并运行：

```bash
tar -xzf pdai-*-linux-amd64.tar.gz
cd pdai-*-linux-amd64
chmod +x pdai
./pdai
```

或者一键下载、解压并运行最新版 Linux amd64：

```bash
tmp="$(mktemp -d)" && cd "$tmp" && url="$(curl -fsSL https://api.github.com/repos/pdai-top/PdaiServerPanel/releases/latest | grep -oE 'https://[^"]+pdai-[^"]+linux-amd64\.tar\.gz' | head -n1)" && curl -fL "$url" -o pdai.tar.gz && tar -xzf pdai.tar.gz && cd pdai-*-linux-amd64 && chmod +x pdai && ./pdai
```

运行后访问：

```text
http://服务器IP:39921
```

## 常用命令

重置管理员密码：

```bash
./pdai --reset-password
```

查看版本：

```bash
./pdai --version
```

## 配置文件模板

程序首次运行会在当前目录自动生成 `config.ini`。你也可以在运行前手动创建：

```ini
PDAI_PORT=39921
PDAI_DATA_DIR=./data
PDAI_DB_PATH=./data/Pdai.db
PDAI_JWT_SECRET=请替换为足够长的随机字符串
PDAI_CADDY_BIN=./data/caddy/caddy
PDAI_CADDYFILE_PATH=./data/caddy/Caddyfile
PDAI_LOG_DIR=./data/logs
PDAI_ADMIN_API=http://localhost:2019
GIN_MODE=release
```

常用配置说明：

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `PDAI_PORT` | `39921` | 面板监听端口 |
| `PDAI_DATA_DIR` | `./data` | 面板数据目录 |
| `PDAI_DB_PATH` | `./data/Pdai.db` | SQLite 数据库路径 |
| `PDAI_JWT_SECRET` | 自动生成 | JWT 签名密钥 |
| `PDAI_CADDY_BIN` | `./data/caddy/caddy` | Caddy 可执行文件路径 |
| `PDAI_CADDYFILE_PATH` | `./data/caddy/Caddyfile` | Caddyfile 路径 |
| `PDAI_LOG_DIR` | `./data/logs` | 日志目录 |
| `PDAI_ADMIN_API` | `http://localhost:2019` | Caddy Admin API 地址 |
| `PDAI_UPDATE_RELEASE_API` | GitHub latest release API | 面板自更新检查地址，可用于镜像或测试源 |
| `GIN_MODE` | `debug` | 建议生产环境设置为 `release` |

## 安全提示

- 面板可以操作服务器、容器、文件系统、数据库和部署流程，请只暴露在可信网络或反向代理认证之后。
- 生产环境务必设置强密码，并妥善保存 `PDAI_JWT_SECRET`。
- 使用 AI 助手执行操作前，请仔细检查确认内容。
- 删除文件、容器、数据库、备份等操作可能不可逆，请先做好备份。

## 许可证

本仓库基于 `web-casa/webcasa` 重构，使用 MIT License 发布。

项目维护信息：派达 `pdai.top`

- 请随仓库一并保留根目录 `LICENSE`
- 上游 `web-casa/webcasa` 的来源和版权声明已保留在 `NOTICE`
- 第三方依赖、字体、图标和素材仍遵循各自许可证
