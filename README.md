# 115strm

`115strm` 是一个单端口 Web 应用，用于 115 网盘自有文件的 STRM 生成和 302 反代播放。

## 更名后的部署

服务名和构建产物使用 `115strm`；Docker 数据、日志目录继续使用 `/var/lib/panstr`、`/var/log/panstr`，兼容原部署。已部署旧版本升级时，应先备份数据，再将原配置中的数据、数据库、Cookie和日志实际路径填写到新配置，并相应调整Docker卷映射，确保继续使用原数据；不要直接用新的空目录替换旧数据目录。systemd部署改用 `deploy/115strm.service`，核对运行用户、路径和文件权限后再切换服务。

浏览器中的旧登录缓存会迁移到新键；已有Cookie加密格式保持兼容。本次本地改名没有迁移已部署服务器的数据。

## 功能范围

- 115 扫码登录，默认使用 Android 端登录，cookie 加密保存到本地。
- 支持导入 115 Cookie，避免重新扫码占用浏览器 Web 登录态。
- 支持导入 115 Open Token，适合 Emby / Infuse 纯 302 播放。
- 浏览自己的 115 网盘目录，查看并复制 pickcode。
- 从 pickcode 列表生成 STRM。
- 从指定的非根目录 CID 递归生成 STRM。
- 按 115 原始目录层级输出到 `/opt/Media`。
- 下载伴生文件，例如 NFO、海报、背景图、字幕。
- 伴生文件按 115 原始目录层级保存，支持同季目录和上级媒体目录中的 `poster.jpg`、`fanart.jpg`、`*-thumb.jpg` 等文件。
- 302 反代播放，跳转目标只允许 115 相关域名。
- 增量同步、Hash 巡检、回收站记录清理。
- Vue 3 + Element Plus 浅色管理界面。
- SQLite 本地存储。
- 推荐 Docker 部署，systemd 裸机部署作为备选。

明确不做：115 分享接收/浏览、自动接收、CloudSaver、Pansou、TMDB、Emby/Jellyfin API 集成、Telegram、WebDAV、多网盘后端。

## 默认参数

- 服务端口：`8585`
- STRM 输出目录：`/opt/Media`
- 数据目录：`/var/lib/panstr`
- 日志目录：`/var/log/panstr`
- 配置文件：Docker 使用 `deploy/config.yaml` 挂载到容器内 `/etc/115strm/config.yaml`
- 默认 115 QPS：`0.8`
- 默认 115 扫码登录端：`android`
- 配置修改后不会热生效，需要重启服务。

## 推荐部署方式：Docker

### 1. 安装 Docker

Ubuntu 22.04：

```bash
sudo apt update
sudo apt install -y docker.io docker-compose-plugin unzip
sudo systemctl enable --now docker
```

确认 Docker 可用：

```bash
docker --version
docker compose version
```

### 2. 准备目录

```bash
sudo mkdir -p /opt/115strm-src /opt/Media /var/lib/115strm /var/log/115strm
sudo chmod 755 /opt/Media /var/lib/115strm /var/log/115strm
```

### 3. 上传项目

把本项目整个目录上传到服务器，例如上传到 `/opt/115strm-src`。

如果从 Windows PowerShell 上传：

```powershell
cd E:\11111\111115
Compress-Archive -Path * -DestinationPath .\115strm.zip -Force
scp .\115strm.zip ubuntu@你的服务器IP:/tmp/115strm.zip
```

服务器上解压：

```bash
sudo rm -rf /opt/115strm-src/*
sudo unzip /tmp/115strm.zip -d /opt/115strm-src
cd /opt/115strm-src
rm -rf web/node_modules web/dist internal/web/dist bin
```

如果本地压缩包里包含了 Windows 的 `web/node_modules`，必须删除。Docker 会在 Linux 镜像里重新执行 `npm ci` 安装依赖。

### 4. 创建配置

```bash
cp configs/config.yaml.example deploy/config.yaml
```

如服务器需要公网访问，建议编辑：

```bash
nano deploy/config.yaml
```

重点检查：

```yaml
server:
  host: 0.0.0.0
  port: 8585
  base_url: "http://你的服务器IP:8585"

pan115:
  auth_mode: cookie
  qrcode_app: android
  access_token: ""
  refresh_token: ""

strm302:
  batch_dir: "/opt/Media"
  scan_qps: 0.8
```

`base_url` 会写入 `.strm` 文件里。媒体服务器如果通过公网访问 115strm，就填公网 IP 或域名；如果只在内网访问，就填内网地址。

### 5. 启动

```bash
cd /opt/115strm-src
docker compose -f deploy/docker-compose.yml up -d --build
```

查看运行状态：

```bash
docker compose -f deploy/docker-compose.yml ps
docker compose -f deploy/docker-compose.yml logs -f 115strm
```

健康检查：

```bash
curl http://127.0.0.1:8585/api/health
```

浏览器访问：

```text
http://你的服务器IP:8585
```

### 6. 查看初始密码

首次启动会创建 `admin` 用户和随机密码。

查看密码：

```bash
sudo cat /var/lib/115strm/.initial_password
```

格式类似：

```text
admin:随机密码
```

登录后建议在“设置”页面修改密码。

### 7. 放行端口

如果是 Oracle Cloud，需要同时放行云安全组和服务器防火墙。

服务器本机如果使用 ufw：

```bash
sudo ufw allow 8585/tcp
sudo ufw reload
```

Oracle Cloud 还需要在控制台安全列表或 NSG 中放行 `8585/TCP`。

