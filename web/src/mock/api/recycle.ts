import type { IRecycleApi } from '@/api/interfaces/recycle'
import type { RecycleItem, RecycleQuery, RestoreRequest, RestoreResponse, RestoreResult } from '@/api/types/recycle'
import { recycleMockItems } from '@/mock/data/recycle'

/** 隔离开发模拟，不执行真实磁盘操作。 */
export class MockRecycleApi implements IRecycleApi {
  private items: RecycleItem[]
  constructor(items = recycleMockItems) { this.items = [...items] }
  /** 模拟分页。 */
  async list(query: RecycleQuery) {
    return { items: this.items.slice(query.offset, query.offset + query.limit), total: this.items.length }
  }
  /** 模拟恢复后移出列表。 */
  async restore(request: RestoreRequest): Promise<RestoreResponse> {
    const results: RestoreResult[] = [...new Set(request.ids)].map(id => {
      const item = this.items.find(row => row.id === id)
      return { id, status: item ? 'restored' : 'not_found', relative_path: item?.deleted_relative_path ?? '', message: item ? '模拟恢复成功' : '记录不存在' }
    })
    this.items = this.items.filter(item => !request.ids.includes(item.id))
    const restored = results.filter(item => item.status === 'restored').length
    return { requested: results.length, restored, already_restored: 0, conflicts: 0, failed: results.length - restored, results }
  }
}
