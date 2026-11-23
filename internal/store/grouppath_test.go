package store

import (
	"strings"
	"testing"
)

func TestNormalizeGroupPathValid(t *testing.T) {
	cases := map[string]string{
		"a":               "a",
		"a/b/c":           "a/b/c",
		" a/b/c ":         "a/b/c",
		"a / b":           "a/b", // 段首尾空白被规范化
		"生产环境/华东":         "生产环境/华东",
		"a/b/c/d/e/f/g/h": "a/b/c/d/e/f/g/h", // 后端最多接受 8 级
	}
	for raw, want := range cases {
		got, err := NormalizeGroupPath(raw)
		if err != nil {
			t.Fatalf("NormalizeGroupPath(%q) 返回错误: %v", raw, err)
		}
		if got != want {
			t.Fatalf("NormalizeGroupPath(%q) = %q，期望 %q", raw, got, want)
		}
	}
}

func TestNormalizeGroupPathNFC(t *testing.T) {
	// "e" + 组合重音 与预组合的 "é" 必须合并成同一分组。
	got, err := NormalizeGroupPath("cafe\u0301")
	if err != nil {
		t.Fatal(err)
	}
	if got != "café" {
		t.Fatalf("NFC 规范化失败: got=%q want=%q", got, "café")
	}
	// 两次规范化必须幂等。
	again, err := NormalizeGroupPath(got)
	if err != nil || again != got {
		t.Fatalf("NFC 规范化不幂等: got=%q again=%q err=%v", got, again, err)
	}
}

func TestNormalizeGroupPathInvalid(t *testing.T) {
	cases := []string{
		"",
		"   ",
		"/",
		"/a",
		"a/",
		"a//b",
		"a/./b",
		"a/../b",
		"a/\x01/b",
		"a\tb",
		strings.Repeat("a/", 8) + "b", // 9 级
		strings.Repeat("字", 33),       // 单段 33 个字符
	}
	for _, raw := range cases {
		if got, err := NormalizeGroupPath(raw); err == nil {
			t.Fatalf("NormalizeGroupPath(%q) 期望错误，实际返回 %q", raw, got)
		}
	}
}

func TestNormalizeGroupPathBounds(t *testing.T) {
	if _, err := NormalizeGroupPath(strings.Repeat("a/", 5) + "b"); err != nil {
		t.Fatalf("6 级路径应合法: %v", err)
	}
	if _, err := NormalizeGroupPath(strings.Repeat("a/", 7) + "b"); err != nil {
		t.Fatalf("8 级路径应合法（后端兼容上限）: %v", err)
	}
	if _, err := NormalizeGroupPath(strings.Repeat("字", 32)); err != nil {
		t.Fatalf("单段 32 个字符应合法: %v", err)
	}
}

func TestNormalizeLegacyGroupPath(t *testing.T) {
	cases := map[string]string{
		"":          "",
		"   ":       "",
		" / ":       "",
		"///":       "",
		"a":         "a",
		"/a":        "a",
		"a/":        "a",
		"a//b":      "a/b",
		"//a///b//": "a/b",
		"a / b":     "a/b",
		"a/b/":      "a/b",
	}
	for raw, want := range cases {
		got, err := NormalizeLegacyGroupPath(raw)
		if err != nil {
			t.Fatalf("NormalizeLegacyGroupPath(%q) 返回错误: %v", raw, err)
		}
		if got != want {
			t.Fatalf("NormalizeLegacyGroupPath(%q) = %q，期望 %q", raw, got, want)
		}
		// 幂等：再次规范化不再变化。
		again, err := NormalizeLegacyGroupPath(got)
		if err != nil || again != got {
			t.Fatalf("NormalizeLegacyGroupPath 不幂等: raw=%q got=%q again=%q err=%v", raw, got, again, err)
		}
	}
}

func TestNormalizeLegacyGroupPathInvalid(t *testing.T) {
	cases := []string{
		"a/./b",
		"a/../b",
		"a/\x02/b",
		strings.Repeat("a/", 8) + "b", // 9 级
	}
	for _, raw := range cases {
		if _, err := NormalizeLegacyGroupPath(raw); err == nil {
			t.Fatalf("NormalizeLegacyGroupPath(%q) 期望错误", raw)
		}
	}
	// 旧数据超长单段仍可读。
	long := strings.Repeat("字", 40)
	if got, err := NormalizeLegacyGroupPath(long); err != nil || got != long {
		t.Fatalf("旧数据超长单段应可读: got=%q err=%v", got, err)
	}
}

