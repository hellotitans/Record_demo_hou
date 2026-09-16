package debate

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDilemmaValidate(t *testing.T) {
	valid := func() Dilemma {
		return Dilemma{
			ID:       "d1",
			Question: "要不要买房",
			Options:  [2]Option{{ID: "a", Label: "买"}, {ID: "b", Label: "租"}},
		}
	}

	tests := []struct {
		name    string
		mutate  func(*Dilemma)
		wantErr bool
	}{
		{name: "合法辩题", mutate: func(*Dilemma) {}},
		{name: "缺 ID", mutate: func(d *Dilemma) { d.ID = "  " }, wantErr: true},
		{name: "缺问题", mutate: func(d *Dilemma) { d.Question = "" }, wantErr: true},
		{name: "选项缺 ID", mutate: func(d *Dilemma) { d.Options[1].ID = "" }, wantErr: true},
		{name: "选项缺标签", mutate: func(d *Dilemma) { d.Options[0].Label = "" }, wantErr: true},
		{
			name:    "两个选项 ID 相同",
			mutate:  func(d *Dilemma) { d.Options[1].ID = d.Options[0].ID },
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := valid()
			tt.mutate(&d)
			err := d.Validate()
			if tt.wantErr && err == nil {
				t.Fatalf("期望报错，实际通过")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("期望通过，实际报错：%v", err)
			}
		})
	}
}

func TestDilemmaOption(t *testing.T) {
	d := Dilemma{Options: [2]Option{{ID: "a", Label: "买"}, {ID: "b", Label: "租"}}}

	if got := d.Option("b"); got.Label != "租" {
		t.Errorf("Option(b) = %q, 期望 %q", got.Label, "租")
	}
	// 找不到的情况返回零值而不是 panic —— 编排层依赖这个行为做降级。
	if got := d.Option("nope"); got != (Option{}) {
		t.Errorf("Option(nope) = %+v, 期望零值", got)
	}
}

func TestSide(t *testing.T) {
	if SideData.DisplayName() != "数据派" {
		t.Errorf("SideData.DisplayName() = %q", SideData.DisplayName())
	}
	if SideLife.DisplayName() != "生活派" {
		t.Errorf("SideLife.DisplayName() = %q", SideLife.DisplayName())
	}
	if SideData.Opponent() != SideLife || SideLife.Opponent() != SideData {
		t.Error("Opponent 应当互为对手")
	}
}

func TestRoundNoValid(t *testing.T) {
	for _, r := range AllRounds {
		if !r.Valid() {
			t.Errorf("轮次 %d 应当合法", int(r))
		}
	}
	if (RoundNo(0)).Valid() || (RoundNo(5)).Valid() {
		t.Error("0 和 5 都不是合法轮次")
	}
}

// TestRoundNoFirstSpeaker 验证先手安排：
// R1 并行所以没有先手；R2–R4 必须交替，避免某一方持续占据叙事主动权。
func TestRoundNoFirstSpeaker(t *testing.T) {
	if s, ok := RoundOpening.FirstSpeaker(); ok {
		t.Errorf("R1 是并行的，不应有先手，实际得到 %q", s)
	}

	want := map[RoundNo]Side{
		RoundRebuttal: SideLife,
		RoundConcede:  SideData,
		RoundClosing:  SideLife,
	}
	for r, w := range want {
		got, ok := r.FirstSpeaker()
		if !ok {
			t.Fatalf("轮次 %d 应当有先手", int(r))
		}
		if got != w {
			t.Errorf("轮次 %d 先手 = %q, 期望 %q", int(r), got, w)
		}
	}
}

func TestAllRoundsOrder(t *testing.T) {
	if len(AllRounds) != 4 {
		t.Fatalf("应当恰好四轮，实际 %d", len(AllRounds))
	}
	for i, r := range AllRounds {
		if int(r) != i+1 {
			t.Errorf("AllRounds[%d] = %d, 期望 %d", i, int(r), i+1)
		}
	}
}

func TestUsageAndResultJSONShape(t *testing.T) {
	// Result 会被直接序列化进 SSE 的 done 帧，字段名是前后端契约，不能随意改。
	r := Result{
		DilemmaID:   "d1",
		Assignments: []Assignment{{Side: SideData, OptionID: "a", Reason: "因为"}},
		Turns:       []Turn{{Round: RoundOpening, Side: SideData, Content: "x"}},
		Assumptions: []Assumption{{ID: "asm-1", Side: SideLife, Variable: "涨幅", Operator: ">=", Value: 3, Unit: "%"}},
	}
	if r.Assumptions[0].Operator != ">=" {
		t.Errorf("Operator = %q", r.Assumptions[0].Operator)
	}
}

