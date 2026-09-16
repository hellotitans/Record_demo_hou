import { Component, type ErrorInfo, type ReactNode } from 'react'

// 全局错误边界：拦住任何未被捕获的渲染异常。
//
// 为什么必须有它：React 18 遇到未捕获异常时会**卸载整棵组件树**，
// 用户看到的就是一片纯白 —— 没有报错、没有提示、控制台之外什么线索都没有。
// 2026-09-16 实测踩到：主持人帧的 unresolved 字段是 null，
// 组件里 `note.unresolved.length` 抛 TypeError，整页瞬间变白。
// 这类问题排查成本极高，因为现象（白屏）完全不指向原因（某个字段是 null）。
//
// 有了这层，最坏情况也只是显示一段可读的错误卡片 + 堆栈，
// 用户能直接把原因发过来，而不是只能描述"页面白了"。
type Props = { children: ReactNode }
type State = { error: Error | null; info: ErrorInfo | null }

export class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null, info: null }

  static getDerivedStateFromError(error: Error): Partial<State> {
    return { error }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    this.setState({ info })
    // 同步到控制台，方便开发者对照 React 的组件栈
    console.error('[ErrorBoundary] 捕获到渲染异常：', error, info.componentStack)
  }

  private reset = () => this.setState({ error: null, info: null })

  render() {
    const { error, info } = this.state
    if (!error) return this.props.children

    return (
      <div className="mx-auto my-10 max-w-2xl rounded-2xl border border-rose-300 bg-rose-50 p-6 text-sm text-rose-900">
        <h1 className="text-base font-bold">页面出错了</h1>
        <p className="mt-2 leading-relaxed">
          界面渲染时抛出了异常，已被拦下（不会再白屏）。可以把下面这段信息发给开发者。
        </p>

        <div className="mt-4 rounded-xl bg-white/70 p-3 font-mono text-xs text-rose-800">
          {error.name}: {error.message}
        </div>

        {info?.componentStack && (
          <pre className="mt-3 max-h-48 overflow-auto whitespace-pre-wrap rounded-xl bg-white/70 p-3 font-mono text-[11px] leading-relaxed text-rose-700">
            {info.componentStack.trim()}
          </pre>
        )}

        <button
          onClick={this.reset}
          className="mt-4 rounded-xl border border-rose-300 bg-white px-4 py-2 text-sm text-rose-700 transition hover:bg-rose-100"
        >
          重试
        </button>
      </div>
    )
  }
}
