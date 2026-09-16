import { useMemo, useState } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import type { Assumption, Side } from '../types'
import { SIDE_LABEL } from '../types'
import {
  groupByVariable,
  groupRange,
  computeWinner,
  flipHint,
  defaultValues,
} from '../lib/critical'

// 结论卡片的配色：用 data/life 调色板 + 平局/双崩的中性色。
const winnerTone: Record<string, string> = {
  data: 'bg-data-soft text-data-deep border-data-deep',
  life: 'bg-life-soft text-life-deep border-life-deep',
  tie: 'bg-slate-100 text-slate-600 border-slate-300',
  'both-broken': 'bg-rose-50 text-rose-700 border-rose-300',
}

// 临界点计算器 UI：把辩论暴露的假设变成可拖拽的"结论翻转沙盘"。
// 每个变量一根滑块，实时显示哪一方论证先崩、结论何时翻转。
export function CriticalCalculator({ assumptions }: { assumptions: Assumption[] }) {
  // 后端已保证是数组，但跨进程 JSON 仍可能是 null（旧数据、Mock 降级等）。
  // for...of null 会抛 TypeError 并让 React 卸载整棵树，这里兜住。
  const list = assumptions ?? []
  const groups = useMemo(() => groupByVariable(list), [list])
  const [values, setValues] = useState<Record<string, number>>(() => defaultValues(list))

  const result = useMemo(() => computeWinner(list, values), [list, values])

  const onSlide = (variable: string, raw: string) =>
    setValues((prev) => ({ ...prev, [variable]: Number(raw) }))

  const reset = () => setValues(defaultValues(list))

  const winnerLabel =
    result.winner === 'tie'
      ? '平局'
      : result.winner === 'both-broken'
        ? '双双崩塌'
        : SIDE_LABEL[result.winner as Side]

  const subLine =
    result.winner === 'tie'
      ? '两边都还站在自己的假设线上。拖动滑块，看哪一方先塌。'
      : result.winner === 'both-broken'
        ? '现实同时击穿了双方的前提，原结论都不成立。'
        : `${winnerLabel} 的论证在当前取值下仍站得住。`

  return (
    <div className="mt-6 rounded-3xl border border-slate-200 bg-white p-6 shadow-xl">
      <div className="text-xs font-medium text-slate-400">临界点计算器</div>
      <h3 className="mt-1 text-lg font-bold text-slate-800">把假设拖成现实，看结论何时翻转</h3>

      <div className="mt-4 space-y-5">
        {groups.length === 0 && (
          <div className="rounded-2xl border border-dashed border-slate-300 px-4 py-3 text-sm text-slate-500">
            本场没有提取出可量化的假设，计算器暂无内容可显示。
          </div>
        )}
        {groups.map((g) => {
          const range = groupRange(g)
          const v = values[g.variable]
          const unit = g.assumptions[0]?.unit ?? ''
          return (
            <div key={g.variable}>
              <div className="flex items-baseline justify-between text-sm">
                <span className="font-semibold text-slate-700">{g.variable}</span>
                <span className="tabular-nums text-slate-500">
                  {v}
                  {unit}
                </span>
              </div>
              <input
                type="range"
                aria-label={`滑块 ${g.variable}`}
                className="mt-2 w-full accent-blue-500"
                min={range.min}
                max={range.max}
                step={range.step}
                value={v}
                onChange={(e) => onSlide(g.variable, e.target.value)}
              />
              <div className="mt-1 space-y-0.5 text-xs text-slate-500">
                {g.assumptions.map((a) => (
                  <div key={a.id}>
                    <span
                      className={
                        result.winner === a.side ? 'font-semibold text-slate-700' : ''
                      }
                    >
                      {SIDE_LABEL[a.side]}
                    </span>
                    ：{a.statement}（{flipHint(a)}）
                  </div>
                ))}
              </div>
            </div>
          )
        })}
      </div>

      <div className="mt-5">
        <AnimatePresence mode="wait">
          <motion.div
            key={result.winner}
            initial={{ opacity: 0, y: 6 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: -6 }}
            transition={{ duration: 0.18 }}
            className={`rounded-2xl border px-4 py-3 text-sm ${winnerTone[result.winner] ?? winnerTone.tie}`}
          >
            <div className="font-semibold">{winnerLabel}</div>
            <div className="mt-0.5 opacity-80">{subLine}</div>
          </motion.div>
        </AnimatePresence>
      </div>

      <button
        onClick={reset}
        className="mt-3 rounded-xl border border-slate-300 px-4 py-2 text-sm text-slate-600 transition hover:bg-slate-100"
      >
        复原到假设线
      </button>
    </div>
  )
}
