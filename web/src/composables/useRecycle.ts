import { computed, onMounted, onBeforeUnmount, shallowRef } from 'vue'
import { ElMessage } from 'element-plus'
import { recycleApi } from '@/api/modules/recycle'
import type { IRecycleApi } from '@/api/interfaces/recycle'
import type { RecycleItem, RestoreResult } from '@/api/types/recycle'

/** 管理列表、当前页选择和分批恢复状态。 */
export function useRecycle(api: IRecycleApi = recycleApi) {
  const items = shallowRef<RecycleItem[]>([])
  const selected = shallowRef<RecycleItem[]>([])
  const page = shallowRef(1)
  const pageSize = shallowRef(50)
  const total = shallowRef(0)
  const loading = shallowRef(false)
  const tableKey = shallowRef(0)
  const restoring = shallowRef(false)
  const processed = shallowRef(0)
  const restoreTotal = shallowRef(0)
  const results = shallowRef<RestoreResult[]>([])
  const resultVisible = shallowRef(false)
  const incomplete = shallowRef(false)
  const busy = computed(() => loading.value || restoring.value)
  let queryVersion = 0
  let disposed = false

  /** 刷新清空选择，并校正恢复后超出范围的页码。 */
  async function load() {
    if (restoring.value || disposed) return
    selected.value = []
    const version = ++queryVersion
    loading.value = true
    try {
      let data = await api.list({ limit: pageSize.value, offset: (page.value - 1) * pageSize.value })
      if (disposed || version !== queryVersion) return
      const lastPage = Math.max(1, Math.ceil(data.total / pageSize.value))
      if (page.value > lastPage) {
        page.value = lastPage
        data = await api.list({ limit: pageSize.value, offset: (page.value - 1) * pageSize.value })
      }
      if (disposed || version !== queryVersion) return
      items.value = data.items ?? []
      // Element Plus 按 row-key 可能保留旧选择；重建表格确保视觉与提交状态一致。
      tableKey.value++
      total.value = data.total
    } catch (error) {
      if (!disposed && version === queryVersion) {
        items.value = []
        tableKey.value++
        ElMessage.error(error instanceof Error ? error.message : '回收站加载失败')
      }
    } finally {
      if (version === queryVersion) loading.value = false
    }
  }
  /** 不保留跨页选择。 */
  function select(rows: RecycleItem[]) { if (!busy.value) selected.value = rows }
  /** 切换容量后从第一页查询。 */
  function changeSize() { page.value = 1; void load() }
  /** 顺序提交每批最多 50 项，异常时停止并提示核对。 */
  async function restore() {
    if (busy.value || !selected.value.length) return
    const ids = selected.value.map(item => item.id)
    restoring.value = true
    processed.value = 0
    restoreTotal.value = ids.length
    results.value = []
    incomplete.value = false
    try {
      for (let offset = 0; offset < ids.length; offset += 50) {
        if (disposed) { incomplete.value = true; break }
        const response = await api.restore({ ids: ids.slice(offset, offset + 50) })
        results.value = [...results.value, ...response.results]
        processed.value += response.requested
      }
    } catch (error) {
      incomplete.value = true
      if (!disposed) ElMessage.warning(`恢复结果未完整获取，请刷新核对后重试。${error instanceof Error ? error.message : ''}`)
    } finally {
      restoring.value = false
      selected.value = []
      if (!disposed) { resultVisible.value = true; await load() }
    }
  }
  onMounted(load)
  onBeforeUnmount(() => { disposed = true; queryVersion++ })
  return { items, selected, page, pageSize, total, loading, tableKey, restoring, busy, processed, restoreTotal, results, resultVisible, incomplete, load, select, changeSize, restore }
}
