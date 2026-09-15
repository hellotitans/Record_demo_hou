package debate

import (
	"strings"
	"testing"
)

// TestFourStageInstructionsDistinct 是 P1 最核心的断言：
// 四轮各自的战术指令必须两两不同。这是"每轮比前一轮更深"的唯一实现保证，
// 不能靠一句"请继续反驳"了事——大模型会原地打转。
func TestFourStageInstructionsDistinct(t *testing.T) {
	insts := map[RoundNo]string{}
	for _, r := range AllRounds {
		insts[r] = roundInstruction(r, SideData)
	}

	// 每条指令都非空，且都带"本轮任务"战术标记。
	for r, inst := range insts {
		if strings.TrimSpace(inst) == "" {
			t.Errorf("轮次 %d 的战术指令为空", int(r))
		}
		if !strings.Contains(inst, "【本轮任务") {
			t.Errorf("轮次 %d 的战术指令缺少《本轮任务》标记：%q", int(r), inst)
		}
	}

	// 两两互不相同：R1≠R2≠R3≠R4。
	pairs := [][2]RoundNo{
		{RoundOpening, RoundRebuttal},
		{RoundOpening, RoundConcede},
		{RoundOpening, RoundClosing},
		{RoundRebuttal, RoundConcede},
		{RoundRebuttal, RoundClosing},
		{RoundConcede, RoundClosing},
	}
	for _, p := range pairs {
		if insts[p[0]] == insts[p[1]] {
			t.Errorf("轮次 %d 与轮次 %d 的战术指令相同，深度递增失效", int(p[0]), int(p[1]))
		}
	}
}

// TestBuildTurnPromptPerRoundDistinct 验证端到端的用户提示词也随轮次不同。
func TestBuildTurnPromptPerRoundDistinct(t *testing.T) {
	d := Dilemma{
		ID:       "d1",
		Question: "要不要买房",
		Options:  [2]Option{{ID: "a", Label: "买"}, {ID: "b", Label: "租"}},
	}
	ctx := TurnContext{
		Dilemma:        d,
		SelfOption:     d.Options[0],
		OpponentOption: d.Options[1],
		OpponentLast:   "对手说买比租贵",
	}

	seen := map[string]RoundNo{}
	for _, r := range AllRounds {
		p := BuildTurnPrompt(r, SideData, ctx)
		for other, otherR := range seen {
			if p == other {
				t.Errorf("轮次 %d 与轮次 %d 的 BuildTurnPrompt 输出相同", int(r), int(otherR))
			}
		}
		seen[p] = r
	}
}

// TestNoConvergencePhrase 验证防 LLM 趋同的硬指令确实存在。
//
// 趋同是辩论质量的第一杀手：两轮后双方开始互相点头，辩论提前死亡。
// 系统提示词统一禁止投降式开场；R2（质询）与 R3（承认轮）是趋同压力最大处，
// 必须逐轮再强调一次。
func TestNoConvergencePhrase(t *testing.T) {
	if sp := SystemPrompt(SideData, Option{Label: "买"}, "因为买涉及可量化财务变量"); !strings.Contains(sp, "你说得对") {
		t.Error("SystemPrompt 应包含禁止投降式开场的规则（你说得对）")
	}
	if !strings.Contains(roundInstruction(RoundRebuttal, SideData), "你说得对") {
		t.Error("R2 质询轮应明确禁止以「你说得对」开场")
	}
	if !strings.Contains(roundInstruction(RoundConcede, SideData), "你说得对") {
		t.Error("R3 承认轮应明确禁止以「你说得对」开场（此处趋同压力最大）")
	}
}

// TestOpeningForbidsMentioningOpponent 验证 R1 立论轮确实禁止提对方。
// 这是 R1 能并行的前提：双方互不依赖，必须各自只讲自己。
func TestOpeningForbidsMentioningOpponent(t *testing.T) {
	inst := roundInstruction(RoundOpening, SideData)
	if !strings.Contains(inst, "禁止提及") && !strings.Contains(inst, "禁止预判") {
		t.Error("R1 立论轮应含禁止提及/预判对手的指令，否则无法保证并行独立性")
	}
}

