<template>
  <section class="panel">
    <h2 class="page-title">STRM 生成</h2>
    <div class="form-grid">
      <el-form-item label="扫描源">
        <el-select v-model="scanSource" style="width:100%">
          <el-option label="115 API" value="115" />
          <el-option label="CD2 本地挂载" value="cd2" />
          <el-option label="OpenList 目录树" value="openlist" />
        </el-select>
      </el-form-item>
      <el-form-item label="扫描模式">
        <el-select v-model="scanMode" style="width:100%">
          <el-option label="全量扫描" value="full" />
          <el-option label="增量扫描" value="incremental" />
        </el-select>
      </el-form-item>
      <el-form-item v-if="isPan115Source" label="目录 CID">
        <el-input v-model="cid" placeholder="请输入要扫描的 115 文件夹 CID" />
      </el-form-item>
      <el-form-item :label="isPan115Source ? '输出相对目录' : '扫描子目录'">
        <el-input v-model="outputPath" :placeholder="isPan115Source ? '可选，例如 /电影；留空则输出到 STRM 根目录' : '可选，例如 /电视；留空则扫描配置的挂载根目录'" />
      </el-form-item>
    </div>
    <div class="toolbar">
      <el-button type="primary" :icon="FolderTree" @click="generateDir">开始扫描</el-button>
      <el-button v-if="canPause" type="warning" :icon="Pause" @click="pauseJob">暂停当前任务</el-button>
      <el-button v-if="canStop" type="danger" :icon="Square" @click="stopJob">停止当前任务</el-button>
      <el-button v-if="resumeAvailable && !jobRunning" type="success" :icon="RotateCcw" @click="resumeGenerate">继续当前任务</el-button>
      <el-button :icon="RefreshCw" @click="refreshState">刷新状态</el-button>
    </div>
  </section>

  <section v-if="currentJob || resumeAvailable" class="panel">
    <div class="job-head">
      <div>
        <h2 class="page-title">当前任务</h2>
        <p v-if="currentJob" class="muted">#{{ currentJob.id }} · {{ phaseText(currentJob.phase) }} · {{ currentJob.message || statusText(currentJob.status) }}</p>
        <p v-else class="muted">存在可继续的扫描暂存</p>
      </div>
      <el-tag v-if="currentJob" :type="jobTagType(currentJob.status)">{{ statusText(currentJob.status) }}</el-tag>
    </div>

    <el-progress v-if="currentJob" :percentage="jobPercent" :status="progressStatus" />

    <div class="progress-grid">
      <div><span>已扫描</span><strong>{{ currentJob?.scanned || resumeCheckpoint?.files || 0 }}</strong></div>
      <div><span>处理进度</span><strong>{{ currentJob?.done || 0 }} / {{ currentJob?.total || currentJob?.plan_total || 0 }}</strong></div>
      <div><span>下载进度</span><strong>{{ currentJob?.download_done || 0 }} / {{ currentJob?.download_total || 0 }}</strong></div>
      <div><span>失败</span><strong>{{ currentJob?.failed || 0 }}</strong></div>
      <div><span>115 请求</span><strong>{{ currentJob?.pan115_requests?.total || 0 }}</strong></div>
      <div><span>待扫目录</span><strong>{{ resumeCheckpoint?.pending_dirs || 0 }}</strong></div>
    </div>

    <p v-if="currentJob?.current_path" class="code current-path">{{ currentJob.current_path }}</p>
    <p v-else-if="resumeCheckpoint?.current_path" class="code current-path">{{ resumeCheckpoint.current_path }}</p>

    <div v-if="recent115Requests.length" class="recent-requests">
      <div class="recent-head">
        <span>最近 5 条 115 请求</span>
        <span class="muted">用于判断当前卡在哪个目录或 pickcode</span>
      </div>
      <div v-for="(item, index) in recent115Requests" :key="index" class="recent-row">
        <span class="recent-index">{{ index + 1 }}</span>
        <el-tag size="small" type="info">{{ item.label || item.action || '-' }}</el-tag>
        <span class="code" :title="item.url || ''">{{ recent115Name(item) }}</span>
      </div>
    </div>

    <div v-if="currentJob" class="progress-grid plan-grid">
      <div><span>STRM 新建/更新/跳过</span><strong>{{ jobStats.video_new || 0 }} / {{ jobStats.video_updated || 0 }} / {{ jobStats.video_skipped || 0 }}</strong></div>
      <div><span>媒体本地/移动/下载</span><strong>{{ jobStats.media_local || 0 }} / {{ jobStats.media_moved || 0 }} / {{ jobStats.media_download || 0 }}</strong></div>
      <div><span>媒体跳过</span><strong>{{ jobStats.media_skipped || 0 }}</strong></div>
      <div><span>清理删除</span><strong>{{ jobStats.deleted || 0 }}</strong></div>
    </div>

    <el-alert v-if="currentJob?.error" :title="currentJob.error" type="warning" show-icon />
    <el-alert v-if="resumeAvailable" class="resume-alert" type="info" show-icon :title="resumeTitle" />
  </section>

  <section class="panel">
    <div class="job-head">
      <div>
        <h2 class="page-title">115 请求失败列表</h2>
        <p class="muted">扫描过程中 pickcode 查询或补齐失败会保存在这里，记录只会手动删除。</p>
      </div>
      <el-button :icon="RefreshCw" @click="loadFailures">刷新</el-button>
    </div>
    <el-table :data="failures" border>
      <el-table-column prop="source_path" label="文件路径" min-width="320">
        <template #default="{ row }">
          <span class="code">{{ row.source_path || row.file_name || '-' }}</span>
        </template>
      </el-table-column>
      <el-table-column prop="pickcode" label="pickcode" min-width="170">
        <template #default="{ row }"><span class="code">{{ row.pickcode || '' }}</span></template>
      </el-table-column>
      <el-table-column label="失败操作" width="140">
        <template #default="{ row }">{{ operationText(row.operation) }}</template>
      </el-table-column>
      <el-table-column label="状态" width="110">
        <template #default="{ row }">
          <el-tag :type="row.status === 'succeeded' ? 'success' : 'danger'">{{ row.status === 'succeeded' ? '重试成功' : '失败' }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="error" label="失败原因" min-width="260" show-overflow-tooltip />
      <el-table-column label="操作" width="150" fixed="right">
        <template #default="{ row }">
          <el-button link :loading="retryingID === row.id" @click="retryFailure(row)">重试</el-button>
          <el-button link type="danger" @click="deleteFailure(row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { FolderTree, Pause, RefreshCw, RotateCcw, Square } from 'lucide-vue-next'
import { deleteData, getData, postData } from '@/api/client'

defineOptions({ name: 'Strm302' })

const cid = ref('')
const outputPath = ref('')
const scanSource = ref('115')
const scanMode = ref('full')
const rootCID = ref('')
const currentJob = ref<any>(null)
const resumeState = ref<any>(null)
const failures = ref<any[]>([])
const retryingID = ref<number | null>(null)
let pollTimer: number | null = null

const resumeCheckpoint = computed(() => resumeState.value?.checkpoint || null)
const isPan115Source = computed(() => scanSource.value === '115')

const jobStats = computed(() => {
  if (!currentJob.value?.stats_json) return {}
  try {
    return JSON.parse(currentJob.value.stats_json)
  } catch {
    return {}
  }
})

const recent115Requests = computed(() => {
  const recent = currentJob.value?.pan115_requests?.recent
  return Array.isArray(recent) ? recent.slice(0, 5).reverse() : []
})

function recent115Name(item: any) {
  return item?.name || item?.file_name || item?.path || item?.pickcode || item?.label || item?.action || '-'
}

const jobPercent = computed(() => {
  const job = currentJob.value
  if (!job) return 0
  const total = job.total || job.plan_total || 0
  if (!total) return 0
  return Math.min(100, Math.round(((job.done || 0) + (job.failed || 0)) * 100 / total))
})

const progressStatus = computed(() => {
  if (!currentJob.value) return undefined
  if (currentJob.value.status === 'failed') return 'exception'
  if (['partial', 'paused', 'stopped'].includes(currentJob.value.status)) return 'warning'
  if (currentJob.value.status === 'success') return 'success'
  return undefined
})

const resumeAvailable = computed(() => Boolean(resumeState.value?.available))

const jobRunning = computed(() => {
  return currentJob.value && ['pending', 'running'].includes(currentJob.value.status)
})

const canPause = computed(() => {
  return jobRunning.value && currentJob.value?.phase === 'scanning'
})

const canStop = computed(() => {
  return Boolean(jobRunning.value)
})

const resumeTitle = computed(() => {
  const cp = resumeCheckpoint.value
  if (!cp) return ''
  const status = cp.status === 'paused' ? '已暂停' : '上次扫描未完成'
  return `${status}，可从 ${cp.files || 0} 个已暂存文件、${cp.pending_dirs || 0} 个待扫目录继续`
})

async function generateDir() {
  const requestCID = isPan115Source.value ? cid.value.trim() : rootCID.value.trim()
  if (!requestCID || requestCID === '0') {
    return ElMessage.warning(isPan115Source.value ? '请输入非根目录 CID' : '请先在设置页配置 115 根目录 CID')
  }
  const res: any = await postData('/strm302/generate', {
    type: 'own_directory_recursive',
    cid: requestCID,
    source_path: normalizeOutputPath(outputPath.value),
    scan_source: scanSource.value,
    scan_mode: scanMode.value
  })
  resumeState.value = null
  ElMessage.success(`任务已创建：${res.job_id}`)
  watchJob(res.job_id)
}

async function pauseJob() {
  if (!currentJob.value?.id) return
  await postData(`/strm302/generate/pause/${currentJob.value.id}`)
  ElMessage.success('已请求暂停，当前 115 请求结束后会保存进度')
  await loadJob(currentJob.value.id)
}

async function stopJob() {
  if (!currentJob.value?.id) return
  try {
    await ElMessageBox.confirm('停止后当前任务会终止，扫描暂存也会清理。正在下载的单个文件会在本次请求结束后停止后续处理。确认停止？', '停止当前任务', {
      type: 'warning',
      confirmButtonText: '停止',
      cancelButtonText: '取消'
    })
  } catch {
    return
  }
  await postData(`/strm302/generate/stop/${currentJob.value.id}`)
  ElMessage.success('已请求停止当前任务')
  await loadJob(currentJob.value.id)
}

async function resumeGenerate() {
  const res: any = await postData('/strm302/generate/resume')
  ElMessage.success(`继续任务已创建：${res.job_id}`)
  await loadResumeState(false)
  watchJob(res.job_id)
}

async function refreshState() {
  if (currentJob.value?.id) {
    await loadJob(currentJob.value.id)
  } else {
    await loadResumeState(true)
  }
  await loadFailures()
}

function watchJob(jobID: number) {
  if (pollTimer) window.clearInterval(pollTimer)
  loadJob(jobID)
  pollTimer = window.setInterval(() => loadJob(jobID), 1200)
}

async function loadJob(jobID: number) {
  const job: any = await getData(`/strm302/generate/status/${jobID}`)
  currentJob.value = job
  if (!['pending', 'running'].includes(job.status) && pollTimer) {
    window.clearInterval(pollTimer)
    pollTimer = null
    await loadResumeState(false)
    await loadFailures()
  }
}

async function loadFailures() {
  failures.value = await getData('/strm302/failures', { limit: '500' })
}

async function retryFailure(row: any) {
  retryingID.value = row.id
  try {
    await postData(`/strm302/failures/${row.id}/retry`)
    ElMessage.success('重试完成，记录已保留，请确认后手动删除')
    await loadFailures()
  } catch (error: any) {
    ElMessage.error(error?.message || '重试失败')
    await loadFailures()
  } finally {
    retryingID.value = null
  }
}

async function deleteFailure(row: any) {
  await ElMessageBox.confirm(`删除失败记录：${row.source_path || row.file_name || row.pickcode || row.id}？`, '确认删除')
  await deleteData(`/strm302/failures/${row.id}`)
  await loadFailures()
}

function operationText(operation: string) {
  const map: Record<string, string> = {
    pickcode_detail: 'pickcode 详情',
    hydrate_pickcode: '补齐 pickcode'
  }
  return map[operation] || operation || '-'
}

function phaseText(phase: string) {
  const map: Record<string, string> = {
    scanning: '扫描',
    hydrating: '补齐 pickcode',
    planning: '计划',
    writing: '写入',
    downloading: '下载',
    cleaning: '清理',
    paused: '已暂停',
    stopped: '已停止',
    finished: '完成',
    failed: '失败'
  }
  return map[phase] || phase || '等待'
}

function jobTagType(status: string) {
  if (status === 'success') return 'success'
  if (status === 'failed') return 'danger'
  if (status === 'partial' || status === 'paused' || status === 'stopped') return 'warning'
  return 'info'
}

function statusText(status: string) {
  const map: Record<string, string> = {
    pending: '等待中',
    running: '运行中',
    paused: '已暂停',
    stopped: '已停止',
    success: '成功',
    partial: '部分成功',
    failed: '失败'
  }
  return map[status] || status
}

async function loadInitialInput() {
  const [cfg, last]: any[] = await Promise.all([
    getData('/strm302/config'),
    getData('/strm302/last-input')
  ])
  cid.value = last.cid || ''
  outputPath.value = last.source_path || ''
  scanSource.value = last.scan_source || cfg.scan_source || '115'
  scanMode.value = last.scan_mode || cfg.scan_mode || 'full'
  rootCID.value = cfg.root_cid || ''
}

async function loadResumeState(setJob: boolean) {
  const state: any = await getData('/strm302/generate/resume-state')
  resumeState.value = state?.available ? state : null
  if (setJob && state?.available && state.job) {
    currentJob.value = state.job
    if (['pending', 'running'].includes(state.job.status)) {
      watchJob(state.job.id)
    }
  }
}

async function loadCurrentJob() {
  const job: any = await getData('/strm302/generate/current')
  if (job?.id && ['pending', 'running'].includes(job.status)) {
    resumeState.value = null
    currentJob.value = job
    watchJob(job.id)
    return true
  }
  return false
}

function normalizeOutputPath(raw: string) {
  const value = raw.trim().replace(/\\/g, '/')
  if (!value) return ''
  return value.startsWith('/') ? value : `/${value}`
}

onMounted(async () => {
  await Promise.all([loadInitialInput(), loadFailures()])
  if (!(await loadCurrentJob())) {
    await loadResumeState(true)
  }
})
onBeforeUnmount(() => {
  if (pollTimer) window.clearInterval(pollTimer)
})
</script>

<style scoped>
.job-actions { display: flex; align-items: center; gap: 10px; }
.resume-alert { margin-top: 12px; }
.recent-requests { margin-top: 12px; display: grid; gap: 6px; }
.recent-head { display: flex; align-items: baseline; gap: 10px; font-weight: 600; }
.recent-row { display: grid; grid-template-columns: 26px 130px minmax(0, 1fr); gap: 8px; align-items: center; padding: 8px 10px; background: var(--panel-soft); border: 1px solid var(--line); border-radius: 6px; }
.recent-index { width: 22px; height: 22px; display: inline-flex; align-items: center; justify-content: center; border-radius: 50%; background: var(--panel); border: 1px solid var(--line); font-size: 12px; color: var(--muted); }
.recent-row .code { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
</style>
