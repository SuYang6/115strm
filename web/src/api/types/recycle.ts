/** 回收站中可恢复的文件记录。 */
export interface RecycleItem {
  id: number
  deleted_file_name: string
  deleted_relative_path: string
  original_path: string
  recycle_path: string
  size: number
  reason: string
  source_label: string
  deleted_at: string
  expires_at?: string
  restored_at?: string
}
/** 沿用 limit/offset 的列表参数。 */
export interface RecycleQuery { limit: number; offset: number }
/** 回收站分页结果。 */
export interface RecycleList { items: RecycleItem[]; total: number }
/** 仅提交记录 ID，不接受客户端路径。 */
export interface RestoreRequest { ids: number[] }
/** 单项恢复结果。 */
export interface RestoreResult {
  id: number
  status: 'restored' | 'already_restored' | 'conflict' | 'source_missing' | 'not_found' | 'purged' | 'invalid_path' | 'failed'
  relative_path: string
  message: string
}
/** 单批次恢复汇总。 */
export interface RestoreResponse {
  requested: number
  restored: number
  already_restored: number
  conflicts: number
  failed: number
  results: RestoreResult[]
}
