// 品类模板库：高频两难的决策维度预设。
//
// 这是 P6·b 的核心数据。边界刻意划在纯前端：这里只产出"模板"，
// 选中后由 App 预填表单并把 category + dimensions 一起发给后端；
// 后端的 debate.Dimension 与 prompt.go 早已支持消费这些字段，本阶段不碰后端。
//
// 每个维度的 weight 之和应在 1 附近（前端展示成百分比）。key 在同一模板内唯一。

import type { CategoryTemplate } from '../types'

export const CATEGORY_TEMPLATES: CategoryTemplate[] = [
  {
    id: 'phone',
    name: '数码产品购买',
    emoji: '📱',
    sampleQuestion: '买 iPhone 还是安卓旗舰？',
    sampleOptions: { a: '买 iPhone', b: '买安卓旗舰' },
    dimensions: [
      { key: 'budget', label: '预算与残值', weight: 0.3, source: 'template' },
      { key: 'ecosystem', label: '生态绑定', weight: 0.25, source: 'template' },
      { key: 'photo', label: '影像需求', weight: 0.2, source: 'template' },
      { key: 'durability', label: '耐用年限', weight: 0.25, source: 'template' },
    ],
  },
  {
    id: 'housing',
    name: '租房 vs 买房',
    emoji: '🏠',
    sampleQuestion: '这套房该租还是该买？',
    sampleOptions: { a: '租房', b: '买房' },
    dimensions: [
      { key: 'cashflow', label: '月现金流压力', weight: 0.3, source: 'template' },
      { key: 'appreciation', label: '房产增值预期', weight: 0.25, source: 'template' },
      { key: 'flexibility', label: '生活灵活度', weight: 0.25, source: 'template' },
      { key: 'commit', label: '定居意愿', weight: 0.2, source: 'template' },
    ],
  },
  {
    id: 'job',
    name: '求职选择',
    emoji: '💼',
    sampleQuestion: '两个 offer 怎么选？',
    sampleOptions: { a: '大厂稳定岗', b: '创业公司高潜岗' },
    dimensions: [
      { key: 'salary', label: '薪资与期权', weight: 0.3, source: 'template' },
      { key: 'growth', label: '成长空间', weight: 0.3, source: 'template' },
      { key: 'wlb', label: '工作生活平衡', weight: 0.2, source: 'template' },
      { key: 'risk', label: '职业风险', weight: 0.2, source: 'template' },
    ],
  },
  {
    id: 'study',
    name: '考研 vs 工作',
    emoji: '🎓',
    sampleQuestion: '毕业该考研还是直接工作？',
    sampleOptions: { a: '考研深造', b: '直接工作' },
    dimensions: [
      { key: 'career', label: '目标岗位学历门槛', weight: 0.35, source: 'template' },
      { key: 'cost', label: '时间与经济成本', weight: 0.25, source: 'template' },
      { key: 'interest', label: '学术兴趣', weight: 0.2, source: 'template' },
      { key: 'market', label: '就业市场时机', weight: 0.2, source: 'template' },
    ],
  },
  {
    id: 'city',
    name: '城市选择',
    emoji: '🌆',
    sampleQuestion: '留一线城市还是回老家？',
    sampleOptions: { a: '留一线城市', b: '回老家' },
    dimensions: [
      { key: 'income', label: '收入天花板', weight: 0.3, source: 'template' },
      { key: 'support', label: '家庭与熟人支持', weight: 0.25, source: 'template' },
      { key: 'cost', label: '生活成本', weight: 0.25, source: 'template' },
      { key: 'pace', label: '生活节奏偏好', weight: 0.2, source: 'template' },
    ],
  },
  {
    id: 'invest',
    name: '投资理财',
    emoji: '📈',
    sampleQuestion: '这笔钱该稳健理财还是进取投资？',
    sampleOptions: { a: '稳健理财', b: '进取投资' },
    dimensions: [
      { key: 'return', label: '预期收益', weight: 0.3, source: 'template' },
      { key: 'risk', label: '可承受回撤', weight: 0.3, source: 'template' },
      { key: 'liquidity', label: '流动性需求', weight: 0.2, source: 'template' },
      { key: 'horizon', label: '投资期限', weight: 0.2, source: 'template' },
    ],
  },
  {
    id: 'marriage',
    name: '婚恋同居',
    emoji: '💞',
    sampleQuestion: '要不要现在结婚/同居？',
    sampleOptions: { a: '结婚/同居', b: '暂缓' },
    dimensions: [
      { key: 'readiness', label: '双方就绪度', weight: 0.35, source: 'template' },
      { key: 'finance', label: '经济共同基础', weight: 0.25, source: 'template' },
      { key: 'value', label: '价值观契合', weight: 0.25, source: 'template' },
      { key: 'timing', label: '时机成本', weight: 0.15, source: 'template' },
    ],
  },
  {
    id: 'medical',
    name: '医疗方案',
    emoji: '🩺',
    sampleQuestion: '这个病该手术还是保守治疗？',
    sampleOptions: { a: '手术治疗', b: '保守治疗' },
    dimensions: [
      { key: 'efficacy', label: '根治概率', weight: 0.35, source: 'template' },
      { key: 'risk', label: '手术/副作用风险', weight: 0.3, source: 'template' },
      { key: 'recovery', label: '恢复成本', weight: 0.2, source: 'template' },
      { key: 'quality', label: '生活质量影响', weight: 0.15, source: 'template' },
    ],
  },
  {
    id: 'startup',
    name: '创业 vs 上班',
    emoji: '🚀',
    sampleQuestion: '该辞职创业还是继续上班？',
    sampleOptions: { a: '辞职创业', b: '继续上班' },
    dimensions: [
      { key: 'runway', label: '现金流续航', weight: 0.3, source: 'template' },
      { key: 'upside', label: '上行空间', weight: 0.3, source: 'template' },
      { key: 'risk', label: '失败可承受度', weight: 0.25, source: 'template' },
      { key: 'passion', label: '内在驱动力', weight: 0.15, source: 'template' },
    ],
  },
  {
    id: 'car',
    name: '买车',
    emoji: '🚗',
    sampleQuestion: '买电车还是油车？',
    sampleOptions: { a: '买电车', b: '买油车' },
    dimensions: [
      { key: 'cost', label: '用车成本', weight: 0.3, source: 'template' },
      { key: 'range', label: '续航/补能便利', weight: 0.3, source: 'template' },
      { key: 'policy', label: '牌照与政策', weight: 0.2, source: 'template' },
      { key: 'habit', label: '驾驶习惯', weight: 0.2, source: 'template' },
    ],
  },
]

// 按 id 查模板，找不到返回 undefined。App 选中/清空时用到。
export function findTemplate(id: string): CategoryTemplate | undefined {
  return CATEGORY_TEMPLATES.find((t) => t.id === id)
}
