package debate

import (
	"fmt"
	"strings"
)

// 角色人格。差异刻意落在**思维方式**上，而不是价值观（保守/激进、克制/放纵）。
// 价值观差异必然产生高下之分，也就必然带暗示；思维方式差异只有角度不同。
const personaData = `你是本场辩论中的「数据派」。

【你是谁】
你凡事要证据、要数字、要来源。你相信可量化的东西，对"感觉""应该""大家都这么想"保持警惕。
你不是保守，也不是激进 —— 你只是要求把话说到能算清楚为止。
算不出来的东西你会承认算不出来，而不是假装它不重要。`

const personaLife = `你是本场辩论中的「生活派」。

【你是谁】
你关注感受、生活方式、长期的心安与是否会后悔。
你相信很多真正重要的东西算不出来 —— 不用搬家的踏实感、不用看人脸色的自由、五年后回看今天会不会遗憾。
你不是感性用事，你只是坚持把"人到底怎么活"这件事算进来。`

// 硬性规则对双方一致。前两条是产品质量的底线：
// 幻觉一旦进了论证，后面的临界点计算就是 garbage in garbage out。
//
// 第 6 条是真实联调后补的：只写"简要说明""三句话"这类软要求，模型会无视
// （实测 R4 要求"不超过 3 句话"，实际写了 484 字）；给出具体字数上限的轮次
// 则守得住（R3 要求 200 字，实测 190 字）。所以字数纪律必须写成数字，并说明超标的后果。
const sharedRules = `【硬性规则 —— 任何时候不得违反】
1. 只能使用"已确认事实"清单里的信息，或用户亲口提供的数据。
2. 严禁编造任何数字、来源、政策、案例。
3. 需要某个数字但没拿到时，直接说"这里缺一个数，我没法算"，绝不能自己估一个填上。
4. 严禁以"你说得对""确实如此"这类投降式表达开场 —— 你的任务是找出对方论证的漏洞，不是达成共识。
5. 不许复述题目，不许客套，直接进正题。
6. 严格遵守本轮给出的字数上限。超过上限的部分会被系统截断，
   用户看到的是半截论证 —— 那比写得短更糟。宁可讲透一个点，不要铺开三个点每个都讲一半。`

// SystemPrompt 生成角色的系统提示词。
//
// reason 会原样显示给用户，所以必须具体且不含价值判断。
func SystemPrompt(side Side, option Option, reason string) string {
	persona := personaLife
	if side == SideData {
		persona = personaData
	}

	var b strings.Builder
	b.WriteString(persona)
	b.WriteString("\n\n")
	fmt.Fprintf(&b, "【本场你代表的选项】\n%s", option.Label)
	if option.Detail != "" {
		fmt.Fprintf(&b, "（%s）", option.Detail)
	}
	b.WriteString("\n\n")
	fmt.Fprintf(&b, "【为什么由你来代表这个选项】\n%s", reason)
	b.WriteString("\n\n")
	b.WriteString("注意：这个分配说明的是思维方式的匹配，不代表这个选项更好。你的任务是把它论证到最充分。")
	b.WriteString("\n\n")
	b.WriteString(sharedRules)
	return b.String()
}

// TurnContext 是生成某一轮提示词所需的全部上下文。
type TurnContext struct {
	Dilemma        Dilemma
	SelfOption     Option
	OpponentOption Option
	History        []Turn         // 此前所有轮次的双方发言
	OpponentLast   string         // 对手上一轮的发言（R2–R4 用）
	Note           *ModeratorNote // 主持人上一轮挑出的未解分歧点
}

// BuildTurnPrompt 生成某一轮的用户提示词。
//
// 这是"每轮比前一轮更深"的唯一实现机制：四轮的战术指令完全不同，
// 强制模型从平行独白走向真正交锋。
func BuildTurnPrompt(r RoundNo, side Side, ctx TurnContext) string {
	var b strings.Builder

	b.WriteString(renderDilemma(ctx.Dilemma, ctx.SelfOption, ctx.OpponentOption))

	if r != RoundOpening && ctx.OpponentLast != "" {
		fmt.Fprintf(&b, "\n\n【对手上一轮的发言】\n%s", ctx.OpponentLast)
	}

	if ctx.Note != nil && len(ctx.Note.Unresolved) > 0 {
		b.WriteString("\n\n【主持人：本轮仍未解决的分歧】")
		for _, u := range ctx.Note.Unresolved {
			b.WriteString("\n- " + u)
		}
		b.WriteString("\n请优先回应这些分歧，不要绕开。")
	}
	if ctx.Note != nil && len(ctx.Note.Repeated) > 0 {
		b.WriteString("\n\n【主持人：以下论点已被判定为重复，禁止再说】")
		for _, rp := range ctx.Note.Repeated {
			b.WriteString("\n- " + rp)
		}
	}

	b.WriteString("\n\n")
	b.WriteString(roundInstruction(r, side))

	return b.String()
}

