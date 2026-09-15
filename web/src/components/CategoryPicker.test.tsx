import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { CategoryPicker } from './CategoryPicker'
import { CATEGORY_TEMPLATES } from '../lib/templates'

describe('CategoryPicker', () => {
  it('渲染全部品类 + 自定义', () => {
    render(<CategoryPicker selectedId={null} onSelect={() => {}} />)
    CATEGORY_TEMPLATES.forEach((t) => {
      expect(screen.getByLabelText(`品类 ${t.name}`)).toBeInTheDocument()
    })
    expect(screen.getByLabelText('品类 自定义')).toBeInTheDocument()
  })

  it('点击品类 → onSelect 收到对应模板', () => {
    const onSelect = vi.fn()
    render(<CategoryPicker selectedId={null} onSelect={onSelect} />)
    fireEvent.click(screen.getByLabelText('品类 数码产品购买'))
    expect(onSelect).toHaveBeenCalledWith(
      expect.objectContaining({ id: 'phone', name: '数码产品购买' }),
    )
  })

  it('点击自定义 → onSelect 收到 null', () => {
    const onSelect = vi.fn()
    render(<CategoryPicker selectedId="phone" onSelect={onSelect} />)
    fireEvent.click(screen.getByLabelText('品类 自定义'))
    expect(onSelect).toHaveBeenCalledWith(null)
  })

  it('选中项显示 aria-pressed=true', () => {
    render(<CategoryPicker selectedId="phone" onSelect={() => {}} />)
    expect(screen.getByLabelText('品类 数码产品购买')).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByLabelText('品类 租房 vs 买房')).toHaveAttribute('aria-pressed', 'false')
  })
})
