<template>
  <section class="panel">
    <div class="toolbar">
      <el-button type="primary" :icon="Plus" @click="openCreate()">新建任务</el-button>
      <el-button :icon="RefreshCw" @click="load">刷新</el-button>
      <el-dropdown @command="fromTemplate">
        <el-button>任务模板</el-button>
        <template #dropdown>
          <el-dropdown-menu>
            <el-dropdown-item v-for="tpl in templates" :key="tpl.name" :command="tpl">{{ tpl.name }}</el-dropdown-item>
          </el-dropdown-menu>
        </template>
      </el-dropdown>
    </div>
    <el-table :data="tasks" border>
      <el-table-column prop="id" label="ID" width="90" />
      <el-table-column prop="name" label="名称" min-width="180" />
      <el-table-column label="任务" min-width="220">
        <template #default="{ row }">{{ taskSummary(row) }}</template>
      </el-table-column>
      <el-table-column label="执行时间" min-width="180">
        <template #default="{ row }">{{ scheduleSummary(row.cron_expr) }}</template>
      </el-table-column>
      <el-table-column prop="enabled" label="状态" width="90">
        <template #default="{ row }">
          <el-tag :type="row.enabled ? 'success' : 'info'">{{ row.enabled ? '启用' : '停用' }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="last_status" label="上次状态" width="120" />
      <el-table-column label="运行状态" min-width="280">
        <template #default="{ row }">
          <div class="task-status">
            <div class="task-status-line">
              <el-tag size="small" :type="taskStatusTag(row)">{{ taskStatusText(row) }}</el-tag>
              <span v-if="taskLatestRun(row)?.strm_job_id" class="muted">STRM #{{ taskLatestRun(row).strm_job_id }}</span>
            </div>
            <template v-if="taskStatus(row)?.strm_job">
              <el-progress :percentage="taskProgress(row)" :status="taskProgressStatus(row)" :show-text="false" />
              <div class="muted">{{ taskProgressLine(row) }}</div>
            </template>
          </div>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="260">
        <template #default="{ row }">
          <el-button link @click="run(row)">立即运行</el-button>
          <el-button link @click="openEdit(row)">编辑</el-button>
          <el-button link @click="toggle(row)">{{ row.enabled ? '停用' : '启用' }}</el-button>
          <el-button link type="danger" @click="remove(row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>
  </section>

  <el-dialog v-model="dialog" title="任务配置" width="760px">
    <el-form label-position="top">
      <div class="form-grid">
        <el-form-item label="任务名称"><el-input v-model="form.name" /></el-form-item>
        <el-form-item label="任务类型">
          <el-select v-model="form.type" style="width:100%">
            <el-option label="STRM 扫描" value="strm_scan" />
            <el-option label="Hash 巡检" value="hash_patrol" />
            <el-option label="回收站清理" value="recycle_clean" />
          </el-select>
        </el-form-item>
      </div>

      <template v-if="form.type === 'strm_scan' || form.type === 'incremental'">
        <div class="form-grid">
          <el-form-item label="扫描源">
            <el-select v-model="form.scan_source" style="width:100%">
              <el-option label="115 API" value="115" />
              <el-option label="CD2 本地挂载" value="cd2" />
              <el-option label="OpenList 目录树" value="openlist" />
            </el-select>
          </el-form-item>
          <el-form-item label="扫描模式">
            <el-select v-model="form.scan_mode" style="width:100%">
              <el-option label="增量扫描" value="incremental" />
              <el-option label="全量扫描" value="full" />
            </el-select>
          </el-form-item>
          <el-form-item v-if="form.scan_source === '115'" label="目录 CID">
            <el-input v-model="form.cid" placeholder="请输入要扫描的 115 文件夹 CID" />
          </el-form-item>
          <el-form-item :label="form.scan_source === '115' ? '输出相对目录' : '扫描子目录'">
            <el-input v-model="form.source_path" :placeholder="form.scan_source === '115' ? '可选，例如 /电视' : '可选，例如 /电视；留空则扫描配置的根目录'" />
          </el-form-item>
        </div>
      </template>

      <template v-else-if="form.type === 'hash_patrol'">
        <el-form-item label="巡检相对目录"><el-input v-model="form.prefix" placeholder="可选，例如 /电视" /></el-form-item>
      </template>

      <template v-else-if="form.type === 'recycle_clean'">
        <el-form-item label="保留天数"><el-input-number v-model="form.keep_days" :min="1" style="width:100%" /></el-form-item>
      </template>

      <div class="form-grid">
        <el-form-item label="执行周期">
          <el-select v-model="form.schedule_type" style="width:100%">
            <el-option label="每天" value="daily" />
            <el-option label="每周" value="weekly" />
            <el-option label="每月" value="monthly" />
            <el-option label="每 N 分钟" value="minutes" />
            <el-option label="每 N 小时" value="hours" />
          </el-select>
        </el-form-item>
        <el-form-item v-if="form.schedule_type === 'weekly'" label="星期">
          <el-select v-model="form.weekday" multiple collapse-tags collapse-tags-tooltip style="width:100%">
            <el-option v-for="day in weekdays" :key="day.value" :label="day.label" :value="day.value" />
          </el-select>
        </el-form-item>
        <el-form-item v-if="form.schedule_type === 'monthly'" label="日期">
          <el-input-number v-model="form.month_day" :min="1" :max="31" style="width:100%" />
        </el-form-item>
        <el-form-item v-if="['daily','weekly','monthly'].includes(form.schedule_type)" label="小时">
          <el-select v-model="form.hour" style="width:100%"><el-option v-for="n in hours" :key="n" :label="pad(n)" :value="n" /></el-select>
        </el-form-item>
        <el-form-item v-if="['daily','weekly','monthly','hours'].includes(form.schedule_type)" label="分钟">
          <el-select v-model="form.minute" style="width:100%"><el-option v-for="n in minutes" :key="n" :label="pad(n)" :value="n" /></el-select>
        </el-form-item>
        <el-form-item v-if="form.schedule_type === 'minutes'" label="间隔分钟">
          <el-input-number v-model="form.interval_minutes" :min="1" :max="1440" style="width:100%" />
        </el-form-item>
        <el-form-item v-if="form.schedule_type === 'hours'" label="间隔小时">
          <el-input-number v-model="form.interval_hours" :min="1" :max="24" style="width:100%" />
        </el-form-item>
      </div>

      <div class="form-grid">
        <el-form-item label="超时秒数"><el-input-number v-model="form.timeout_seconds" :min="1" style="width:100%" /></el-form-item>
        <el-form-item label="失败重试"><el-input-number v-model="form.retry_count" :min="0" style="width:100%" /></el-form-item>
      </div>
      <el-form-item><el-switch v-model="form.enabled" active-text="启用" inactive-text="停用" /></el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="dialog=false">取消</el-button>
      <el-button type="primary" @click="save">保存</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, RefreshCw } from 'lucide-vue-next'
