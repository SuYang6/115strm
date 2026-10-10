<template>
  <section class="panel">
    <h2 class="page-title">运行配置</h2>
    <el-form v-if="cfg" label-position="top">
      <div class="settings-layout">
        <section class="settings-section">
          <div class="section-head">
            <h3>服务基础</h3>
            <p class="muted">服务监听、外部访问地址和日志级别。</p>
          </div>
          <div class="form-grid">
            <el-form-item label="监听地址"><el-input v-model="cfg.server.host" disabled /></el-form-item>
            <el-form-item label="端口"><el-input-number v-model="cfg.server.port" :min="1" disabled style="width: 100%" /></el-form-item>
            <el-form-item label="对外 Base URL"><el-input v-model="cfg.server.base_url" /></el-form-item>
            <el-form-item label="日志级别"><el-select v-model="cfg.server.log_level" style="width:100%"><el-option value="info" /><el-option value="debug" /><el-option value="warn" /><el-option value="error" /></el-select></el-form-item>
          </div>
        </section>

        <section class="settings-section">
          <div class="section-head">
            <h3>Emby</h3>
            <p class="muted">302 网关、入库通知、扫库任务和神医助手 MediaInfo。</p>
          </div>
          <div class="form-grid">
            <el-form-item label="Emby 地址"><el-input v-model="cfg.emby.base_url" placeholder="http://host.docker.internal:8096" /></el-form-item>
            <el-form-item label="Emby API Key"><el-input v-model="cfg.emby.api_key" type="password" show-password /></el-form-item>
            <el-form-item label="Emby 入库通知"><el-switch v-model="cfg.emby.notify_refresh" /></el-form-item>
            <el-form-item label="通知冷却分钟"><el-input-number v-model="cfg.emby.notify_cooldown_minutes" :min="1" style="width:100%" /></el-form-item>
            <el-form-item label="Emby 扫库任务 ID"><el-input v-model="cfg.emby.library_task_id" placeholder="ScheduledTasks 里的媒体库扫描任务 ID" /></el-form-item>
            <el-form-item label="神医助手 MediaInfo 任务 ID"><el-input v-model="cfg.emby.mediainfo_task_id" placeholder="可选，新增 STRM 后触发" /></el-form-item>
          </div>
        </section>

        <section class="settings-section">
          <div class="section-head">
            <h3>115 与 STRM</h3>
            <p class="muted">登录端、输出目录、默认扫描方式和 115 请求节流。</p>
          </div>
          <div class="form-grid">
            <el-form-item label="115 扫码登录端"><el-select v-model="cfg.pan115.qrcode_app" style="width:100%"><el-option label="Android" value="android" /><el-option label="TV" value="tv" /><el-option label="iOS" value="ios" /><el-option label="Web" value="web" /></el-select></el-form-item>
            <el-form-item label="STRM 输出目录"><el-input v-model="cfg.strm302.batch_dir" /></el-form-item>
            <el-form-item label="视频扩展名"><el-input v-model="cfg.strm302.video_extensions" /></el-form-item>
            <el-form-item label="115 根目录 CID"><el-input v-model="cfg.strm302.root_cid" placeholder="2950294963365084864" /></el-form-item>
            <el-form-item label="默认扫描源">
              <el-select v-model="cfg.strm302.scan_source" style="width:100%">
                <el-option label="115 API" value="115" />
                <el-option label="CD2 本地挂载" value="cd2" />
                <el-option label="OpenList 目录树" value="openlist" />
              </el-select>
            </el-form-item>
            <el-form-item label="默认扫描模式">
              <el-select v-model="cfg.strm302.scan_mode" style="width:100%">
                <el-option label="全量扫描" value="full" />
                <el-option label="增量扫描" value="incremental" />
              </el-select>
            </el-form-item>
            <el-form-item label="115 QPS"><el-input-number v-model="cfg.strm302.scan_qps" :min="0.01" :step="0.01" :precision="2" style="width:100%" /></el-form-item>
            <el-form-item label="并发数"><el-input-number v-model="cfg.strm302.detail_threads" :min="1" style="width:100%" /></el-form-item>
          </div>
        </section>

        <section class="settings-section">
          <div class="section-head">
            <h3>目录树来源</h3>
            <p class="muted">CD2/OpenList 只用于读取目录树，播放仍走 115strm 302。</p>
          </div>
          <div class="form-grid">
            <el-form-item label="CD2 挂载根目录"><el-input v-model="cfg.strm302.cd2_mount_dir" placeholder="/opt/115open/电视电影" /></el-form-item>
            <el-form-item label="OpenList 地址"><el-input v-model="cfg.strm302.openlist_base_url" placeholder="http://127.0.0.1:5244" /></el-form-item>
            <el-form-item label="OpenList 根路径"><el-input v-model="cfg.strm302.openlist_root_path" placeholder="/电视电影" /></el-form-item>
            <el-form-item label="OpenList 每页数量"><el-input-number v-model="cfg.strm302.openlist_page_size" :min="20" :max="1000" :step="20" style="width:100%" /></el-form-item>
            <el-form-item label="OpenList Token"><el-input v-model="cfg.strm302.openlist_token" type="password" show-password /></el-form-item>
            <el-form-item label="OpenList 强制刷新"><el-switch v-model="cfg.strm302.openlist_refresh" /></el-form-item>
            <el-form-item label="OpenList 用户名"><el-input v-model="cfg.strm302.openlist_username" /></el-form-item>
            <el-form-item label="OpenList 密码"><el-input v-model="cfg.strm302.openlist_password" type="password" show-password /></el-form-item>
          </div>
        </section>

        <section class="settings-section">
          <div class="section-head">
            <h3>存储</h3>
            <p class="muted">数据库、运行日志和审计日志保留时间。</p>
          </div>
          <div class="form-grid">
            <el-form-item label="日志文件"><el-input v-model="cfg.storage.log_file" /></el-form-item>
            <el-form-item label="审计日志保留天数"><el-input-number v-model="cfg.storage.audit_keep_days" :min="1" :max="60" style="width:100%" /></el-form-item>
            <el-form-item label="数据库文件"><el-input v-model="cfg.storage.db_file" /></el-form-item>
          </div>
        </section>
      </div>

      <el-alert title="配置保存后不会热生效，保存后可直接点击下方按钮重启服务。" type="warning" show-icon />
      <div class="toolbar" style="margin-top: 16px">
        <el-button type="primary" @click="save">保存配置</el-button>
        <el-button :icon="Power" :loading="restarting" @click="restartService">重启服务</el-button>
      </div>
    </el-form>
  </section>

  <section class="panel">
    <h2 class="page-title">账号安全</h2>
    <div class="form-grid">
      <el-input v-model="oldPassword" type="password" show-password placeholder="旧密码" />
      <el-input v-model="newPassword" type="password" show-password placeholder="新密码" />
    </div>
    <el-alert class="password-hint" title="旧密码是当前登录密码；首次部署的初始密码在数据目录的 .initial_password 文件里。" type="info" show-icon />
    <div class="toolbar" style="margin-top: 16px"><el-button @click="changePassword">修改密码</el-button></div>
  </section>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Power } from 'lucide-vue-next'
