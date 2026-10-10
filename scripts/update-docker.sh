#!/usr/bin/env bash
set -Eeuo pipefail

# Execute a private copy so extracting a new release cannot change this running script.
if [[ ${PANSTR_UPDATE_RUNNING:-0} != 1 ]]; then
  runner=$(mktemp /tmp/panstr-update-runner.XXXXXX)
  cp -- "$0" "$runner"
  exec env PANSTR_UPDATE_RUNNING=1 bash "$runner" "$@"
fi

SOURCE_DIR=/opt/panstr-src
ZIP_PATH=${1:-/tmp/panstr.zip}
BACKUP_ROOT=/opt/panstr-backups
stage=''
backup=''
stopped=0
starting=0
compose=()

cleanup() {
  [[ -z "$stage" ]] || rm -rf -- "$stage"
  case "$0" in /tmp/panstr-update-runner.*) rm -f -- "$0" ;; esac
}
failed() {
  local status=$?
  trap - ERR
  echo "更新失败，退出码：$status；备份目录：${backup:-尚未创建}" >&2
  if [[ $stopped == 1 && $starting == 0 ]]; then
    echo '新版尚未启动，尝试恢复原容器运行。' >&2
    docker start 115strm || true
  fi
  echo '请保留错误输出；不要删除数据目录。' >&2
  exit "$status"
}
trap cleanup EXIT
trap failed ERR

[[ $(id -u) == 0 ]] || { echo '请使用 sudo bash scripts/update-docker.sh /tmp/panstr.zip'; exit 1; }
for tool in docker unzip tar curl flock; do
  command -v "$tool" >/dev/null || { echo "缺少命令：$tool"; exit 1; }
done
docker compose version >/dev/null
exec 9>/var/lock/panstr-update.lock
flock -n 9 || { echo '另一个更新正在执行，请等待它结束。'; exit 1; }
[[ -f "$ZIP_PATH" ]] || { echo "找不到压缩包：$ZIP_PATH"; exit 1; }
ZIP_PATH=$(realpath "$ZIP_PATH")
cd "$SOURCE_DIR"
[[ -f deploy/config.yaml && -f deploy/docker-compose.yml ]] || {
  echo '未找到现有配置或 Compose，此脚本用于更新当前 115strm 部署。'; exit 1;
}

echo '[1/6] 检查压缩包'
unzip -tq "$ZIP_PATH"
# Reject unsafe names before extraction. Releases must have a flat project root.
if unzip -Z1 "$ZIP_PATH" | LC_ALL=C grep -Eq '(^/|(^|/)\.\.(/|$)|\\|^[A-Za-z]:)'; then
  echo '压缩包包含不安全路径，已停止。'; exit 1
fi
stage=$(mktemp -d /tmp/panstr-update-stage.XXXXXX)
unzip -oq "$ZIP_PATH" -d "$stage"
for required in go.mod go.sum deploy/Dockerfile web/package-lock.json web/src/mock/data/recycle.ts scripts/update-docker.sh; do
  [[ -f "$stage/$required" ]] || { echo "压缩包缺少文件：$required"; exit 1; }
done
if [[ -n $(find "$stage" -type l -print -quit) ]]; then
  echo '源码包含符号链接，已停止。'; exit 1
fi

echo '[2/6] 保存原配置并更新源码'
backup="$BACKUP_ROOT/$(date +%Y%m%d-%H%M%S)-$$"
mkdir -p "$backup"
cp -p deploy/config.yaml "$backup/config.yaml"
cp -p deploy/docker-compose.yml "$backup/docker-compose.yml"
docker inspect 115strm --format '{{.Image}}' > "$backup/previous-image.txt"
project=$(docker inspect 115strm --format '{{index .Config.Labels "com.docker.compose.project"}}')
[[ -n "$project" && "$project" != '<no value>' ]] || { echo '无法读取当前 Compose project 名称。'; exit 1; }
# Never replace the server's working config or custom mounts.
rm -f -- "$stage/deploy/config.yaml" "$stage/deploy/docker-compose.yml"
cp -a "$stage/." "$SOURCE_DIR/"
compose=(docker compose -p "$project" -f deploy/docker-compose.yml)
"${compose[@]}" config --quiet

echo '[3/6] 构建镜像，当前服务保持运行'
"${compose[@]}" build 115strm

echo '[4/6] 停止服务并备份 /var/lib/panstr'
"${compose[@]}" stop 115strm
stopped=1
tar -czf "$backup/panstr-data.tar.gz" -C /var/lib panstr

echo '[5/6] 启动新版'
starting=1
"${compose[@]}" up -d --no-deps 115strm

echo '[6/6] 检查服务健康状态'
for attempt in {1..20}; do
  if curl --noproxy '*' -fsS --max-time 3 http://127.0.0.1:8585/api/health; then
    echo
    "${compose[@]}" ps
    echo "更新成功。原配置与数据备份：$backup"
    exit 0
  fi
  sleep 3
done
"${compose[@]}" logs --tail=100 || true
echo '新版健康检查失败。数据备份已保留，不自动回退数据库，请检查上面的日志。' >&2
exit 1
