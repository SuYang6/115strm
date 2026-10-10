import type { RecycleList, RecycleQuery, RestoreRequest, RestoreResponse } from '@/api/types/recycle'

/** 回收站真实实现与开发模拟共用的契约。 */
export interface IRecycleApi {
  /** 查询待恢复文件。 */
  list(query: RecycleQuery): Promise<RecycleList>
  /** 批量恢复并返回逐项结果。 */
  restore(request: RestoreRequest): Promise<RestoreResponse>
}
