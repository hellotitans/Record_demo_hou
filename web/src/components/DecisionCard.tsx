import { useRef } from 'react'
import { toPng } from 'html-to-image'
import type { DebateResult, Side } from '../types'
import { SIDE_LABEL } from '../types'

// 辩论结束后的决策卡片长图：把整场结论 + 暴露的假设收进一张可下载的图。
// 假设列表直接对接临界点计算器（P6 下一子阶段）。
export function DecisionCard({
  result,
  question,
}: {
  result: DebateResult
  question: string
}) {
  const ref = useRef<HTMLDivElement>(null)

  const finalOf = (side: Side) => {
    const ts = result.turns.filter((t) => t.side === side)
    return ts.length ? ts[ts.length - 1].content : '（本轮未发言）'
  }

  async function download() {
    if (!ref.current) return
    try {
      const url = await toPng(ref.current, { pixelRatio: 2, backgroundColor: '#ffffff' })
      const a = document.createElement('a')
      a.href = url
      a.download = 'decision.png'
      a.click()
    } catch {
      // 导出失败不应影响查看，静默忽略
    }
  }

  return (
    <div className="mt-6">
      <div ref={ref} className="rounded-3xl border border-slate-200 bg-white p-6 shadow-xl">
        <div className="text-xs font-medium text-slate-400">决策卡片</div>
        <h2 className="mt-1 text-lg font-bold text-slate-800">{question}</h2>

        <div className="mt-4 grid grid-cols-1 gap-3 sm:grid-cols-2">
          {(['data', 'life'] as Side[]).map((side) => (
            <div
              key={side}
              className={`rounded-2xl p-3 text-sm ${
                side === 'data' ? 'bg-data-soft text-data-deep' : 'bg-life-soft text-life-deep'
              }`}
            >
              <div className="mb-1 font-semibold">{SIDE_LABEL[side]}的最终立场</div>
              <div className="leading-relaxed">{finalOf(side)}</div>
            </div>
          ))}
        </div>

        {result.assumptions.length > 0 && (
          <div className="mt-4">
            <div className="mb-1 text-sm font-semibold text-slate-700">
              辩论暴露的关键假设（喂给临界点计算器）
            </div>
            <div className="space-y-2">
              {result.assumptions.map((a) => (
                <div key={a.id} className="rounded-xl bg-amber-50 px-3 py-2 text-sm text-amber-900">
                  <span className="font-medium">{SIDE_LABEL[a.side]}</span>：{a.statement}
                  <div className="mt-0.5 text-amber-700">
                    当 {a.variable} {a.operator} {a.value}
                    {a.unit} 时，结论可能翻转
                  </div>
                </div>
              ))}
            </div>
          </div>
        )}

        <div className="mt-4 text-[11px] text-slate-400">
          成本 ¥{result.usage.estimated_cost_cny.toFixed(4)} · 耗时{' '}
          {(result.usage.total_latency_ms / 1000).toFixed(1)}s ·{' '}
          {result.usage.prompt_tokens + result.usage.completion_tokens} tokens
        </div>
      </div>
      <button
        onClick={download}
        className="mt-3 rounded-xl bg-slate-800 px-4 py-2 text-sm font-medium text-white transition hover:bg-slate-700"
      >
        下载决策长图
      </button>
    </div>
  )
}