// TestBuildTurnPromptCarriesOpponentAndNote 验证后手的"深度递增传动轴"：
// R2–R4 的提示词必须带上对手上一轮发言（质询对象）和主持人的未解分歧点。
func TestBuildTurnPromptCarriesOpponentAndNote(t *testing.T) {
	d := Dilemma{ID: "d1", Question: "买房还是租房", Options: [2]Option{{ID: "a", Label: "买"}, {ID: "b", Label: "租"}}}
	note := &ModeratorNote{Round: RoundRebuttal, Unresolved: []string{"对首付压力的量化口径不一致"}, Repeated: []string{"某论点已重复"}}

	ctx := TurnContext{
		Dilemma:        d,
		SelfOption:     d.Options[0],
		OpponentOption: d.Options[1],
		OpponentLast:   "对手认为租房更灵活",
		Note:           note,
	}

	p := BuildTurnPrompt(RoundConcede, SideData, ctx)
	if !strings.Contains(p, "对手认为租房更灵活") {
		t.Error("后手提示词应带入对手上一轮发言，否则质询没有对象")
	}
	if !strings.Contains(p, "对首付压力的量化口径不一致") {
		t.Error("提示词应带入主持人的未解分歧点，这是深度递增的传动轴")
	}
	if !strings.Contains(p, "禁止再说") {
		t.Error("提示词应带入主持人判定的重复论点禁令")
	}
}

// TestOpeningIgnoresOpponent 验证 R1 即便给了对手发言也不会被写进提示词
// （否则并行下另一方的立论会"看见"对方，破坏独立性）。
func TestOpeningIgnoresOpponent(t *testing.T) {
	ctx := TurnContext{
		Dilemma:        Dilemma{ID: "d1", Question: "q", Options: [2]Option{{ID: "a", Label: "买"}, {ID: "b", Label: "租"}}},
		SelfOption:     Option{ID: "a", Label: "买"},
		OpponentOption: Option{ID: "b", Label: "租"},
		OpponentLast:   "这串文字绝不该出现在 R1 提示词里",
	}
	p := BuildTurnPrompt(RoundOpening, SideData, ctx)
	if strings.Contains(p, "这串文字绝不该出现在 R1 提示词里") {
		t.Error("R1 立论轮不应包含对手发言，否则破坏并行独立性")
	}
}

// TestSystemPromptNeutral 验证角色分配的去暗示声明存在：
// 分配说明是思维方式匹配，不代表选项更好。
func TestSystemPromptNeutral(t *testing.T) {
	sp := SystemPrompt(SideData, Option{Label: "买"}, "因为买房涉及大量可量化财务变量")
	if !strings.Contains(sp, "不代表这个选项更好") {
		t.Error("SystemPrompt 应声明分配不代表选项更好，这是去暗示的核心")
	}
	if !strings.Contains(sp, "思维方式") {
		t.Error("SystemPrompt 应说明分配基于思维方式匹配")
	}
}

// TestModeratorPromptJSONOnly 验证主持人提示词约束为只输出 JSON，
// 否则编排层无法稳定解析。
func TestModeratorPromptJSONOnly(t *testing.T) {
	p := BuildModeratorPrompt(RoundRebuttal, TurnContext{Dilemma: Dilemma{ID: "d1", Question: "q", Options: [2]Option{{ID: "a", Label: "买"}, {ID: "b", Label: "租"}}}})
	if !strings.Contains(p, "只输出 JSON") {
		t.Error("主持人提示词应要求只输出 JSON")
	}
	if !strings.Contains(p, "unresolved") || !strings.Contains(p, "repeated") {
		t.Error("主持人提示词应明确 unresolved / repeated 两个字段")
	}
}

// TestAssumptionAndRolePrompts 验证两个 JSON 接口的提示词结构完整。
func TestAssumptionAndRolePrompts(t *testing.T) {
	ap := BuildAssumptionPrompt(
		Dilemma{ID: "d1", Question: "q", Options: [2]Option{{ID: "a", Label: "买"}, {ID: "b", Label: "租"}}},
		[]Turn{{Round: RoundClosing, Side: SideData, Content: "c"}},
	)
	if !strings.Contains(ap, "operator") || !strings.Contains(ap, "value") {
		t.Error("假设提取提示词应要求结构化字段")
	}

	rp := BuildRoleAssignPrompt(Dilemma{ID: "d1", Question: "q", Options: [2]Option{{ID: "a", Label: "买"}, {ID: "b", Label: "租"}}})
	if !strings.Contains(rp, "option_id") || !strings.Contains(rp, "reason") {
		t.Error("角色分配提示词应要求 option_id / reason 字段")
	}
	if !strings.Contains(rp, "不给建议") {
		t.Error("角色分配提示词应重申不给建议，防止理由里出现优劣判断")
	}
}
