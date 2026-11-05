package controllers

import (
	"reflect"
	"testing"
)

func TestDiffBlackIpList(t *testing.T) {
	cases := []struct {
		name string
		old  []string
		new  []string
		want []string
	}{
		{"全部新增", nil, []string{"1.1.1.1", "2.2.2.2"}, []string{"1.1.1.1", "2.2.2.2"}},
		{"全部已存在", []string{"1.1.1.1"}, []string{"1.1.1.1"}, nil},
		{"部分新增", []string{"1.1.1.1"}, []string{"1.1.1.1", "2.2.2.2"}, []string{"2.2.2.2"}},
		{"有删除也有新增", []string{"1.1.1.1"}, []string{"2.2.2.2"}, []string{"2.2.2.2"}},
		{"忽略空串", nil, []string{"1.1.1.1", "  ", ""}, []string{"1.1.1.1"}},
		{"去重", nil, []string{"1.1.1.1", "1.1.1.1"}, []string{"1.1.1.1"}},
	}

	for _, c := range cases {
		got := diffBlackIpList(c.old, c.new)
		if len(got) == 0 && len(c.want) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: diffBlackIpList(%v, %v) = %v, 期望 %v", c.name, c.old, c.new, got, c.want)
		}
	}
}

func TestFilterHiddenMenus(t *testing.T) {
	got := filterHiddenMenus([]string{"udp", " http ", "", "not-exist", "global"})
	// 非法值与受保护项（global）都应被剔除，只保留 udp 与 http。
	want := []string{"udp", "http"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("filterHiddenMenus = %v, 期望 %v", got, want)
	}
}

func TestMenuOptionsWithState(t *testing.T) {
	options := menuOptionsWithState([]string{"udp", "p2p"})
	if len(options) != len(MenuOptions) {
		t.Fatalf("菜单项数量应保持不变，期望 %d 实际 %d", len(MenuOptions), len(options))
	}
	state := map[string]bool{}
	for _, item := range options {
		state[item.Key] = item.Checked
	}
	if !state["udp"] || !state["p2p"] {
		t.Error("被隐藏的菜单项应标记为已勾选")
	}
	if state["tcp"] || state["client"] {
		t.Error("未隐藏的菜单项不应标记为已勾选")
	}
}
