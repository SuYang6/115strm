# Docker 压缩包更新说明

日期：2026-10-10

压缩包：panstr-docker-update-20261010-v4.zip。源码 ZIP 用于服务器 Docker 构建，不是 docker load 镜像包。

## 当前服务器部署约定

默认 deploy/docker-compose.yml 与当前服务器一致：服务及容器名称 115strm，数据挂载 /var/lib/panstr:/var/lib/panstr，日志挂载 /var/log/panstr:/var/log/panstr，媒体挂载 /opt/Media:/opt/Media。宿主机 deploy/config.yaml 挂载到容器 /etc/115strm/config.yaml，启动命令读取这一配置文件。

示例配置和 Dockerfile 的数据、日志目录同步为 panstr 路径。回收站位于 /var/lib/panstr/recycle。不要删除 /var/lib/panstr。

运行配置 deploy/config.yaml 不进入 ZIP。更新时保留服务器原文件，不需要编辑配置或手动替换 Compose 中的目录。

## 当前 115strm 容器的更新流程

上传新版源码包为 /tmp/panstr.zip，暂停扫描，再在服务器执行以下 Bash 命令。每条完整命令独立执行；不删除源码目录，不使用 down -v。

```bash
cd /opt/panstr-src
```

下面的括号块是一条完整命令，可整块粘贴；任一步失败即停止后续操作。

```bash
(
set -e
STAMP=$(date +%Y%m%d-%H%M%S)
BACKUP="/opt/panstr-backups/$STAMP"
test -f deploy/config.yaml
sudo mkdir -p "$BACKUP"
sudo cp -p deploy/config.yaml "$BACKUP/config.yaml"
sudo cp -p deploy/docker-compose.yml "$BACKUP/docker-compose.yml"
sudo unzip -oq /tmp/panstr.zip -d /opt/panstr-src -x deploy/config.yaml
sudo docker compose -f deploy/docker-compose.yml config --quiet
sudo docker compose -f deploy/docker-compose.yml build 115strm
sudo docker compose -f deploy/docker-compose.yml stop 115strm
sudo tar -czf "$BACKUP/panstr-data.tar.gz" -C /var/lib panstr
sudo docker compose -f deploy/docker-compose.yml up -d --no-deps 115strm
sudo docker compose -f deploy/docker-compose.yml ps
sudo docker compose -f deploy/docker-compose.yml logs --tail=50
curl --noproxy '*' -f --max-time 10 http://127.0.0.1:8585/api/health
)
```

构建期间当前服务继续运行。停服务后备份数据库及回收站，备份失败不会启动新版；先解决备份问题再执行 up -d。更新不会修改原运行配置。压缩包包含的默认 Compose 已使用正确挂载，直接采用即可。若服务器额外定制了端口、用户或挂载，应与备份的 Compose 核对。

## 仍使用 panstr 容器名称的旧部署

deploy/docker-compose.panstr.yml 仅供仍使用 panstr 服务和容器名称的部署。当前服务器已经运行 115strm，继续使用默认 deploy/docker-compose.yml，不需要切换服务名。

## 构建与打包验证

v2 补齐 web/src/mock/data/recycle.ts，解压后 Node 22 npm ci、生产构建以及 Linux ARM64 后端编译均通过。v3 只调整部署路径和说明；默认 Compose、配置示例、Dockerfile 路径一致，ZIP 文件逐项对比源码 SHA256。

本机没有 Docker，未执行本地镜像构建。Dockerfile 沿用 Linux ARM64 编译目标。后续源码包继续遵循这里的部署约定，运行配置不进入发布 ZIP。
## 一键更新（推荐）

将本包上传为 /tmp/panstr.zip。首次安装更新脚本只需执行下面一条命令：

```bash
unzip -p /tmp/panstr.zip scripts/update-docker.sh > /tmp/panstr-update.sh && sudo bash /tmp/panstr-update.sh /tmp/panstr.zip
```

更新成功后，后续上传新版包并执行：

```bash
sudo bash /opt/panstr-src/scripts/update-docker.sh /tmp/panstr.zip
```

无需自行修改配置。脚本保留服务器 config.yaml 和 docker-compose.yml，使用现有 Compose project 名称；检查源码包，构建镜像，停服务备份数据，再启动新版并检查 8585 健康接口。支持同时更新限制，备份位于 /opt/panstr-backups/时间戳-进程号。构建失败时旧服务继续运行；数据备份失败时尝试启动原容器。新版启动失败时保留数据备份和日志，不自动回退迁移后的数据库。

脚本适用于当前 /opt/panstr-src、115strm 容器、/var/lib/panstr 数据和 8585 端口。需要 Bash、Docker Compose、unzip、tar、curl、flock 及足够的备份空间。若以后发布明确要求调整部署文件，需合并相应变化；脚本默认保留当前部署设置。