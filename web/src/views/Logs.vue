<template>
  <section class="panel">
    <div class="toolbar log-toolbar">
      <el-date-picker
        v-model="query.timeRange"
        type="datetimerange"
        value-format="YYYY-MM-DD HH:mm:ss"
        range-separator="至"
        start-placeholder="开始时间"
        end-placeholder="结束时间"
        :clearable="false"
        @change="resetAndLoad"
      />
      <el-select v-model="query.type" style="width:150px" @change="resetAndLoad">
        <el-option v-for="item in typeOptions" :key="item" :label="item" :value="item" />
      </el-select>
      <el-input v-model="query.keyword" placeholder="搜索路径、接口、错误" clearable style="width:260px" @keyup.enter="resetAndLoad" @clear="resetAndLoad" />
      <el-button :icon="RefreshCw" @click="load">刷新</el-button>
      <el-button :icon="Download" @click="downloadLogs">下载</el-button>
      <el-button :type="streaming ? 'success' : 'default'" @click="toggleStream">{{ streaming ? '实时输出中' : '恢复实时输出' }}</el-button>
    </div>

    <div class="log-list">
      <div v-for="item in logs" :key="`${item.time}-${item.type}-${item.path}-${item.error}`" class="log-row">
        <span class="code">{{ new Date(item.time).toLocaleString() }}</span>
        <el-tag size="small" :type="tagType(item.type)">{{ item.type }}</el-tag>
        <span class="code">{{ item.path || '-' }}</span>
        <span v-if="item.type === '接口错误'" class="muted">{{ item.method }} {{ item.status }} {{ item.error }}</span>
      </div>
      <el-empty v-if="!logs.length" description="暂无审计日志" />
    </div>

    <div class="pager">
      <el-pagination
        v-model:current-page="query.page"
        v-model:page-size="query.pageSize"
        :page-sizes="[30, 50, 100, 200]"
        :total="query.total"
        layout="total, sizes, prev, pager, next, jumper"
        @size-change="handlePageSizeChange"
        @current-change="load"
      />
    </div>
  </section>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { Download, RefreshCw } from 'lucide-vue-next'
import { getData } from '@/api/client'
import { useAuthStore } from '@/stores/auth'

defineOptions({ name: 'Logs' })

const typeOptions = ['全部', '接口错误', 'Emby通知', '定时任务', 'STRM创建', 'STRM更新', 'STRM删除', '元数据创建', '元数据更新', '元数据删除', '元数据移动', '回收站移入', '回收站清理', '空目录删除']
const logs = ref<any[]>([])
const streaming = ref(false)
const auth = useAuthStore()
const query = reactive({
  timeRange: defaultTimeRange(),
  type: '全部',
  keyword: '',
  page: 1,
  pageSize: 50,
  total: 0
})
let source: EventSource | null = null

async function load() {
  const data: any = await getData('/audit/logs', params())
  const items = Array.isArray(data) ? data : data?.items
  logs.value = Array.isArray(items) ? items.filter(Boolean) : []
  query.total = Number(data?.total || logs.value.length || 0)
}

function resetAndLoad() {
  query.page = 1
  load()
}

function handlePageSizeChange() {
  query.page = 1
  load()
}

function toggleStream() {
  if (source) {
    closeStream()
    return
  }
  startStream()
}

function startStream() {
  if (source || !auth.token) return
  source = new EventSource(`/api/audit/logs/stream?token=${encodeURIComponent(auth.token)}`)
  streaming.value = true
  source.addEventListener('audit', (event) => {
    const parsed = JSON.parse((event as MessageEvent).data)
    if (parsed && matchesCurrentFilter(parsed)) {
      if (query.page !== 1) {
        query.total++
        return
      }
      logs.value.unshift(parsed)
      query.total++
      if (logs.value.length > query.pageSize) logs.value.pop()
    }
  })
  source.onerror = () => {
    closeStream()
  }
}

function closeStream() {
  if (source) {
    source.close()
    source = null
  }
  streaming.value = false
}

function downloadLogs() {
  const search = new URLSearchParams({ ...params(), token: auth.token, limit: '50000', offset: '0' })
  window.location.href = `/api/audit/logs/download?${search.toString()}`
}

function params() {
  return {
    start: query.timeRange?.[0] || '',
    end: query.timeRange?.[1] || '',
    type: query.type === '全部' ? '' : query.type,
    keyword: query.keyword || '',
    limit: String(query.pageSize),
    offset: String((query.page - 1) * query.pageSize)
  }
}

function matchesCurrentFilter(item: any) {
  if (!item) return false
  if (query.type !== '全部' && item.type !== query.type) return false
  const t = new Date(item.time).getTime()
  const start = query.timeRange?.[0] ? new Date(query.timeRange[0]).getTime() : Number.NEGATIVE_INFINITY
  const end = query.timeRange?.[1] ? new Date(query.timeRange[1]).getTime() : Number.POSITIVE_INFINITY
  if (Number.isFinite(start) && t < start) return false
  if (Number.isFinite(end) && t > end) return false
  const keyword = query.keyword.trim().toLowerCase()
  if (!keyword) return true
  return `${item.type} ${item.path || ''} ${item.method || ''} ${item.error || ''}`.toLowerCase().includes(keyword)
}

function tagType(type: string) {
  type = type || ''
  if (type === '接口错误') return 'danger'
  if (type === 'Emby通知') return 'primary'
  if (type === '定时任务') return 'primary'
  if (type.includes('删除')) return 'warning'
  if (type.includes('创建')) return 'success'
  return 'info'
}

function defaultTimeRange() {
  const now = new Date()
  const start = new Date(now)
  start.setHours(0, 0, 0, 0)
  const end = new Date(now)
  end.setHours(23, 59, 59, 0)
  return [formatDateTime(start), formatDateTime(end)]
}

function formatDateTime(date: Date) {
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`
}

onMounted(async () => {
  await load()
  startStream()
})
onBeforeUnmount(closeStream)
</script>

<style scoped>
.log-toolbar { align-items: center; flex-wrap: wrap; }
.log-list { display: grid; gap: 6px; max-height: calc(100vh - 246px); overflow: auto; }
.log-row { display: grid; grid-template-columns: 190px 110px minmax(240px, 1fr) minmax(0, 1fr); gap: 10px; align-items: start; padding: 8px 10px; background: var(--panel-soft); border: 1px solid var(--line); border-radius: 6px; }
.pager { display: flex; justify-content: flex-end; padding-top: 12px; }
</style>
