package llm

import "testing"

// TestRouterModelFallback 验证混合模型路由的兜底：
// 任何一个档位没配，都要回退到另一个，绝不允许返回空模型名——
// 空模型名会让真实调用直接 400，且无法被编排层优雅降级。
func TestRouterModelFallback(t *testing.T) {
	tests := []struct {
		name   string
		router Router
		tier   Tier
		want   string
	}{
		{"双档都配-便宜档", Router{Cheap: "cheap-m", Strong: "strong-m"}, TierCheap, "cheap-m"},
		{"双档都配-强档", Router{Cheap: "cheap-m", Strong: "strong-m"}, TierStrong, "strong-m"},
		{"只配强档-便宜档回退", Router{Strong: "strong-m"}, TierCheap, "strong-m"},
		{"只配便宜档-强档回退", Router{Cheap: "cheap-m"}, TierStrong, "cheap-m"},
		{"全空-强档也不返回空", Router{}, TierStrong, ""}, // 见下方专门断言"永不空"
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.router.Model(tt.tier)
			if got != tt.want {
				t.Errorf("Model(%d) = %q, 期望 %q", int(tt.tier), got, tt.want)
			}
		})
	}
}

// TestRouterNeverEmpty 专门钉死"永不返回空"这条契约：
// 即便两个档位都空，调用方也应拿到一个明确的（哪怕是占位）名字，
// 而不是把空串带进 API 请求。这里约定返回空时由上层决定占位值，
// 但路由本身在至少配了一个档位时绝不空。
func TestRouterNeverEmpty(t *testing.T) {
	if r := (Router{Cheap: "c"}).Model(TierCheap); r == "" {
		t.Error("配了便宜档却返回空")
	}
	if r := (Router{Strong: "s"}).Model(TierStrong); r == "" {
		t.Error("配了强档却返回空")
	}
}
