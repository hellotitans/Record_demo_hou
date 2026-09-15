import { motion } from 'framer-motion'
import type { ModeratorNote as Note } from '../types'

// 主持人每轮后的分歧挑明卡：居中、紫色，和两边发言的彩色气泡区分开。
export function ModeratorNote({ note }: { note: Note }) {
  const empty = note.unresolved.length === 0 && note.repeated.length === 0
  return (
    <motion.div
      initial={{ opacity: 0, y: 8 }}
      animate={{ opacity: 1, y: 0 }}
      className="mx-auto my-3 max-w-2xl rounded-2xl border border-violet-200 bg-violet-50 px-4 py-3 text-sm text-violet-900"
    >
      <div className="mb-1 font-semibold">主持人 · 第 {note.round} 轮</div>
      {note.unresolved.length > 0 && (
        <div className="mb-1">
          <span className="text-violet-500">未解分歧：</span>
          {note.unresolved.join('；')}
        </div>
      )}
      {note.repeated.length > 0 && (
        <div>
          <span className="text-violet-500">重复论点（已去重）：</span>
          {note.repeated.join('；')}
        </div>
      )}
      {empty && <div className="text-violet-400">本轮暂无需要挑明的分歧。</div>}
    </motion.div>
  )
}