func TestNormalizeGroupLeaf(t *testing.T) {
	for _, raw := range []string{"a", " a ", "生产环境", strings.Repeat("字", 32)} {
		if _, err := NormalizeGroupLeaf(raw); err != nil {
			t.Fatalf("NormalizeGroupLeaf(%q) 返回错误: %v", raw, err)
		}
	}
	if got, err := NormalizeGroupLeaf(" a "); err != nil || got != "a" {
		t.Fatalf("NormalizeGroupLeaf 去空白失败: got=%q err=%v", got, err)
	}
	for _, raw := range []string{"", "a/b", ".", "..", "a\x03b", strings.Repeat("字", 33)} {
		if _, err := NormalizeGroupLeaf(raw); err == nil {
			t.Fatalf("NormalizeGroupLeaf(%q) 期望错误", raw)
		}
	}
}

func TestGroupPathHelpers(t *testing.T) {
	parentCases := map[string]string{
		"":        "",
		"a":       "",
		"a/b":     "a",
		"a/b/c":   "a/b",
		"a/b/c/d": "a/b/c",
	}
	for path, want := range parentCases {
		if got := GroupParent(path); got != want {
			t.Fatalf("GroupParent(%q) = %q，期望 %q", path, got, want)
		}
	}
	baseCases := map[string]string{
		"":      "",
		"a":     "a",
		"a/b":   "b",
		"a/b/c": "c",
	}
	for path, want := range baseCases {
		if got := GroupBase(path); got != want {
			t.Fatalf("GroupBase(%q) = %q，期望 %q", path, got, want)
		}
	}
	ancestorCases := map[string][]string{
		"":      nil,
		"a":     nil,
		"a/b":   {"a"},
		"a/b/c": {"a", "a/b"},
	}
	for path, want := range ancestorCases {
		got := GroupAncestors(path)
		if len(got) != len(want) {
			t.Fatalf("GroupAncestors(%q) = %v，期望 %v", path, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("GroupAncestors(%q) = %v，期望 %v", path, got, want)
			}
		}
	}
	depthCases := map[string]int{"": 0, "a": 1, "a/b": 2, "a/b/c": 3}
	for path, want := range depthCases {
		if got := GroupDepth(path); got != want {
			t.Fatalf("GroupDepth(%q) = %d，期望 %d", path, got, want)
		}
	}
	descendantCases := []struct {
		path, ancestor string
		want           bool
	}{
		{"a/b", "a", true},
		{"a/b/c", "a", true},
		{"a", "a", false},
		{"ab", "a", false},
		{"a/b", "", false},
		{"", "a", false},
	}
	for _, tc := range descendantCases {
		if got := IsGroupDescendant(tc.path, tc.ancestor); got != tc.want {
			t.Fatalf("IsGroupDescendant(%q, %q) = %v，期望 %v", tc.path, tc.ancestor, got, tc.want)
		}
	}
	joinCases := []struct {
		parent, leaf, want string
	}{
		{"", "a", "a"},
		{"a", "b", "a/b"},
		{"a", "", "a"},
	}
	for _, tc := range joinCases {
		if got := JoinGroupPath(tc.parent, tc.leaf); got != tc.want {
			t.Fatalf("JoinGroupPath(%q, %q) = %q，期望 %q", tc.parent, tc.leaf, got, tc.want)
		}
	}
}

func TestCanonicalGroupPath(t *testing.T) {
	if _, err := CanonicalGroupPath(""); err == nil {
		t.Fatal("空路径应返回错误")
	}
	if got, err := CanonicalGroupPath("a//b/"); err != nil || got != "a/b" {
		t.Fatalf("CanonicalGroupPath = %q, err=%v", got, err)
	}
}

func TestResolveGroupRename(t *testing.T) {
	oldPath, target, err := ResolveGroupRename("a/b", "c")
	if err != nil {
		t.Fatal(err)
	}
	if oldPath != "a/b" || target != "a/c" {
		t.Fatalf("ResolveGroupRename = (%q, %q)", oldPath, target)
	}
	oldPath, target, err = ResolveGroupRename("a", "b")
	if err != nil || oldPath != "a" || target != "b" {
		t.Fatalf("根分组重命名 = (%q, %q, %v)", oldPath, target, err)
	}
	// 旧路径宽容规范化后再算目标。
	oldPath, target, err = ResolveGroupRename("a//b/", "c")
	if err != nil || oldPath != "a/b" || target != "a/c" {
		t.Fatalf("规范化旧路径重命名 = (%q, %q, %v)", oldPath, target, err)
	}
	for _, tc := range []struct{ path, leaf string }{
		{"", "c"},
		{"a/b", ""},
		{"a/b", "c/d"},
		{"a//b", "."},
	} {
		if _, _, err := ResolveGroupRename(tc.path, tc.leaf); err == nil {
			t.Fatalf("ResolveGroupRename(%q, %q) 期望错误", tc.path, tc.leaf)
		}
	}
}
