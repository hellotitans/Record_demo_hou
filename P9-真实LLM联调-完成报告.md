# P9 真实 LLM 联调完成报告

**日期**：2026-09-15
**模型**：DeepSeek（`deepseek-flash` 廉价档 / `deepseek-v4-pro` 强力档）
**结论**：真实联调跑通，**辩论质量达到可用标准**，并暴露出 2 个 Mock 阶段不可能发现的缺陷，均已修复并回归验证。

---

## 一、接入条件实测

| 项目 | 实测结果 |
|---|---|
| 网络可达性 | `api.deepseek.com` 返回 401（非 000），未被代理拦截 |
| 余额 | ¥9.86（联调后 ¥9.80） |
| 可用模型 | **仅 `deepseek-flash` 与 `deepseek-v4-pro`** |
| 协议兼容性 | OpenAI 兼容，SSE / JSON 模式 / 流式 usage 全部正常 |
| 代码改动量 | 极小——`main.go` 早预留了 `OPENAI_BASE_URL` / `MODEL_CHEAP` / `MODEL_STRONG` |

⚠️ **网上的 `deepseek-chat` / `deepseek-reasoner` 已不在模型列表中**，照抄教程会 404。

### 启动配置

```bash
export OPENAI_BASE_URL="https://api.deepseek.com"
export OPENAI_API_KEY="<你的 key>"          # 走环境变量，绝不进代码
export MODEL_CHEAP="deepseek-flash"          # 立论 + 主持人
export MODEL_STRONG="deepseek-v4-pro"        # 质询/承认反击/总结/假设提取/角色分配
export LLM_THINKING="disabled"               # 关键，见下文
```

---

## 二、真实成本（实测，非估算）

| 指标 | 数值 |
|---|---|
| 单场耗时 | **44.8 秒** |
| 单场调用次数 | 13 次（8 发言 + 3 主持人 + 1 假设提取 + 1 角色分配） |
| 单场 token | 约 14.8k prompt + 3.6k completion |
| **实测花费** | **约 ¥0.03 / 场**（余额 9.86 → 9.80，含 2 场辩论 + 十余次探测） |
| 剩余额度可跑 | **300 场以上** |

⚠️ **待办**：代码内 `estimateCost` 估的是 ¥0.09~0.10/场，**比实际高约 3 倍**——该估算按 OpenAI 价格标定，需按 DeepSeek 实际单价重新校准。

---

## 三、两个真实缺陷（Mock 永远测不出来）

### 缺陷 1：思考模式导致整场辩论拿到空响应

**现象**：默认请求下 `deepseek-flash`/`v4-pro` 把正文写进 `delta.reasoning_content`，`delta.content` 全程为 `null`；`readSSE` 只读 `content` → **空响应 → ErrEmptyResponse**。usage 显示 `reasoning_tokens` 占比 100%。

**修复**：`OpenAIClient` 新增客户端级 `Thinking` 字段，非空时请求体带 `thinking:{type:"disabled"}`；`main.go` 新增 `LLM_THINKING` 环境变量（默认 `disabled`，设为客户端级而非请求级，一次配置对全部 13 次调用生效）。

**附带收益**：同一问题 completion_tokens 从 **300 降到 11**（省 96%）。

### 缺陷 2：关键假设因角色名中英文不一致被静默丢弃

**现象**：提示词示例写 `"side": "data"`，但整场发言标签都是中文「数据派 / 生活派」，**模型照抄中文标签**。`orchestrator.go` 直接拿 `a.Side` 与 `SideData/SideLife` 比较，不匹配就丢弃——**不报错、不埋点**，前端只看到空的临界点计算器。

**修复**（三层）：
1. 新增 `debate.ParseSide`：中英文、大小写、空格、`派` 后缀通吃，无法识别才丢弃；
2. 提示词补一条 `side 只能填 "data" 或 "life"`；
3. 编号改为按保留项连续编序，丢弃中间项后不留空洞。

**判断依据**：与其指望模型永远守规矩，不如在入口容错——**约定由代码兜住，提示词只做提示**。这条对任何接 LLM 的结构化输出都适用。

---

## 四、辩论质量评估：达到可用标准

选题「32 岁要不要从大厂跳槽去 A 轮创业公司」，产出 8 段发言、3 段主持人点评、2 条可计算假设。

### 亮点

- **立场鲜明且真的在交锋**：数据派主张「A 轮硬信息足以给出期权价值的期望区间」，生活派反驳「区间越宽，意味着你未来两年的节奏、睡眠、现金流波动也越宽」——不是各说各话。
- **会点名对方论点**：「对手论点二称……这个说法把『能拿到材料』偷换成了『能据此判断风险』」。
- **有真实让步**：「对手最有道理的一点是：A 轮披露的数据是历史快照……但这恰恰说明该用区间定价而非点定价」。
- **主持人有效**：准确抓到「双方在『宽区间是否构成可接受决策依据』上未对齐」，并标记出重复论点（「生活派重复第 2 轮已提出的论点」）。

### 假设提取效果（临界点计算器的输入）

```json
{"side":"life","statement":"用户当前每月可自由支配现金流不足以覆盖至少18个月空窗期",
 "variable":"可支撑空窗期","operator":"<","value":18,"unit":"月"}
{"side":"data","statement":"用户可支撑的无收入或大幅降薪空窗期不少于18个月",
 "variable":"可支撑空窗期","operator":">=","value":18,"unit":"月"}
```

**双方锁定了同一个变量、同一个阈值（18 个月）**，方向上正好相反——这是临界点计算器最理想的输入形态，用户拖动滑块就能看到结论翻转。

---

## 五、门禁结果

后端全部通过：`gofmt` 干净、`vet` 无告警、`build` 正常、测试全绿。

| 包 | 覆盖率 |
|---|---|
| orchestrator | 93.8% |
| store | 92.2% |
| telemetry | 89.9% |
| httpsrv | 86.8% |
| debate | 86.3%（P9 前 85.8%） |
| llm | 85.0% |

新增测试：`TestOpenAIThinkingField`（thinking 字段包含与省略两个方向）、`TestParseSide`（11 个用例）、`TestParseSideRoundTrip`、`TestAssumptionsAcceptChineseSide`（锁定中文角色名缺陷）。

---

## 六、待办

1. **校准 `estimateCost`**：当前高估约 3 倍，需按 DeepSeek 实际单价重算。
2. **密钥清理**：key 仍在 `%TEMP%/ds_key.txt`（仓库外、未入库），确认不再联调后删除；**因密钥已出现在对话中，建议到 DeepSeek 后台轮换**。
3. **提示词可继续打磨**（非阻塞）：单场 45 秒偏长，如需提速可考虑廉价档承担更多环节。
