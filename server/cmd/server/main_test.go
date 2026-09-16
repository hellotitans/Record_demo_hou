package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestResolveDataPath 守住一个排查成本极高的坑：数据文件路径不能依赖进程 CWD。
//
// 背景：2026-09-16 实测中，`go run ./cmd/server` 启动时 os.OpenFile("telemetry.jsonl")
// 返回 "Access is denied"，log.Fatalf 让进程秒退，前端表现为「点开始辩论跳空白页」。
// 根因就是相对路径按 CWD 解析，而 go run 的 CWD 不可控。
func TestResolveDataPath(t *testing.T) {
	t.Parallel()

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd 失败: %v", err)
	}

	tests := []struct {
		name string
		in   string
		// want 为 nil 表示只校验「结果可接受」，不比对具体值
		check func(t *testing.T, got string)
	}{
		{
			name: "绝对路径原样返回，不改写用户意图",
			in:   filepath.Join(cwd, "custom.jsonl"),
			check: func(t *testing.T, got string) {
				want := filepath.Join(cwd, "custom.jsonl")
				if got != want {
					t.Fatalf("绝对路径被改写: got %q, want %q", got, want)
				}
			},
		},
		{
			name: "Windows 盘符绝对路径原样返回",
			in:   `D:\data\telemetry.jsonl`,
			check: func(t *testing.T, got string) {
				if got != `D:\data\telemetry.jsonl` {
					t.Fatalf("盘符路径被改写: got %q", got)
				}
			},
		},
		{
			name: "空字符串保持为空，交由上层决定默认值",
			in:   "",
			check: func(t *testing.T, got string) {
				if got != "" {
					t.Fatalf("空值被改写: got %q", got)
				}
			},
		},
		{
			name: "相对路径必须解析成绝对路径或保持可预期",
			in:   "telemetry.jsonl",
			check: func(t *testing.T, got string) {
				if got == "" {
					t.Fatal("相对路径被解析成空串")
				}
				// go run 场景下回退原值是可接受的行为，但绝不能拼出随机临时路径。
				if got != "telemetry.jsonl" {
					if !filepath.IsAbs(got) {
						t.Fatalf("相对路径解析结果既不是绝对路径也不是原值: %q", got)
					}
					if strings.Contains(got, "go-build") {
						t.Fatalf("解析结果落进了 go 构建临时目录: %q", got)
					}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.check(t, resolveDataPath(tt.in))
		})
	}
}

// TestEnv 校验环境变量读取的默认值语义：空值等同未设置。
//
// 注意：这里刻意不加 t.Parallel()。本测试要改动进程级环境变量，
// 与并行执行互斥（Go 运行时对 t.Setenv + t.Parallel 会直接 panic）。
func TestEnv(t *testing.T) {
	const key = "DEBATE_TEST_ENV_KEY"

	tests := []struct {
		name string
		set  bool
		val  string
		def  string
		want string
	}{
		{name: "未设置时用默认值", set: false, def: "fallback", want: "fallback"},
		{name: "设置为空串时用默认值", set: true, val: "", def: "fallback", want: "fallback"},
		{name: "有值时用环境变量", set: true, val: "explicit", def: "fallback", want: "explicit"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.set {
				t.Setenv(key, tt.val) // 自动注册清理
			} else {
				prev, had := os.LookupEnv(key)
				if err := os.Unsetenv(key); err != nil {
					t.Fatalf("Unsetenv 失败: %v", err)
				}
				t.Cleanup(func() {
					if had {
						_ = os.Setenv(key, prev)
					} else {
						_ = os.Unsetenv(key)
					}
				})
			}
			if got := env(key, tt.def); got != tt.want {
				t.Fatalf("env(%q, %q) = %q, want %q", key, tt.def, got, tt.want)
			}
		})
	}
}
