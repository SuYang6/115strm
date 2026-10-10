<template>
  <div class="grid grid-4">
    <section class="panel metric">
      <span>115 登录</span>
      <strong>{{ stats?.['115']?.logged_in ? '已登录' : '未登录' }}</strong>
      <p class="muted code">{{ authStatusLine }}</p>
    </section>
    <section class="panel metric">
      <span>电视剧</span>
      <strong>{{ mediaLibrary.tv_strm_count ?? 0 }}</strong>
      <p class="muted code">{{ mediaStatsLine }}</p>
    </section>
    <section class="panel metric">
      <span>电影</span>
      <strong>{{ mediaLibrary.movie_strm_count ?? 0 }}</strong>
    </section>
    <section class="panel metric">
      <span>运行中任务</span>
      <strong>{{ stats?.running_tasks ?? 0 }}</strong>
    </section>
  </div>

  <section class="panel">
    <div class="job-head">
      <div>
        <h2 class="page-title">115 请求统计</h2>
        <p class="muted">今日 {{ pan115RequestStats.day || '-' }}</p>
      </div>
      <el-tag>限速后计数</el-tag>
    </div>
    <div class="progress-grid">
      <div><span>今日总请求</span><strong>{{ pan115RequestStats.total || 0 }}</strong></div>
      <div><span>目录列表</span><strong>{{ pan115Actions.list || 0 }}</strong></div>
      <div><span>搜索</span><strong>{{ pan115Actions.search || 0 }}</strong></div>
      <div><span>取直链</span><strong>{{ pan115DownloadURLCount }}</strong></div>
    </div>
    <p class="muted code stats-line">OpenAPI 目录 {{ pan115Actions.open_get_files || 0 }}，OpenAPI 直链 {{ pan115Actions.open_down_url || 0 }}</p>
  </section>

  <section class="panel">
    <div class="toolbar">
      <el-button type="primary" :icon="RefreshCw" @click="load">刷新状态</el-button>
      <el-select v-model="loginApp" style="width: 160px">
        <el-option label="Android 扫码" value="android" />
        <el-option label="TV 扫码" value="tv" />
        <el-option label="iOS 扫码" value="ios" />
        <el-option label="Web 扫码" value="web" />
      </el-select>
      <el-button :icon="QrCode" @click="startQR">115 扫码登录</el-button>
      <el-button :icon="KeyRound" @click="cookieDialog = true">导入 Cookie</el-button>
      <el-button :icon="KeyRound" @click="openTokenDialog = true">导入 Open Token</el-button>
      <el-button :icon="LogOut" @click="logout115">退出 115</el-button>
    </div>
    <el-alert v-if="qrImage" title="请用 115 客户端扫码，扫码确认后点击检查状态，再点击确认登录。默认 Android 端登录，避免占用浏览器 Web 登录态。" type="info" show-icon />
    <div v-if="qrImage" class="qr-row">
      <img :src="qrImage" alt="115 登录二维码" />
      <div>
        <el-button @click="checkQR">检查状态</el-button>
        <el-button type="primary" @click="finishQR">确认登录</el-button>
        <p class="muted">状态：{{ qrStatus }}</p>
      </div>
    </div>
  </section>

  <el-dialog v-model="cookieDialog" title="导入 115 Cookie" width="640px">
    <el-alert title="从已经登录 115 的浏览器复制 Cookie，至少需要 UID、CID、SEID，KID 可有可无。导入后系统只保存本地加密凭证。" type="info" show-icon />
    <el-input v-model="cookieText" type="textarea" :rows="7" placeholder="UID=...; CID=...; SEID=...; KID=..." style="margin-top: 14px" />
    <template #footer>
      <el-button @click="cookieDialog=false">取消</el-button>
      <el-button type="primary" @click="importCookie">验证并导入</el-button>
    </template>
  </el-dialog>

  <el-dialog v-model="openTokenDialog" title="导入 115 Open Token" width="680px">
    <el-alert title="用于 Emby / Infuse 纯 302 播放。请填入 115 开放平台 Access Token 和 Refresh Token，导入后会切换到 open_token 模式。" type="info" show-icon />
    <div class="token-form">
      <el-input v-model="openAccessToken" type="textarea" :rows="3" placeholder="Access Token" />
      <el-input v-model="openRefreshToken" type="textarea" :rows="3" placeholder="Refresh Token" />
    </div>
    <template #footer>
      <el-button @click="openTokenDialog=false">取消</el-button>
      <el-button type="primary" @click="importOpenToken">验证并导入</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { KeyRound, LogOut, QrCode, RefreshCw } from 'lucide-vue-next'