import { deleteData, getData, postData, putData } from '@/api/client'

defineOptions({ name: 'Tasks' })

const tasks = ref<any[]>([])
const templates = ref<any[]>([])
const dialog = ref(false)
const form = ref<any>({})
const strmConfig = ref<any>({})
const taskStatuses = ref<Record<string, any>>({})
let statusTimer: number | null = null

const weekdays = [
  { label: '周日', value: 0 },
  { label: '周一', value: 1 },
  { label: '周二', value: 2 },
  { label: '周三', value: 3 },
  { label: '周四', value: 4 },
  { label: '周五', value: 5 },
  { label: '周六', value: 6 }
]
const hours = Array.from({ length: 24 }, (_, i) => i)
const minutes = Array.from({ length: 60 }, (_, i) => i)

async function load() {
  const [taskList, tplList, cfg]: any[] = await Promise.all([
    getData('/tasks'),
    getData('/tasks/templates'),
    getData('/strm302/config')
  ])
  tasks.value = taskList
  templates.value = tplList
  strmConfig.value = cfg || {}
  await loadStatuses()
}

async function loadStatuses() {
  if (!tasks.value.length) {
    taskStatuses.value = {}
    return
  }
  const pairs = await Promise.all(tasks.value.map(async (task) => {
    try {
      const status = await getData(`/tasks/${task.id}/status`)
      return [String(task.id), status]
    } catch {
      return [String(task.id), null]
    }
  }))
  taskStatuses.value = Object.fromEntries(pairs.filter(([, status]) => status))
}