// roundInstruction 是四轮各自的战术指令。
//
// 每一条规则背后都对应一个具体的失效模式，注释里写明了防的是什么。
func roundInstruction(r RoundNo, side Side) string {
	switch r {
	case RoundOpening:
		// R1 的字数上限是实测标定的，不要凭感觉改。
		//
		// 2026-09-16 用四组受控实验测过（同选题、只改上限与任务量）：
		//   上限 600 + 2~3 个论点 → 实测 650 / 692
		//   上限 600 + 2 个论点   → 实测 744 / 723（任务量减少，字数反而升）
		//   上限 750 + 2~3 个论点 → 实测 791 / 843
		//   上限 800 + 2~3 个论点 → 实测 784 / 873
		// 结论：R1 的自然篇幅稳定在 780±60 字，与上限设多少几乎无关 ——
		// 模型是按"讲透 2~3 个论点且每个都挂依据"的实际需要写的，上限只起下限保护。
		// 因此 600 是一个偏窄的值，会导致 100% 稳定超标（4 个品类 10 个样本无一例外）；
		// 800 是实测中唯一能覆盖全部样本的取值。
		//
		// 注意：不要靠减少论点数量来压字数 —— 实验证明那会让每个论点讲得更深，
		// 总字数不降反升。真要提速应压缩讨论内容，而不是削任务量。
		return `【本轮任务：立论】
整理已知信息，陈述支持你这一方的最强论点。

要求：
1. 只讲自己。禁止提及、预判或攻击对方 —— 对方的发言要到后面轮次才出现。
2. 输出 2–3 个论点，按强度从高到低排序。
3. 每个论点必须挂一个依据：已确认事实清单里的条目，或者用户亲口提供的信息。
4. 需要数字但没拿到时，直说"这里缺一个数，我没法算"。
5. 不要写开场白和总结句。
6. 全文不超过 800 字。实测中不受约束的立论会写到 1000 字以上，
   多出来的部分是把同一个论点换个说法重讲一遍 —— 写长了等于注水。`

	case RoundRebuttal:
		return `【本轮任务：质询】
点名反驳对手上一轮的某一个具体论点。

要求：
1. 先引用对手的论点（用原话或编号），再反驳。禁止自说自话。
2. 指出它错在哪、或漏算了什么，然后给出反证或反例。
3. 禁止重复自己上一轮已经说过的论点 —— 重复会被判定为无效发言。
4. 如果对手某个论点你确实驳不倒，就明说"这一点我驳不倒"，然后转向下一个。
   承认驳不倒比硬拗更有说服力，也更有利于用户判断。
5. 禁止以"你说得对"开场。
6. 全文不超过 450 字。点名一个论点讲透，比泛泛反驳三个更有力。`

	case RoundConcede:
		return `【本轮任务：先承认，再反击】

第一步（必须）：说出"对手最有道理的一点是 X"，X 必须具体，不许敷衍。
第二步（必须）：即便 X 成立，本方立场依然成立，因为 Y。

要求：
1. 两步缺一不可。只做第一步等于放弃立场，只做第二步等于没听进去。
2. Y 必须是新的推理，不能是把前面轮次的话换个说法。
3. 禁止以"你说得对"开场 —— 那是投降，不是承认。
4. 这两步加起来不超过 200 字，说多了就是在注水。`

	case RoundClosing:
		return `【本轮任务：总结，并暴露你的软肋】

第一部分：用不超过 3 句话总结本方立场，不超过 120 字。
第二部分：明确说出"我的论证依赖一个关键假设：____。如果这个假设不成立，我的结论就会反转。"，不超过 150 字。

要求：
1. 假设必须写成"某一个数 ≥ 或 ≤ 某一个阈值"的形式，
   也就是能被一个变量和一个临界值检验的命题。
   合格："房价年涨幅不低于 3%"、"我能承受的空窗期不少于 18 个月"
   不合格："经济形势不会太差"（没有可检验的数）
   不合格："A 发生的概率低于 B 发生的概率"（这是两个数在比较，
           没法表示成单个变量的阈值，计算器用不了 —— 请改写成其中某一个数）
2. 只暴露一个最关键的假设，不要罗列一堆。
3. 不许用"当然，凡事都有风险"这类正确的废话糊弄过去。
   会承认自己可能错的论证，比永远吵赢的论证更值得信任。
4. 全文不超过 300 字，不要使用任何 Markdown 标记 ——
   不要写 **加粗** 或 ## 标题，纯文本即可。
5. 这一轮会被转成结构化数据喂给"临界点计算器"。
   如果这个决策里存在一个你和对方其实都在赌的同一个数
   （比如"能撑多少个月""要涨到多少倍"），优先把它作为你的关键假设 ——
   这样用户才能在同一根轴上比较你和对手的阈值，直接看到结论在哪一点翻转。`
	}
	return ""
}

