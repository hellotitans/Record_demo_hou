import type { DecisionRecord } from '../types'

interface FollowUpProps {
  record: DecisionRecord
  onAnswer: (regret: 'yes' | 'no') => void
}

// 三个月回访卡：唯一抄不走的数据资产。点选后把结果交回上层落库。
export function FollowUp({ record, onAnswer }: FollowUpProps) {
  return (
    <div className="mt-2 rounded-xl border border-violet-200 bg-violet-50 p-3" data-testid="followup">
      <p className="text-sm text-violet-800">
        三个月过去了，你对「{record.question}」这个决定后悔吗？
      </p>
      <div className="mt-2 flex gap-2">
        <button
          type="button"
          aria-label="回访-后悔"
          onClick={() => onAnswer('yes')}
          className="rounded-lg bg-violet-600 px-3 py-1.5 text-sm text-white hover:bg-violet-700"
        >
          后悔
        </button>
        <button
          type="button"
          aria-label="回访-不后悔"
          onClick={() => onAnswer('no')}
          className="rounded-lg border border-violet-300 px-3 py-1.5 text-sm text-violet-700 hover:bg-violet-100"
        >
          不后悔
        </button>
      </div>
    </div>
  )
}
