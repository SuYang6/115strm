<script setup lang="ts">
import { computed } from 'vue'
import type { RestoreResult } from '@/api/types/recycle'

/** 展示已返回结果，不将网络中断视为全部恢复失败。 */
const props = defineProps<{ results: RestoreResult[]; incomplete: boolean }>()
const visible = defineModel<boolean>({ required: true })
const summary = computed(() => {
  const restored = props.results.filter(row => row.status === 'restored').length
  const already = props.results.filter(row => row.status === 'already_restored').length
  const conflict = props.results.filter(row => row.status === 'conflict').length
  return `成功 ${restored} 项、已恢复 ${already} 项、冲突 ${conflict} 项、失败 ${props.results.length - restored - already - conflict} 项`
})
const labels: Record<RestoreResult['status'], string> = {
  restored: '成功', already_restored: '已恢复', conflict: '冲突', source_missing: '暂存缺失',
  not_found: '记录不存在', purged: '已清理', invalid_path: '路径无效', failed: '失败'
}
</script>
<template>
  <el-dialog v-model="visible" title="恢复结果" width="min(960px, 94vw)">
    <p>{{ summary }}</p>
    <el-alert v-if="incomplete" title="部分结果未完整获取，请刷新核对后重试尚未恢复的记录。" type="warning" :closable="false" />
    <el-table :data="results" row-key="id" max-height="420" empty-text="没有已返回的结果">
      <el-table-column label="结果" width="120"><template #default="{ row }">{{ labels[row.status as RestoreResult['status']] }}</template></el-table-column>
      <el-table-column prop="relative_path" label="原相对路径" min-width="260" />
      <el-table-column prop="message" label="说明" min-width="280" />
    </el-table>
    <template #footer><el-button @click="visible = false">关闭</el-button></template>
  </el-dialog>
</template>