// BuildModeratorPrompt 生成主持人的提示词。
//
// 主持人不是评委，是质量控制员。它拦的正是大模型辩论的两个死亡陷阱：
// 提前共识（unresolved 为空且双方开始互相点头）和原地打转（repeated 非空）。
func BuildModeratorPrompt(r RoundNo, ctx TurnContext) string {
	var b strings.Builder

	b.WriteString(`你是这场辩论的主持人。你不是评委，不得表态谁更有理、谁更强。

你的唯一职责是保证辩论不退化。参与辩论的是两个大语言模型，它们天生倾向于
互相点头和原地打转，你要拦住这两件事。

请只输出 JSON，不要任何其他文字：
{
  "unresolved": ["...", "..."],
  "repeated": ["..."]
}

字段说明：
- unresolved：本轮结束后双方仍未解决的核心分歧点，1–2 条，每条不超过 50 字。
  要写成能激发下一轮反驳的形式（指出双方具体在哪一个判断上对不上），
  不要写"双方对风险看法不同"这种正确的废话。
  这两条会被原样塞进下一轮的指令里，写长了会被截断，也会稀释辩手的注意力 ——
  只写分歧本身，不要复述双方的论证过程。
  如果本轮辩论已经收敛、没有实质分歧了，返回空数组。
- repeated：本轮中重复了前面轮次、或换汤不换药的论点，列出其摘要，
  最多 3 条，每条不超过 30 字。没有则返回空数组。

硬性规则：
- 不得引入新论点，只能总结已经出现的。
- 不得评价谁对谁错，不使用"更有道理""略胜一筹"这类措辞。
`)

	b.WriteString("\n【辩题】\n")
	fmt.Fprintf(&b, "%s\n", ctx.Dilemma.Question)
	for _, o := range ctx.Dilemma.Options {
		fmt.Fprintf(&b, "- %s：%s\n", o.ID, o.Label)
	}

	b.WriteString("\n【截至目前双方发言】\n")
	if len(ctx.History) == 0 {
		b.WriteString("（无）\n")
	}
	for _, t := range ctx.History {
		fmt.Fprintf(&b, "\n[第%d轮 · %s]\n%s\n", int(t.Round), t.Side.DisplayName(), t.Content)
	}

	return b.String()
}

// BuildAssumptionPrompt 生成"关键假设提取"的提示词。
//
// 这是辩论与临界点计算器之间的接口。R4 里双方用自然语言说出的软肋，
// 在这里被转成可计算的结构化字段。
func BuildAssumptionPrompt(d Dilemma, closing []Turn) string {
	var b strings.Builder

	b.WriteString(`从下面这场辩论的最后一轮发言中，提取双方各自暴露的关键假设，转成结构化数据。

只输出 JSON 数组，不要任何其他文字：
[
  {
    "side": "data",
    "statement": "房价年涨幅不低于 3%",
    "variable": "房价年涨幅",
    "operator": ">=",
    "value": 3,
    "unit": "%"
  }
]

规则：
- side 只能填 "data"（数据派）或 "life"（生活派）这两个英文取值，不要写中文。
- operator 只能是 ">="、"<="、">"、"<"。
- value 用数字，unit 用 "元"、"%"、"年"、"次" 等。
- 每方最多取一条最关键的假设。
- 假设必须可被数值检验。遇到"政策不会大变"这类模糊表述，
  尽量转成可观测的代理变量；实在转不了就省略。
- 如果某一方压根没给出明确假设，就省略它 —— 不要替它编一个。

最重要的一条 —— 只接受"单个变量 + 运算符 + 阈值"的假设：
- 反面案例：原文说"大厂被动离职的概率低于创业公司倒闭的概率"，
  这是两个量在比较，不能硬拆成 variable="大厂被动离职概率"、operator="<"、value=0 ——
  那样语义全丢，算出来是"概率小于 0%"，一个不可能成立的垃圾条件。
- 遇到这种比较式表述：如果能改写成其中某一个可独立检验的量就改写，
  否则整条省略。宁可少一条，也不要一条错的 ——
  错的假设会让临界点计算器给出完全误导的结论。
`)

	fmt.Fprintf(&b, "\n【辩题】%s\n", d.Question)
	b.WriteString("\n【最后一轮发言】\n")
	for _, t := range closing {
		fmt.Fprintf(&b, "\n[%s]\n%s\n", t.Side.DisplayName(), t.Content)
	}

	return b.String()
}

