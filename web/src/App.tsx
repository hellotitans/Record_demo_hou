import { useCallback, useEffect, useRef, useState } from 'react'
import { streamDebate } from './lib/sse'
import type {
  Assignment,
  CategoryTemplate,
  DecisionRecord,
  DecisionStats,
  DebateResult,
  Dimension,
  Frame,
  LiveTurn,
  ModeratorNote as Note,
  Side,
  Turn,
} from './types'
import { SIDE_LABEL } from './types'
import { Avatar } from './components/Avatar'
import { DebateStream } from './components/DebateStream'
import { DecisionCard } from './components/DecisionCard'
import { CriticalCalculator } from './components/CriticalCalculator'
import { CategoryPicker } from './components/CategoryPicker'
import { DimensionEditor } from './components/DimensionEditor'
import { DecisionArchive } from './components/DecisionArchive'
import { StatsBoard } from './components/StatsBoard'
import { fetchStats } from './lib/decisionsApi'
import { hydrateArchive, loadArchive, pushDecision, pushFollowup } from './lib/archive'

type Status = 'idle' | 'streaming' | 'done' | 'error'

export default function App() {
  const [question, setQuestion] = useState('')
  const [optA, setOptA] = useState('')
  const [optB, setOptB] = useState('')
  const [status, setStatus] = useState<Status>('idle')
  const [errorMsg, setErrorMsg] = useState('')
  const [assignments, setAssignments] = useState<Assignment[]>([])
  const [turns, setTurns] = useState<Turn[]>([])
  const [notes, setNotes] = useState<Note[]>([])
  const [live, setLive] = useState<LiveTurn | null>(null)
  const [result, setResult] = useState<DebateResult | null>(null)
  const [labels, setLabels] = useState<Record<string, string>>({})
  const [category, setCategory] = useState<string | null>(null)
  const [dimensions, setDimensions] = useState<Dimension[]>([])
  const [archive, setArchive] = useState<DecisionRecord[]>(() => loadArchive())
  const [stats, setStats] = useState<DecisionStats | null>(null)
  const ctrlRef = useRef<AbortController | null>(null)
  // 本场辩论的品类。不用 state 是因为 reset() 会立刻把 category 清掉，
  // 而落库发生在几十秒后的 done 帧 —— 那时候 state 早就是 null 了。
  const debateCategoryRef = useRef<string | null>(null)

  // 刷新看板统计。失败时 setStats(null)，看板整块不渲染，不打扰主流程。
  const refreshStats = useCallback(() => {
    void fetchStats().then(setStats)
  }, [])

  // 挂载时从后端水合档案：成功以远端为准，失败静默退回本地缓存。
  // 换一台设备打开，看到的还是同一份"你后悔吗"——这是 P7 的意义。
  useEffect(() => {
    let cancelled = false
    void hydrateArchive().then((list) => {
      if (!cancelled) setArchive(list)
    })
    void fetchStats().then((s) => {
      if (!cancelled) setStats(s)
    })
    return () => {
      cancelled = true
    }
  }, [])

  function reset() {
    setStatus('idle')
    setErrorMsg('')
    setAssignments([])
    setTurns([])
    setNotes([])
    setLive(null)
    setResult(null)
    setLabels({})
    setCategory(null)
    setDimensions([])
  }

  // 选中品类：自动预填问题/选项，并载入该品类的决策维度；选"自定义"则清空模板。
  function selectCategory(t: CategoryTemplate | null) {
    if (t) {
      setCategory(t.id)
      setDimensions(t.dimensions)
      setQuestion(t.sampleQuestion)
      setOptA(t.sampleOptions.a)
      setOptB(t.sampleOptions.b)
    } else {
      setCategory(null)
      setDimensions([])
    }
  }

  // 所有帧处理都用函数式 setState，避免流式高频回调里的旧状态闭包。
  function onFrame(f: Frame) {
    switch (f.kind) {
      case 'role_assign':
        setAssignments(f.data as Assignment[])
        break
      case 'turn_start':
        setLive({ round: f.round ?? 0, side: (f.side ?? 'data') as Side, content: '' })
        break
      case 'delta':
        setLive((prev) => (prev ? { ...prev, content: prev.content + (f.text ?? '') } : prev))
        break
      case 'turn_end':
        if (f.data) setTurns((prev) => [...prev, f.data as Turn])
        setLive(null)
        break
      case 'moderator':
        if (f.data) setNotes((prev) => [...prev, f.data as Note])
        break
      case 'done':
        if (f.data) {
          const res = f.data as DebateResult
          setResult(res)
          // 落库：先本地（后端没起也不丢），再推远端做跨设备同步。
          void pushDecision({
            id: res.dilemma_id,
            question: question.trim(),
            optionA: labels['a'] ?? '选项 A',
            optionB: labels['b'] ?? '选项 B',
            createdAt: new Date().toISOString(),
            assumptionCount: res.assumptions.length,
            // 带上品类，看板才能回答"哪类决策你最容易后悔"。
            // 老记录没有这个字段，归入"未分类"，不影响既有档案。
            ...(debateCategoryRef.current ? { category: debateCategoryRef.current } : {}),
          })
            .then(setArchive)
            .then(refreshStats)
        }
        setStatus('done')
        break
      case 'error':
        setStatus('error')
        setErrorMsg(typeof f.text === 'string' ? f.text : '辩论过程中出错')
        break
    }
  }

  async function start() {
    if (!question.trim() || !optA.trim() || !optB.trim()) return
    // 先把本场的品类/维度抓在手里：reset() 会清掉它们的 state，
    // 而请求体和落库都要用（落库更是要等到几十秒后的 done 帧）。
    const cat = category
    const dims = dimensions
    debateCategoryRef.current = cat
    reset()
    setStatus('streaming')
    const body: Record<string, unknown> = {
      id: globalThis.crypto?.randomUUID?.() ?? 'debate-' + Date.now(),
      question: question.trim(),
      options: [
        { id: 'a', label: optA.trim() },
        { id: 'b', label: optB.trim() },
      ],
    }
    // 品类与维度随请求一起发（后端 prompt.go 已会消费维度权重）。
    if (cat) body.category = cat
    if (dims.length > 0) body.dimensions = dims
    setLabels({ a: optA.trim(), b: optB.trim() })
    const ctrl = new AbortController()
    ctrlRef.current = ctrl
    await streamDebate('/api/debate', body, {
      onFrame,
      onError: (e) => {
        setStatus('error')
        setErrorMsg(e.message)
      },
      signal: ctrl.signal,
    })
  }

  function stop() {
    ctrlRef.current?.abort()
  }

  const canStart = question.trim() && optA.trim() && optB.trim()

  return (
    <div className="mx-auto flex min-h-full max-w-3xl flex-col px-4 py-8">
      <header className="mb-6 text-center">
        <h1 className="text-2xl font-bold text-slate-800">决策辩论 · 数据派 vs 生活派</h1>
        <p className="mt-1 text-sm text-slate-500">
          把你的两难交给两个角色互辩，最后用一张卡片收住结论与关键假设
        </p>
      </header>

      {status === 'idle' && (
        <>
          <form
            className="space-y-3 rounded-3xl border border-slate-200 bg-white/70 p-5 shadow-sm"
            onSubmit={(e) => {
              e.preventDefault()
              void start()
            }}
        >
          <CategoryPicker selectedId={category} onSelect={selectCategory} />

          <input
            className="w-full rounded-xl border border-slate-200 px-4 py-3 text-sm outline-none focus:border-blue-400"
            placeholder="你纠结的问题？例如：买 iPhone 还是安卓旗舰？"
            value={question}
            onChange={(e) => setQuestion(e.target.value)}
          />
          <div className="grid grid-cols-2 gap-3">
            <input
              className="rounded-xl border border-slate-200 px-4 py-3 text-sm outline-none focus:border-blue-400"
              placeholder="选项 A"
              value={optA}
              onChange={(e) => setOptA(e.target.value)}
            />
            <input
              className="rounded-xl border border-slate-200 px-4 py-3 text-sm outline-none focus:border-orange-400"
              placeholder="选项 B"
              value={optB}
              onChange={(e) => setOptB(e.target.value)}
            />
          </div>

          <DimensionEditor dimensions={dimensions} onChange={setDimensions} />
          <button
            type="submit"
            disabled={!canStart}
            className="w-full rounded-xl bg-slate-800 py-3 text-sm font-medium text-white transition enabled:hover:bg-slate-700 disabled:opacity-40"
          >
            开始辩论
          </button>
        </form>

        <div className="mt-4">
          <StatsBoard stats={stats} />
        </div>

        <DecisionArchive
          records={archive}
          onFollowup={(id, regret) => {
            void pushFollowup(id, regret)
              .then(setArchive)
              .then(refreshStats)
          }}
        />
        </>
      )}

      {status !== 'idle' && (
        <div className="rounded-3xl border border-slate-200 bg-white/70 p-5 shadow-sm">
          <div className="mb-4 flex items-center justify-center gap-8">
            {assignments.length === 0 ? (
              <>
                <Avatar side="data" speaking={!!live && live.side === 'data'} />
                <Avatar side="life" speaking={!!live && live.side === 'life'} />
              </>
            ) : (
              assignments.map((a) => (
                <div key={a.side} className="flex flex-col items-center gap-1">
                  <Avatar side={a.side} speaking={!!live && live.side === a.side} />
                  <div className="text-center text-xs text-slate-600">
                    <div className="font-semibold">{SIDE_LABEL[a.side]}</div>
                    <div className="opacity-70">{labels[a.option_id] ?? a.option_id}</div>
                  </div>
                </div>
              ))
            )}
          </div>

          <DebateStream turns={turns} notes={notes} live={live} />

          {status === 'error' && (
            <div className="mt-4 rounded-xl bg-rose-50 px-4 py-3 text-sm text-rose-700">
              出错了：{errorMsg}
            </div>
          )}

          {status === 'done' && result && (
            <>
              <DecisionCard result={result} question={question} />
              {result.assumptions.length > 0 && (
                <CriticalCalculator assumptions={result.assumptions} />
              )}
            </>
          )}

          <div className="mt-5 flex justify-center gap-3">
            {status === 'streaming' && (
              <button
                onClick={stop}
                className="rounded-xl border border-slate-300 px-4 py-2 text-sm text-slate-600 hover:bg-slate-100"
              >
                停止
              </button>
            )}
            {(status === 'done' || status === 'error') && (
              <button
                onClick={reset}
                className="rounded-xl bg-slate-800 px-4 py-2 text-sm font-medium text-white hover:bg-slate-700"
              >
                重新辩论
              </button>
            )}
          </div>
        </div>
      )}
    </div>
  )
}
