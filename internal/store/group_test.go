package store

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func newJSONGroupTestStore(t *testing.T) *JSONGroupStore {
	t.Helper()
	s, err := NewJSONGroupStore(filepath.Join(t.TempDir(), "groups.json"))
	if err != nil {
		t.Fatalf("创建 JSON 分组存储失败: %v", err)
	}
	return s
}

func mustJSONList(t *testing.T, s *JSONGroupStore) []string {
	t.Helper()
	list, err := s.List()
	if err != nil {
		t.Fatalf("读取 JSON 分组列表失败: %v", err)
	}
	return list
}

func TestJSONGroupStoreAddCreatesAncestors(t *testing.T) {
	s := newJSONGroupTestStore(t)
	if err := s.Add("a/b/c"); err != nil {
		t.Fatalf("Add 失败: %v", err)
	}
	if got := mustJSONList(t, s); !slices.Equal(got, []string{"a", "a/b", "a/b/c"}) {
		t.Fatalf("JSON 分组列表 = %v", got)
	}
	if err := s.Add("a/b/c"); err != nil {
		t.Fatalf("重复 Add 失败: %v", err)
	}
	if got := mustJSONList(t, s); len(got) != 3 {
		t.Fatalf("重复 Add 后列表 = %v", got)
	}
	if err := s.Add("a//b"); err == nil {
		t.Fatal("非法路径 Add 应返回错误")
	}
	if err := s.Add(""); err == nil {
		t.Fatal("空路径 Add 应返回错误")
	}
	if s.CascadesServers() {
		t.Fatal("JSON 兜底不应声明已级联服务器")
	}
}

func TestJSONGroupStoreRenamePrefix(t *testing.T) {
	s := newJSONGroupTestStore(t)
	if err := s.Add("a/b/c"); err != nil {
		t.Fatal(err)
	}
	if err := s.Rename("a/b", "x"); err != nil {
		t.Fatalf("Rename 失败: %v", err)
	}
	if got := mustJSONList(t, s); !slices.Equal(got, []string{"a", "a/x", "a/x/c"}) {
		t.Fatalf("重命名后列表 = %v", got)
	}
	// 目标冲突：拒绝且零改动。
	if err := s.Add("a/y"); err != nil {
		t.Fatal(err)
	}
	before := mustJSONList(t, s)
	if err := s.Rename("a/x", "y"); err == nil {
		t.Fatal("目标已存在时应返回错误")
	}
	if got := mustJSONList(t, s); !slices.Equal(got, before) {
		t.Fatalf("冲突重命名后列表变化: %v", got)
	}
	// newLeaf 只能是一段。
	if err := s.Rename("a/x", "q/r"); err == nil {
		t.Fatal("newLeaf 含 / 应返回错误")
	}
}

func TestJSONGroupStoreRemove(t *testing.T) {
	s := newJSONGroupTestStore(t)
	if err := s.Add("a/b/c"); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove("a", false); err != nil {
		t.Fatalf("递归删除失败: %v", err)
	}
	if got := mustJSONList(t, s); len(got) != 0 {
		t.Fatalf("递归删除后列表 = %v", got)
	}
	// 删除不存在的分组幂等返回 nil（只有服务器在用该分组时由 app 层处理）。
	if err := s.Remove("nope", false); err != nil {
		t.Fatalf("删除不存在的分组应幂等: %v", err)
	}
	if err := s.Remove("", false); err == nil {
		t.Fatal("空路径删除应返回错误")
	}
}

func TestJSONGroupStoreRemoveKeepChildren(t *testing.T) {
	s := newJSONGroupTestStore(t)
	if err := s.Add("a/b/c"); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove("a", true); err != nil {
		t.Fatalf("保留子分组删除失败: %v", err)
	}
	if got := mustJSONList(t, s); !slices.Equal(got, []string{"b", "b/c"}) {
		t.Fatalf("上移后列表 = %v", got)
	}

	// 冲突时拒绝且零改动。
	if err := s.Add("x/y"); err != nil {
		t.Fatal(err)
	}
	if err := s.Add("y"); err != nil {
		t.Fatal(err)
	}
	before := mustJSONList(t, s)
	if err := s.Remove("x", true); err == nil {
		t.Fatal("上移与已有分组冲突时应返回错误")
	}
	if got := mustJSONList(t, s); !slices.Equal(got, before) {
		t.Fatalf("冲突删除后列表变化: %v", got)
	}
}

func TestJSONGroupStoreListNormalizes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "groups.json")
	raw := `["a//b", "a/b", "/c", "", " x/y "]`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := NewJSONGroupStore(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{"a/b", "c", "x/y"}) {
		t.Fatalf("规范化后列表 = %v", got)
	}
}