function openCreate(seed: any = {}) {
  form.value = normalizeForm({
    name: '',
    type: 'strm_scan',
    cron_expr: '30 17 * * *',
    params_json: '',
    enabled: true,
    timeout_seconds: 3600,
    retry_count: 0,
    scan_source: strmConfig.value.scan_source || 'cd2',
    scan_mode: 'incremental',
    cid: strmConfig.value.root_cid || '',
    source_path: '',
    schedule_type: 'daily',
    hour: 17,
    minute: 30,
    weekday: [1],
    month_day: 1,
    interval_minutes: 30,
    interval_hours: 1,
    keep_days: 3,
    prefix: '',
    ...seed
  })
  dialog.value = true
}

function openEdit(row: any) {
  form.value = normalizeForm({ ...row })
  dialog.value = true
}

function fromTemplate(tpl: any) { openCreate(tpl) }

async function save() {
  const payload = toTaskPayload(form.value)
  if (!String(form.value.name || '').trim()) return ElMessage.warning('请输入任务名称')
  if (payload.type === 'strm_scan' && payload.params.scan_source === '115' && !payload.params.cid) {
    return ElMessage.warning('请输入 115 目录 CID')
  }
  const body = {
    ...form.value,
    type: payload.type,
    cron_expr: payload.cron,
    params_json: JSON.stringify(payload.params),
    timeout_seconds: Number(form.value.timeout_seconds || 3600),
    retry_count: Number(form.value.retry_count || 0)
  }
  if (form.value.id) await putData(`/tasks/${form.value.id}`, body)
  else await postData('/tasks', body)
  dialog.value = false
  await load()
}

async function run(row: any) {
  await postData(`/tasks/${row.id}/run`)
  ElMessage.success('任务已开始')
  await loadStatuses()
}
async function toggle(row: any) { await postData(`/tasks/${row.id}/toggle`, { enabled: !row.enabled }); await load() }
async function remove(row: any) {
  await ElMessageBox.confirm(`删除任务 ${row.name}？`, '确认删除')
  await deleteData(`/tasks/${row.id}`)
  await load()
}

function normalizeForm(raw: any) {
  const params = safeJSON(raw.params_json)
  const schedule = parseCron(raw.cron_expr || '30 17 * * *')
  const type = raw.type === 'incremental' ? 'strm_scan' : (raw.type || 'strm_scan')
  return {
    ...raw,
    type,
    scan_source: params.scan_source || (raw.type === 'incremental' ? '115' : strmConfig.value.scan_source || 'cd2'),
    scan_mode: params.scan_mode || 'incremental',
    cid: params.cid || params.start_cid || strmConfig.value.root_cid || '',
    source_path: params.source_path || '',
    prefix: params.prefix || '',
    keep_days: params.keep_days || 3,
    schedule_type: schedule.schedule_type,
    hour: schedule.hour,
    minute: schedule.minute,
    weekday: schedule.weekday,
    month_day: schedule.month_day,
    interval_minutes: schedule.interval_minutes,
    interval_hours: schedule.interval_hours
  }
}

