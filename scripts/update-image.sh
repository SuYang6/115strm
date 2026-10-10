#!/usr/bin/env bash
set -Eeuo pipefail

# For the existing 115strm server after switching to the registry Compose file.
[[ $(id -u) == 0 ]] || { echo '请使用 sudo bash scripts/update-image.sh'; exit 1; }
for tool in docker tar curl flock; do
  command -v "$tool" >/dev/null || { echo "缺少命令：$tool"; exit 1; }
done
cd /opt/panstr-src
[[ -f deploy/config.yaml && -f deploy/docker-compose.yml ]] || {
  echo '未找到现有部署文件。'; exit 1;
}
exec 9>/var/lock/panstr-update.lock
flock -n 9 || { echo '另一个更新正在执行。'; exit 1; }
project=$(docker inspect 115strm --format '{{index .Config.Labels "com.docker.compose.project"}}')
[[ -n "$project" && "$project" != '<no value>' ]] || { echo '无法读取当前 Compose project 名称。'; exit 1; }
compose=(docker compose -p "$project" -f deploy/docker-compose.yml)
"${compose[@]}" config --quiet
[[ $("${compose[@]}" config --images) == 'ghcr.io/suyang6/115strm:latest' ]] || {
  echo '当前 Compose 尚未切换到 ghcr.io/suyang6/115strm:latest，请先完成一次性配置。'; exit 1;
}
backup="/opt/panstr-backups/$(date +%Y%m%d-%H%M%S)-image-$$"
mkdir -p "$backup"
cp -p deploy/config.yaml "$backup/config.yaml"
cp -p deploy/docker-compose.yml "$backup/docker-compose.yml"
old_image=$(docker inspect 115strm --format '{{.Image}}')
echo "$old_image" > "$backup/previous-image.txt"
# Keep the previous image reachable when the mutable latest tag is pulled again.
docker image tag "$old_image" "115strm:before-update-$(basename "$backup")"
stopped=0
starting=0
failed() {
  local status=$?
  trap - ERR
  if [[ $stopped == 1 && $starting == 0 ]]; then
    docker start 115strm || true
  fi
  echo "更新失败；备份保留在 $backup。" >&2
  exit "$status"
}
trap failed ERR

echo '拉取最新镜像；当前服务保持运行。'
"${compose[@]}" pull 115strm
"${compose[@]}" stop 115strm
stopped=1
tar -czf "$backup/panstr-data.tar.gz" -C /var/lib panstr
starting=1
"${compose[@]}" up -d --no-deps --no-build --pull never 115strm
for attempt in {1..20}; do
  if curl --noproxy '*' -fsS --max-time 3 http://127.0.0.1:8585/api/health; then
    echo
    "${compose[@]}" ps
    echo "更新成功。备份：$backup"
    exit 0
  fi
  sleep 3
done
"${compose[@]}" logs --tail=100 || true
echo '健康检查失败，请检查日志。数据备份已保留，不自动回退数据库。' >&2
exit 1
