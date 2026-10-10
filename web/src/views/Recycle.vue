<script setup lang="ts">
import { RefreshCw, Undo2 } from 'lucide-vue-next'
import { useRecycle } from '@/composables/useRecycle'
import RecycleRestoreResults from '@/components/recycle/RecycleRestoreResults.vue'

defineOptions({ name: 'Recycle' })
const { items, selected, page, pageSize, total, loading, tableKey, restoring, busy, processed, restoreTotal, results, resultVisible, incomplete, load, select, changeSize, restore } = useRecycle()
/** 按浏览器时区显示回收时间。 */
function formatTime(value?: string) { return value ? new Date(value).toLocaleString() : '-' }
/** 格式化字节数。 */
function formatSize(value: number) {
  const size = Number(value || 0)
  if (size < 1024) return size + ' B'
  if (size < 1024 * 1024) return (size / 1024).toFixed(1) + ' KB'
  return (size / 1024 / 1024).toFixed(1) + ' MB'
}
</script>

<template>
  <section class="panel">
    <div class="job-head">
      <div>
        <h2 class="page-title">回收站</h2>
        <p class="muted">暂存副本超过 3 天后自动清理。恢复到原路径，不覆盖已有不同内容文件。</p>
      </div>
      <div class="recycle-actions">
        <span class="muted">已选择 {{ selected.length }} 项</span>
        <el-button type="primary" :icon="Undo2" :loading="restoring" :disabled="loading || !selected.length" @click="restore">恢复</el-button>
        <el-button :icon="RefreshCw" :disabled="busy" @click="load">刷新</el-button>
      </div>
    </div>

    <p v-if="restoring" role="status">正在恢复：已处理 {{ processed }} / {{ restoreTotal }} 项</p>
    <el-table :key="tableKey" v-loading="loading" :data="items" row-key="id" border height="calc(100vh - 280px)" empty-text="暂无待恢复文件" @selection-change="select">
      <el-table-column type="selection" width="48" :selectable="() => !busy" />
      <el-table-column label="删除时间" width="180">
        <template #default="{ row }">{{ formatTime(row.deleted_at) }}</template>
      </el-table-column>
      <el-table-column label="过期时间" width="180">
        <template #default="{ row }">{{ formatTime(row.expires_at) }}</template>
      </el-table-column>
      <el-table-column prop="source_label" label="来源" width="180" />
      <el-table-column label="大小" width="120">
        <template #default="{ row }">{{ formatSize(row.size) }}</template>
      </el-table-column>
      <el-table-column prop="reason" label="原因" width="150" />
      <el-table-column prop="deleted_relative_path" label="原相对路径" min-width="340">
        <template #default="{ row }">
          <span class="code">{{ row.deleted_relative_path || '-' }}</span>
        </template>
      </el-table-column>
      <el-table-column prop="recycle_path" label="暂存路径" min-width="420">
        <template #default="{ row }">
          <span class="code">{{ row.recycle_path || '-' }}</span>
        </template>
      </el-table-column>
    </el-table>

    <div class="pager">
      <el-pagination
        v-model:current-page="page"
        v-model:page-size="pageSize"
        :disabled="busy"
        :page-sizes="[50, 200, 500, 1000]"
        :total="total"
        layout="total, sizes, prev, pager, next, jumper"
        @size-change="changeSize"
        @current-change="load"
      />
    </div>
    <RecycleRestoreResults v-model="resultVisible" :results="results" :incomplete="incomplete" />
  </section>
</template>

<style scoped>
.recycle-actions { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; }
</style>