function toTaskPayload(value: any) {
  const type = value.type || 'strm_scan'
  if (type === 'hash_patrol') {
    return { type, cron: buildCron(value), params: { prefix: normalizeOutputPath(value.prefix || '') } }
  }
  if (type === 'recycle_clean') {
    return { type, cron: buildCron(value), params: { keep_days: Number(value.keep_days || 3) } }
  }
  const source = value.scan_source || 'cd2'
  return {
    type: 'strm_scan',
    cron: buildCron(value),
    params: {
      scan_source: source,
      scan_mode: value.scan_mode || 'incremental',
      cid: source === '115' ? String(value.cid || '').trim() : '',
      source_path: normalizeOutputPath(value.source_path || '')
    }
  }
}

function buildCron(value: any) {
  const minute = Number(value.minute || 0)
  const hour = Number(value.hour || 0)
  if (value.schedule_type === 'weekly') return `${minute} ${hour} * * ${weekdayCronValue(value.weekday)}`
  if (value.schedule_type === 'monthly') return `${minute} ${hour} ${Number(value.month_day || 1)} * *`
  if (value.schedule_type === 'minutes') return `*/${Number(value.interval_minutes || 30)} * * * *`
  if (value.schedule_type === 'hours') return `${minute} */${Number(value.interval_hours || 1)} * * *`
  return `${minute} ${hour} * * *`
}

function parseCron(raw: string) {
  const parts = String(raw || '').trim().split(/\s+/)
  const fallback = { schedule_type: 'daily', hour: 17, minute: 30, weekday: [1], month_day: 1, interval_minutes: 30, interval_hours: 1 }
  if (parts.length !== 5) return fallback
  const [min, hour, dom, , dow] = parts
  if (min.startsWith('*/')) return { ...fallback, schedule_type: 'minutes', interval_minutes: Number(min.slice(2)) || 30 }
  if (hour.startsWith('*/')) return { ...fallback, schedule_type: 'hours', minute: Number(min) || 0, interval_hours: Number(hour.slice(2)) || 1 }
  if (dom !== '*') return { ...fallback, schedule_type: 'monthly', minute: Number(min) || 0, hour: Number(hour) || 0, month_day: Number(dom) || 1 }
  if (dow !== '*') return { ...fallback, schedule_type: 'weekly', minute: Number(min) || 0, hour: Number(hour) || 0, weekday: parseWeekdays(dow) }
  return { ...fallback, schedule_type: 'daily', minute: Number(min) || 0, hour: Number(hour) || 0 }
}

function taskSummary(row: any) {
  const params = safeJSON(row.params_json)
  if (row.type === 'hash_patrol') return `Hash 巡检 ${params.prefix || '/'}`
  if (row.type === 'recycle_clean') return `回收站清理，保留 ${params.keep_days || 3} 天`
  const sourceText: Record<string, string> = { '115': '115 API', cd2: 'CD2', openlist: 'OpenList' }
  const modeText = params.scan_mode === 'full' ? '全量' : '增量'
  return `${sourceText[params.scan_source] || '115 API'} ${modeText} ${params.source_path || '/'}`
}

function scheduleSummary(cron: string) {
  const s = parseCron(cron)
  if (s.schedule_type === 'weekly') return `每周${weekdaySummary(s.weekday)} ${pad(s.hour)}:${pad(s.minute)}`
  if (s.schedule_type === 'monthly') return `每月 ${s.month_day} 日 ${pad(s.hour)}:${pad(s.minute)}`
  if (s.schedule_type === 'minutes') return `每 ${s.interval_minutes} 分钟`
  if (s.schedule_type === 'hours') return `每 ${s.interval_hours} 小时，第 ${pad(s.minute)} 分钟`
  return `每天 ${pad(s.hour)}:${pad(s.minute)}`
}

function taskStatus(row: any) {
  return taskStatuses.value[String(row.id)] || null
}