### 8. 重启、停止、升级

重启：

```bash
docker compose -f /opt/115strm-src/deploy/docker-compose.yml restart 115strm
```

停止：

```bash
docker compose -f /opt/115strm-src/deploy/docker-compose.yml down
```

升级或改代码后重新构建：

```bash
cd /opt/115strm-src
docker compose -f deploy/docker-compose.yml up -d --build
```

只要 `/var/lib/115strm`、`/var/log/115strm`、`/opt/Media` 是宿主机目录，删除或重建容器不会删除业务数据。

Docker 部署默认设置 `TZ=Asia/Shanghai`，并只读挂载宿主机 `/etc/localtime`，用于让容器日志时间和服务器北京时间一致。

### 9. 卸载 Docker 部署

停止并删除容器：

```bash
docker compose -f /opt/115strm-src/deploy/docker-compose.yml down
```

保留数据：

```text
/var/lib/115strm
/var/log/115strm
/opt/Media
```

如果确认要彻底删除数据：

```bash
sudo rm -rf /var/lib/115strm /var/log/115strm /opt/Media /opt/115strm-src
```

## 备选部署方式：systemd

如果不想用 Docker，可以使用已经构建好的 ARM64 二进制。

### 1. 上传二进制

本地已有：

```text
bin/115strm-linux-arm64
```

上传到服务器后改名：

```bash
sudo mkdir -p /opt/115strm/bin
sudo cp 115strm-linux-arm64 /opt/115strm/bin/115strm
sudo chmod +x /opt/115strm/bin/115strm
```

### 2. 准备配置和目录

```bash
sudo useradd -r -s /usr/sbin/nologin 115strm 2>/dev/null || true
sudo mkdir -p /etc/115strm /var/lib/115strm /var/log/115strm /opt/Media
sudo cp configs/config.yaml.example /etc/115strm/config.yaml
sudo chown -R 115strm:115strm /opt/115strm /var/lib/115strm /var/log/115strm /opt/Media
sudo chown 115strm:115strm /etc/115strm/config.yaml
sudo chmod 600 /etc/115strm/config.yaml
```

### 3. 注册服务

```bash
sudo cp deploy/115strm.service /etc/systemd/system/115strm.service
sudo systemctl daemon-reload
sudo systemctl enable --now 115strm
```

查看日志：

```bash
sudo journalctl -u 115strm -f
```

## 本地开发构建

```bash
cd web
npm install
npm run build
cd ..
go test ./...
go build -o bin/115strm ./cmd/server
```

Linux ARM64 交叉编译：

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o bin/115strm-linux-arm64 ./cmd/server
```

## 使用流程

1. 打开 Web 页面。
2. 用初始密码登录。
3. 在总览页登录 115。只生成 STRM 或下载伴生文件可用默认 Android 扫码或 Cookie 导入。
4. 如果要给 Emby / Infuse 纯 302 播放，推荐在总览页导入 115 Open Token，导入后会切换到 `open_token` 模式。
5. 到“115 浏览”页面从根目录逐级进入目标目录，复制 pickcode 或选择目录。
6. 到“STRM 生成”页面生成 STRM。
7. STRM 和伴生文件会输出到 `/opt/Media`。
8. 媒体服务器扫描 `/opt/Media`。

## 注意事项

- `/api/strm302/redirect` 不需要登录，这是为了让媒体服务器能直接播放 `.strm`。
- 扫码或 Cookie 模式下获取到的 115 下载链接可能依赖 Cookie / User-Agent，媒体服务器跟随 302 时无法携带 115strm 服务端的 Cookie，可能出现 115 返回 403。需要稳定纯 302 播放时，请使用 115 Open Token。
- Open Token 模式会用媒体服务器请求 `/api/strm302/redirect` 时的 User-Agent 去换 115 下载链接，并按认证模式、pickcode、User-Agent 分开缓存。
- 如果选择 Web 扫码登录，可能会挤掉你浏览器里的 115 Web 登录态；建议用默认 Android/TV 扫码或 Cookie 导入。
- 伴生文件下载按配置的 `companion_extensions` 白名单识别；会保留 115 原始目录层级，不再只下载和视频同名的文件。
- 配置保存后需要重启服务。
- 增量同步必须选择非根目录 CID，不会默认扫描根目录。
- 自动扫描清理的文件移入 `storage.data_dir/recycle/日期/任务名称/原相对路径`，暂存约 3 天；“回收站”页面支持勾选并恢复到原路径。分页可选 50/200/500/1000，默认 50。
- 恢复不会覆盖原路径已有的不同内容文件；内容相同（如已用 rsync 手工恢复）会核对并更新恢复状态。恢复采用复制方式，暂存副本按原期限清理，清理副本不影响恢复目标。
- 全量扫描的普通孤儿清理始终保护本地 `-mediainfo.json`，只有对应 STRM 真实回收后才联动回收同名 JSON。其他本地元数据仍沿用现有规则。
- 恢复要求运行进程具备媒体目录写权限，目标文件系统支持硬链接原子发布；不支持时返回失败并保留暂存文件。未保存实体的历史删除记录无法恢复。

## 镜像发布与宝塔更新

GitHub Actions 自动测试并发布 Linux AMD64/ARM64 镜像到 ghcr.io/suyang6/115strm。首次切换后可在宝塔点击 Shell 更新任务，无需上传源码压缩包。详见 [宝塔镜像更新说明](deploy/宝塔镜像更新说明.md)。
