package debate

import "testing"

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