import { getData, postData, putData } from '@/api/client'

defineOptions({ name: 'Settings' })

const cfg = ref<any>(null)
const oldPassword = ref('')
const newPassword = ref('')
const restarting = ref(false)

async function load() { cfg.value = await getData('/settings') }
async function save() {
  await putData('/settings', cfg.value)
  try {
    await ElMessageBox.alert('配置已保存。点击确定后会立即重启服务，恢复后页面会自动刷新。', '需要重启', {
      confirmButtonText: '确定并重启',
      type: 'warning',
      closeOnClickModal: false,
      closeOnPressEscape: false
    })
  } catch {
    return
  }
  await requestRestartAndReload('配置已保存，服务正在重启，恢复后会自动刷新页面', serviceOrigin())
}
async function restartService() {
  try {
    await ElMessageBox.confirm('服务会短暂中断，并由 Docker 自动拉起。确认重启？', '重启服务')
  } catch {
    return
  }
  await requestRestartAndReload('服务正在重启，恢复后会自动刷新页面', serviceOrigin())
}

async function requestRestartAndReload(message: string, targetOrigin: string) {
  if (restarting.value) return
  restarting.value = true
  try {
    await postData('/system/restart')
    ElMessage.success(message)
    await reloadWhenServiceReady(targetOrigin)
  } catch {
    ElMessage.warning('已发送重启请求，页面会继续等待服务恢复')
    await reloadWhenServiceReady(targetOrigin)
  }
}

async function reloadWhenServiceReady(targetOrigin: string) {
  const started = Date.now()
  const deadline = started + 90000
  let sawDown = false
  await sleep(1200)
  while (Date.now() < deadline) {
    const ready = await healthReady(targetOrigin)
    if (!ready) sawDown = true
    if (ready && (sawDown || Date.now() - started > 8000)) {
      reloadToOrigin(targetOrigin)
      return
    }
    await sleep(1000)
  }
  restarting.value = false
  ElMessage.warning('服务重启时间较长，请手动刷新页面')
}

async function healthReady(targetOrigin: string) {
  const controller = new AbortController()
  const timer = window.setTimeout(() => controller.abort(), 1500)
  const sameOrigin = targetOrigin === window.location.origin
  try {
    const res = await fetch(`${targetOrigin}/api/health?_=${Date.now()}`, {
      cache: 'no-store',
      mode: sameOrigin ? 'cors' : 'no-cors',
      signal: controller.signal
    })
    return sameOrigin ? res.ok : true
  } catch {
    return false
  } finally {
    window.clearTimeout(timer)
  }
}

function sleep(ms: number) {
  return new Promise((resolve) => window.setTimeout(resolve, ms))
}

function serviceOrigin() {
  const url = new URL(window.location.href)
  const port = Number(cfg.value?.server?.port || 0)
  if (port > 0) {
    url.port = String(port)
  }
  return url.origin
}

function reloadToOrigin(targetOrigin: string) {
  if (targetOrigin === window.location.origin) {
    window.location.reload()
    return
  }
  const target = new URL(window.location.href)
  const origin = new URL(targetOrigin)
  target.protocol = origin.protocol
  target.host = origin.host
  window.location.href = target.toString()
}
async function changePassword() {
  try {
    await putData('/auth/password', { old_password: oldPassword.value, new_password: newPassword.value })
    ElMessage.success('密码已修改')
    oldPassword.value = ''
    newPassword.value = ''
  } catch (error: any) {
    const message = error?.message || ''
    if (message.includes('old password invalid')) {
      ElMessage.error('旧密码不正确。请填写当前登录密码，初始密码可在数据目录 .initial_password 文件查看。')
      return
    }
    throw error
  }
}
onMounted(load)
</script>

<style scoped>
.settings-layout {
  display: grid;
  gap: 20px;
}

.settings-section {
  padding-top: 18px;
  border-top: 1px solid var(--line);
}

.settings-section:first-child {
  padding-top: 0;
  border-top: 0;
}

.section-head {
  margin-bottom: 12px;
}

.section-head h3 {
  margin: 0 0 4px;
  font-size: 16px;
  font-weight: 700;
}

.section-head p {
  margin: 0;
}

.password-hint {
  margin-top: 12px;
}
</style>
