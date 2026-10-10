import { getData, postData } from '@/api/client'
import type { IRecycleApi } from '@/api/interfaces/recycle'
import type { RecycleList, RecycleQuery, RestoreRequest, RestoreResponse } from '@/api/types/recycle'
import { MockRecycleApi } from '@/mock/api/recycle'

/** 复用现有客户端登录及错误处理。 */
class RealRecycleApi implements IRecycleApi {
  /** 查询待恢复文件。 */
  list(query: RecycleQuery) { return getData<RecycleList>('/recycle', query) }
  /** 恢复选中记录。 */
  restore(request: RestoreRequest) { return postData<RestoreResponse>('/recycle/restore', request) }
}
/** 生产固定使用真实接口，仅开发环境可显式开启模拟。 */
export function createRecycleApi(): IRecycleApi {
  return import.meta.env.DEV && import.meta.env.VITE_MOCK_ENABLED === 'true' ? new MockRecycleApi() : new RealRecycleApi()
}
export const recycleApi = createRecycleApi()