function taskLatestRun(row: any) {
  return taskStatus(row)?.latest_run || null
}

function taskStatusText(row: any) {
  const status = taskStatus(row)
  const run = status?.latest_run
  if (status?.running || run?.status === 'running') return '运行中'
  if (!run) return '未运行'
  const map: Record<string, string> = {
    success: '已完成',
    failed: '失败',
    stopped: '已停止',
    running: '运行中'
  }
  return map[run.status] || run.status || '未知'
}

function taskStatusTag(row: any) {
  const status = taskStatus(row)
  const run = status?.latest_run
  if (status?.running || run?.status === 'running') return 'primary'
  if (run?.status === 'success') return 'success'
  if (run?.status === 'failed') return 'danger'
  if (run?.status === 'stopped') return 'warning'
  return 'info'
}

function taskProgress(row: any) {
  const job = taskStatus(row)?.strm_job
  if (!job) return 0
  const total = job.total || job.plan_total || 0
  if (!total) return 0
  return Math.min(100, Math.round(((job.done || 0) + (job.failed || 0)) * 100 / total))
}

function taskProgressStatus(row: any) {
  const job = taskStatus(row)?.strm_job
  if (!job) return undefined
  if (job.status === 'success') return 'success'
  if (job.status === 'failed') return 'exception'
  if (['partial', 'paused', 'stopped'].includes(job.status)) return 'warning'
  return undefined
}

function taskProgressLine(row: any) {
  const job = taskStatus(row)?.strm_job
  if (!job) return ''
  const total = job.total || job.plan_total || 0
  const message = job.message || phaseText(job.phase)
  const download = job.download_total ? `，下载 ${job.download_done || 0}/${job.download_total}` : ''
  return `${phaseText(job.phase)} · ${message} · 处理 ${job.done || 0}/${total}${download}`
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

function safeJSON(raw: string) {
  try { return raw ? JSON.parse(raw) : {} } catch { return {} }
}

function normalizeOutputPath(raw: string) {
  const value = String(raw || '').trim().replace(/\\/g, '/')
  if (!value) return ''
  return value.startsWith('/') ? value : `/${value}`
}

function weekdayCronValue(raw: any) {
  const days = normalizeWeekdays(raw)
  return days.length ? days.join(',') : '1'
}

function parseWeekdays(raw: string) {
  const values: number[] = []
  for (const part of String(raw || '').split(',')) {
    const item = part.trim()
    if (!item) continue
    const range = item.match(/^(\d+)-(\d+)$/)
    if (range) {
      const start = Number(range[1])
      const end = Number(range[2])
      for (let n = Math.min(start, end); n <= Math.max(start, end); n++) values.push(n)
      continue
    }
    values.push(Number(item))
  }
  return normalizeWeekdays(values)
}

function normalizeWeekdays(raw: any) {
  const values = Array.isArray(raw) ? raw : [raw]
  const seen = new Set<number>()
  const out: number[] = []
  for (const item of values) {
    const n = Number(item)
    if (!Number.isInteger(n) || n < 0 || n > 6 || seen.has(n)) continue
    seen.add(n)
    out.push(n)
  }
  return out
}

function weekdaySummary(raw: any) {
  const days = normalizeWeekdays(raw)
  if (!days.length) return '一'
  return days
    .map((value) => weekdays.find((x) => x.value === value)?.label.replace('周', '') || '')
    .filter(Boolean)
    .join('、')
}

function pad(n: number) { return String(n).padStart(2, '0') }

onMounted(async () => {
  await load()
  statusTimer = window.setInterval(loadStatuses, 3000)
})
onBeforeUnmount(() => {
  if (statusTimer) window.clearInterval(statusTimer)
})
</script>

<style scoped>
.mini { margin-top: 8px; }
.task-status { display: grid; gap: 6px; }
.task-status-line { display: flex; align-items: center; gap: 8px; }
</style>