// TestModeratorNoteNeverSerializesNull 锁住一个会让前端整页白屏的 bug。
//
// 背景：Go 的 nil slice 序列化成 `null`，而前端把 unresolved/repeated 声明成
// string[]。Mock 模式下主持人输出不是合法 JSON，降级分支把两个字段设为 nil，
// SSE 里就成了 `"unresolved":null`；前端 `note.unresolved.length` 抛 TypeError，
// React 18 卸载整棵组件树 —— 用户看到的是纯白页面，控制台之外毫无线索。
//
// 这个测试保证：无论字段是 nil 还是有值，序列化结果永远是数组。
func TestModeratorNoteNeverSerializesNull(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		note ModeratorNote
	}{
		{name: "两个字段都是 nil（降级分支的真实形态）", note: ModeratorNote{Round: RoundOpening}},
		{name: "只有 unresolved 为 nil", note: ModeratorNote{Round: RoundRebuttal, Repeated: []string{"某论点重复"}}},
		{name: "只有 repeated 为 nil", note: ModeratorNote{Round: RoundConcede, Unresolved: []string{"口径不一致"}}},
		{name: "两个字段都有值", note: ModeratorNote{Round: RoundClosing, Unresolved: []string{"a"}, Repeated: []string{"b"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			raw, err := json.Marshal(tt.note)
			if err != nil {
				t.Fatalf("序列化失败: %v", err)
			}
			s := string(raw)
			if strings.Contains(s, "null") {
				t.Fatalf("序列化结果不该出现 null: %s", s)
			}
			if !strings.Contains(s, `"unresolved":[]`) && !strings.Contains(s, `"unresolved":[`) {
				t.Fatalf("unresolved 应为数组: %s", s)
			}
			if !strings.Contains(s, `"repeated":[]`) && !strings.Contains(s, `"repeated":[`) {
				t.Fatalf("repeated 应为数组: %s", s)
			}
			// 值必须原样保留，不能为了去 null 把内容丢了
			var back ModeratorNote
			if err := json.Unmarshal(raw, &back); err != nil {
				t.Fatalf("反序列化失败: %v", err)
			}
			if len(back.Unresolved) != len(tt.note.Unresolved) {
				t.Fatalf("unresolved 长度变了: got %d, want %d", len(back.Unresolved), len(tt.note.Unresolved))
			}
			if len(back.Repeated) != len(tt.note.Repeated) {
				t.Fatalf("repeated 长度变了: got %d, want %d", len(back.Repeated), len(tt.note.Repeated))
			}
			if back.Round != tt.note.Round {
				t.Fatalf("round 变了: got %d, want %d", back.Round, tt.note.Round)
			}
		})
	}
}

// TestFrameModeratorCarriesNote 验证主持人帧经 Frame 序列化后依然不是 null ——
// 走的是真实链路（Frame.Data 是 interface{}，走的正是 MarshalJSON）。
func TestFrameModeratorCarriesNote(t *testing.T) {
	t.Parallel()

	f := Frame{Kind: FrameModerator, Round: RoundRebuttal, Data: &ModeratorNote{Round: RoundRebuttal}}
	raw, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("序列化 Frame 失败: %v", err)
	}
	if strings.Contains(string(raw), "null") {
		t.Fatalf("主持人帧在真实链路里仍出现 null: %s", raw)
	}
}

func TestParseSide(t *testing.T) {
	cases := []struct {
		in   string
		want Side
		ok   bool
	}{
		{"data", SideData, true},
		{"life", SideLife, true},
		{"DATA", SideData, true},
		{" data ", SideData, true},
		{"数据派", SideData, true},
		{"生活派", SideLife, true},
		{"数据", SideData, true},
		{"生活", SideLife, true},
		{"路人甲", "", false},
		{"", "", false},
		{"a", "", false},
	}
	for _, c := range cases {
		got, ok := ParseSide(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("ParseSide(%q) = (%q,%v), 期望 (%q,%v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestParseSideRoundTrip(t *testing.T) {
	for _, s := range []Side{SideData, SideLife} {
		got, ok := ParseSide(s.DisplayName())
		if !ok || got != s {
			t.Errorf("ParseSide(%q.DisplayName()) = (%q,%v)，应还原为 %q", s, got, ok, s)
		}
	}
}
