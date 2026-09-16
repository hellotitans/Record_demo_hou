import { motion } from 'framer-motion'
import type { ModeratorNote as Note } from '../types'

// 主持人每轮后的分歧挑明卡：居中、紫色，和两边发言的彩色气泡区分开。
export function ModeratorNote({ note }: { note: Note }) {
  // 后端已保证这两个字段是数组，但 JSON 跨进程传来仍可能是 null
  // （旧版本、代理改写、Mock 降级分支都可能）。这里再兜一层：
  // 在 null 上取 .length 会抛 TypeError，React 会因此卸载整棵树变成白屏。
  const unresolved = note.unresolved ?? []
  const repeated = note.repeated ?? []
  const empty = unresolved.length === 0 && repeated.length === 0
  return (
    <motion.div
      initial={{ opacity: 0, y: 8 }}
      animate={{ opacity: 1, y: 0 }}
      className="mx-auto my-3 max-w-2xl rounded-2xl border border-violet-200 bg-violet-50 px-4 py-3 text-sm text-violet-900"
    >
      <div className="mb-1 font-semibold">主持人 · 第 {note.round} 轮</div>
      {unresolved.length > 0 && (
        <div className="mb-1">
          <span className="text-violet-500">未解分歧：</span>
          {unresolved.join('；')}
        </div>
      )}
      {repeated.length > 0 && (
        <div>
          <span className="text-violet-500">重复论点（已去重）：</span>
          {repeated.join('；')}
        </div>
      )}
      {empty && <div className="text-violet-400">本轮暂无需要挑明的分歧。</div>}
    </motion.div>
  )
}