import { getData, postData } from '@/api/client'

defineOptions({ name: 'Dashboard' })

const stats = ref<any>(null)
const qrImage = ref('')
const qrStatus = ref('未开始')
const loginApp = ref('android')
const cookieDialog = ref(false)
const cookieText = ref('')
const openTokenDialog = ref(false)
const openAccessToken = ref('')
const openRefreshToken = ref('')

const authModeText = computed(() => {
  const mode = stats.value?.['115']?.auth_mode
  return mode === 'open_token' ? 'Open Token' : 'Cookie / 扫码'
})
const authStatusLine = computed(() => {
  const info = stats.value?.['115']
  return [authModeText.value, info?.uid].filter(Boolean).join(' · ')
})
const pan115RequestStats = computed(() => stats.value?.pan115_request_stats || {})
const pan115Actions = computed(() => pan115RequestStats.value?.by_action || {})
const pan115DownloadURLCount = computed(() => {
  return (pan115Actions.value.download_url || 0) + (pan115Actions.value.open_down_url || 0)
})
const mediaLibrary = computed(() => stats.value?.media_library || {})
const mediaStatsLine = computed(() => {
  if (!mediaLibrary.value?.available) return '任务成功后更新'
  if (!mediaLibrary.value?.updated_at) return mediaLibrary.value?.root || ''
  return new Date(mediaLibrary.value.updated_at).toLocaleString()
})

async function load() {
  stats.value = await getData('/dashboard/stats')
}
async function startQR() {
  const data: any = await postData('/115/qrcode/start')
  qrImage.value = data.image
  qrStatus.value = '等待扫码'
}
async function checkQR() {
  const data: any = await getData('/115/qrcode/status')
  qrStatus.value = `${data.msg || ''} (${data.status})`
}
async function finishQR() {
  await postData('/115/qrcode/login', { app: loginApp.value })
  ElMessage.success('115 登录完成')
  qrImage.value = ''
  await load()
}
async function importCookie() {
  if (!cookieText.value.trim()) return ElMessage.warning('请粘贴 115 Cookie')
  await postData('/115/cookie/import', { cookie: cookieText.value })
  ElMessage.success('115 Cookie 已导入')
  cookieText.value = ''
  cookieDialog.value = false
  await load()
}
async function importOpenToken() {
  const accessToken = openAccessToken.value.trim()
  const refreshToken = openRefreshToken.value.trim()
  if (!accessToken || !refreshToken) return ElMessage.warning('请填写 Access Token 和 Refresh Token')
  await postData('/115/open-token/import', { access_token: accessToken, refresh_token: refreshToken })
  ElMessage.success('115 Open Token 已导入')
  openAccessToken.value = ''
  openRefreshToken.value = ''
  openTokenDialog.value = false
  await load()
}
async function logout115() {
  await postData('/115/logout')
  ElMessage.success('已退出 115')
  await load()
}
onMounted(load)
</script>

<style scoped>
.qr-row { display: flex; gap: 20px; align-items: center; margin-top: 16px; }
.qr-row img { width: 180px; height: 180px; border: 1px solid var(--line); border-radius: 8px; }
.token-form { display: grid; gap: 12px; margin-top: 14px; }
.stats-line { margin-top: 12px; }
</style>