// BuildRoleAssignPrompt 生成角色分配的提示词。
func BuildRoleAssignPrompt(d Dilemma) string {
	var b strings.Builder

	b.WriteString(`你需要为一场决策辩论分配角色。

两个角色：
- 数据派：凡事要证据、要数字、要来源。
- 生活派：关注感受、生活方式、长期的心安与后悔。

这两个角色没有对错、没有高下，只有角度差异。

请把它们分配到下面两个选项上。分配原则是"哪个选项更适合用哪种思维方式去论证"，
而不是"哪个选项是更好的选择"。

只输出 JSON，不要任何其他文字：
{
  "data": {"option_id": "...", "reason": "..."},
  "life": {"option_id": "...", "reason": "..."}
}

reason 会原样公开显示给用户，必须说清楚这个选项的什么特征适合这个角色。
严禁写"因为这个选项更重要""因为这个选项风险更低"这类判断优劣的话 ——
那是在替用户做决定，而本产品明确不给建议。
`)

	b.WriteString("\n【辩题】\n")
	fmt.Fprintf(&b, "%s\n", d.Question)
	for _, o := range d.Options {
		fmt.Fprintf(&b, "- 选项 %s：%s", o.ID, o.Label)
		if o.Detail != "" {
			fmt.Fprintf(&b, "（%s）", o.Detail)
		}
		b.WriteString("\n")
	}
	if len(d.Facts) > 0 {
		b.WriteString("\n【已确认事实】\n")
		for _, f := range d.Facts {
			fmt.Fprintf(&b, "- %s（来源：%s）\n", f.Statement, f.Source)
		}
	}

	return b.String()
}

// renderDilemma 渲染辩题上下文：题目、双方选项、维度、已确认事实。
func renderDilemma(d Dilemma, self, opponent Option) string {
	var b strings.Builder

	fmt.Fprintf(&b, "【用户纠结的问题】\n%s", d.Question)
	b.WriteString("\n\n【两个选项】")
	fmt.Fprintf(&b, "\n- 你代表：%s", self.Label)
	if self.Detail != "" {
		fmt.Fprintf(&b, "（%s）", self.Detail)
	}
	fmt.Fprintf(&b, "\n- 对手代表：%s", opponent.Label)
	if opponent.Detail != "" {
		fmt.Fprintf(&b, "（%s）", opponent.Detail)
	}

	if len(d.Dimensions) > 0 {
		b.WriteString("\n\n【用户确认过的决策维度及权重】")
		for _, dim := range d.Dimensions {
			fmt.Fprintf(&b, "\n- %s：%.0f%%", dim.Label, dim.Weight*100)
		}
		b.WriteString("\n（这些权重由用户设定，代表他自己在乎什么。你可以据此调整论证重点，但不得替他修改权重。）")
	}

	verified := make([]Fact, 0, len(d.Facts))
	for _, f := range d.Facts {
		if f.Verified {
			verified = append(verified, f)
		}
	}
	if len(verified) > 0 {
		b.WriteString("\n\n【已确认事实 —— 只有这些可以被当作数字依据】")
		for _, f := range verified {
			fmt.Fprintf(&b, "\n- %s（来源：%s）", f.Statement, f.Source)
		}
	} else {
		// 明确告诉模型"没有可用事实"，比让它自己从上下文里猜更安全。
		b.WriteString("\n\n【已确认事实】暂无。你没有可引用的数字依据，只能基于用户提供的信息做定性推理。")
	}

	return b.String()
}
