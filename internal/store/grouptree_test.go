package store

import (
	"database/sql"
	"path/filepath"
	"slices"
	"testing"

	"ding-ssh/internal/models"
)

func newGroupTreeTestStore(t *testing.T) (*SQLiteGroupTreeStore, *sql.DB) {
	t.Helper()
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewSQLiteGroupTreeStore(db), db
}

func saveTestServer(t *testing.T, db *sql.DB, id, group string) {
	t.Helper()
	node := models.ServerNode{ID: id, Name: id, Host: "127.0.0.1", Port: 22, Group: group}
	if err := NewSQLiteStore(db).Save(node); err != nil {
		t.Fatalf("保存测试服务器失败: id=%s err=%v", id, err)
	}
}

func testGroupRows(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query(`SELECT name FROM groups ORDER BY name`)
	if err != nil {
		t.Fatalf("查询 groups 失败: %v", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("扫描 groups 失败: %v", err)
		}
		out = append(out, name)
	}
	return out
}

func testServerGroups(t *testing.T, db *sql.DB) map[string]string {
	t.Helper()
	rows, err := db.Query(`SELECT id, grp FROM servers`)
	if err != nil {
		t.Fatalf("查询 servers 失败: %v", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, grp string
		if err := rows.Scan(&id, &grp); err != nil {
			t.Fatalf("扫描 servers 失败: %v", err)
		}
		out[id] = grp
	}
	return out
}

func assertGroups(t *testing.T, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("分组列表 = %v，期望 %v", got, want)
	}
}

func assertServerGroups(t *testing.T, got, want map[string]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("服务器分组数量 = %d，期望 %d（got=%v）", len(got), len(want), got)
	}
	for id, wantGroup := range want {
		if got[id] != wantGroup {
			t.Fatalf("服务器 %s 分组 = %q，期望 %q", id, got[id], wantGroup)
		}
	}
}

func TestSQLiteGroupTreeStoreListAdd(t *testing.T) {
	tree, db := newGroupTreeTestStore(t)

	if err := tree.Add("a/b/c"); err != nil {
		t.Fatalf("Add 失败: %v", err)
	}
	list, err := tree.List()
	if err != nil {
		t.Fatalf("List 失败: %v", err)
	}
	assertGroups(t, list, []string{"a", "a/b", "a/b/c"})

	// 幂等：重复添加不新增行。
	if err := tree.Add("a/b/c"); err != nil {
		t.Fatalf("重复 Add 失败: %v", err)
	}
	if got := testGroupRows(t, db); !slices.Equal(got, []string{"a", "a/b", "a/b/c"}) {
		t.Fatalf("重复 Add 后分组行 = %v", got)
	}

	// Trim / NFC 规范化后命中已有分组。
	if err := tree.Add(" a / b "); err != nil {
		t.Fatalf("规范化后 Add 失败: %v", err)
	}
	if got := testGroupRows(t, db); len(got) != 3 {
		t.Fatalf("规范化 Add 后分组行 = %v", got)
	}

	for _, bad := range []string{"", "a//b", "/a", "a/", "a/./b"} {
		if err := tree.Add(bad); err == nil {
			t.Fatalf("Add(%q) 期望错误", bad)
		}
	}
}

func TestSQLiteGroupTreeStoreRenameCascade(t *testing.T) {
	tree, db := newGroupTreeTestStore(t)
	if err := tree.Add("a/b"); err != nil {
		t.Fatal(err)
	}
	saveTestServer(t, db, "s1", "a/b")
	saveTestServer(t, db, "s2", "a/b/c")
	saveTestServer(t, db, "s3", "other")

	if err := tree.Rename("a/b", "x"); err != nil {
		t.Fatalf("Rename 失败: %v", err)
	}
	// 分组行：a 保留，a/b → a/x；a/b/c 是服务器隐式分组，无行。
	assertGroups(t, testGroupRows(t, db), []string{"a", "a/x"})
	assertServerGroups(t, testServerGroups(t, db), map[string]string{
		"s1": "a/x",
		"s2": "a/x/c",
		"s3": "other",
	})

	// 根分组重命名：级联子分组与服务器。
	if err := tree.Rename("a", "prod"); err != nil {
		t.Fatalf("根分组 Rename 失败: %v", err)
	}
	assertGroups(t, testGroupRows(t, db), []string{"prod", "prod/x"})
	assertServerGroups(t, testServerGroups(t, db), map[string]string{
		"s1": "prod/x",
		"s2": "prod/x/c",
		"s3": "other",
	})
}

func TestSQLiteGroupTreeStoreRenameConflictKeepsData(t *testing.T) {
	tree, db := newGroupTreeTestStore(t)
	if err := tree.Add("a/b"); err != nil {
		t.Fatal(err)
	}
	if err := tree.Add("a/x"); err != nil {
		t.Fatal(err)
	}
	saveTestServer(t, db, "s1", "a/b")
	saveTestServer(t, db, "s2", "a/b/c")
	beforeGroups := testGroupRows(t, db)
	beforeServers := testServerGroups(t, db)

	// 目标分组行已存在：拒绝且零改动。
	if err := tree.Rename("a/b", "x"); err == nil {
		t.Fatal("目标分组已存在时应返回错误")
	}
	assertGroups(t, testGroupRows(t, db), beforeGroups)
	assertServerGroups(t, testServerGroups(t, db), beforeServers)

	// 目标只有服务器隐式分组：同样拒绝。
	saveTestServer(t, db, "s3", "a/z")
	beforeGroups = testGroupRows(t, db)
	beforeServers = testServerGroups(t, db)
	if err := tree.Rename("a/b", "z"); err == nil {
		t.Fatal("目标服务器分组已存在时应返回错误")
	}
	assertGroups(t, testGroupRows(t, db), beforeGroups)
	assertServerGroups(t, testServerGroups(t, db), beforeServers)

	// 目标子树已存在：拒绝（避免 PK 冲突）。
	if err := tree.Add("a/y/deep"); err != nil {
		t.Fatal(err)
	}
	beforeGroups = testGroupRows(t, db)
	if err := tree.Rename("a/b", "y"); err == nil {
		t.Fatal("目标子树已存在时应返回错误")
	}
	assertGroups(t, testGroupRows(t, db), beforeGroups)
}

func TestSQLiteGroupTreeStoreRenameValidation(t *testing.T) {
	tree, db := newGroupTreeTestStore(t)
	if err := tree.Add("a/b"); err != nil {
		t.Fatal(err)
	}
	if err := tree.Rename("a/nope", "x"); err == nil {
		t.Fatal("重命名不存在的分组应返回错误")
	}
	if err := tree.Rename("a/b", "c/d"); err == nil {
		t.Fatal("newLeaf 含 / 应返回错误")
	}
	if err := tree.Rename("a/b", ""); err == nil {
		t.Fatal("newLeaf 为空应返回错误")
	}
	if err := tree.Rename("a/b", "b"); err != nil {
		t.Fatalf("重命名为原名称应视为幂等成功: %v", err)
	}
	assertGroups(t, testGroupRows(t, db), []string{"a", "a/b"})
}

func TestSQLiteGroupTreeStoreRemoveRecursive(t *testing.T) {
	tree, db := newGroupTreeTestStore(t)
	if err := tree.Add("a/b/c"); err != nil {
		t.Fatal(err)
	}
	saveTestServer(t, db, "s1", "a")
	saveTestServer(t, db, "s2", "a/b")
	saveTestServer(t, db, "s3", "a/b/c")
	saveTestServer(t, db, "s4", "other")

	if err := tree.Remove("a", false); err != nil {
		t.Fatalf("Remove 失败: %v", err)
	}
	assertGroups(t, testGroupRows(t, db), []string{})
	assertServerGroups(t, testServerGroups(t, db), map[string]string{
		"s1": "",
		"s2": "",
		"s3": "",
		"s4": "other",
	})

	// 删除不存在的分组：幂等返回 nil。
	if err := tree.Remove("nope/x", false); err != nil {
		t.Fatalf("删除不存在的分组应幂等: %v", err)
	}
	if err := tree.Remove("", false); err == nil {
		t.Fatal("空路径应返回错误")
	}
}

func TestSQLiteGroupTreeStoreRemoveKeepChildrenRoot(t *testing.T) {
	tree, db := newGroupTreeTestStore(t)
	if err := tree.Add("a/b/c"); err != nil {
		t.Fatal(err)
	}
	saveTestServer(t, db, "direct", "a")
	saveTestServer(t, db, "child", "a/b")
	saveTestServer(t, db, "deep", "a/b/c")
	saveTestServer(t, db, "other", "other")

	if err := tree.Remove("a", true); err != nil {
		t.Fatalf("Remove(keepChildren) 失败: %v", err)
	}
	// 根组删除：子分组上移一级且去掉前导 '/'。
	assertGroups(t, testGroupRows(t, db), []string{"b", "b/c"})
	assertServerGroups(t, testServerGroups(t, db), map[string]string{
		"direct": "",
		"child":  "b",
		"deep":   "b/c",
		"other":  "other",
	})
}

func TestSQLiteGroupTreeStoreRemoveKeepChildrenNested(t *testing.T) {
	tree, db := newGroupTreeTestStore(t)
	if err := tree.Add("x/y/z"); err != nil {
		t.Fatal(err)
	}
	saveTestServer(t, db, "direct", "x/y")
	saveTestServer(t, db, "deep", "x/y/z")
	saveTestServer(t, db, "sibling", "x/other")

	if err := tree.Remove("x/y", true); err != nil {
		t.Fatalf("Remove(keepChildren) 失败: %v", err)
	}
	assertGroups(t, testGroupRows(t, db), []string{"x", "x/z"})
	assertServerGroups(t, testServerGroups(t, db), map[string]string{
		"direct":  "x",
		"deep":    "x/z",
		"sibling": "x/other",
	})
}

func TestSQLiteGroupTreeStoreRemoveKeepChildrenConflict(t *testing.T) {
	tree, db := newGroupTreeTestStore(t)
	if err := tree.Add("a/b"); err != nil {
		t.Fatal(err)
	}
	if err := tree.Add("b"); err != nil {
		t.Fatal(err)
	}
	saveTestServer(t, db, "s1", "a/b")
	beforeGroups := testGroupRows(t, db)
	beforeServers := testServerGroups(t, db)

	if err := tree.Remove("a", true); err == nil {
		t.Fatal("子分组上移与已有分组冲突时应返回错误")
	}
	assertGroups(t, testGroupRows(t, db), beforeGroups)
	assertServerGroups(t, testServerGroups(t, db), beforeServers)

	// 服务器隐式分组造成的冲突同样拒绝。
	tree2, db2 := newGroupTreeTestStore(t)
	if err := tree2.Add("a/b"); err != nil {
		t.Fatal(err)
	}
	saveTestServer(t, db2, "s1", "a/b")
	saveTestServer(t, db2, "s2", "b")
	beforeGroups = testGroupRows(t, db2)
	beforeServers = testServerGroups(t, db2)
	if err := tree2.Remove("a", true); err == nil {
		t.Fatal("服务器隐式分组冲突时应返回错误")
	}
	assertGroups(t, testGroupRows(t, db2), beforeGroups)
	assertServerGroups(t, testServerGroups(t, db2), beforeServers)

	// 递归删除不受冲突影响。
	if err := tree.Remove("a", false); err != nil {
		t.Fatalf("递归删除应成功: %v", err)
	}
	assertGroups(t, testGroupRows(t, db), []string{"b"})
	assertServerGroups(t, testServerGroups(t, db), map[string]string{"s1": ""})
}

func TestNormalizeLegacyGroupPaths(t *testing.T) {
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	defer db.Close()

	// 直接写入旧版非法路径（空段、首尾斜杠、首尾空白、空名称、重名冲突）。
	for _, name := range []string{"a//b", "a/b", "/c", " x / y ", "", "/d"} {
		if _, err := db.Exec(`INSERT INTO groups (name) VALUES (?)`, name); err != nil {
			t.Fatalf("写入旧分组失败: name=%q err=%v", name, err)
		}
	}
	saveTestServer(t, db, "s1", "a//b/c")
	saveTestServer(t, db, "s2", "  /d  ")
	saveTestServer(t, db, "s3", "prod/us")
	saveTestServer(t, db, "s4", "")

	if err := NormalizeLegacyGroupPaths(db); err != nil {
		t.Fatalf("NormalizeLegacyGroupPaths 失败: %v", err)
	}
	assertGroups(t, testGroupRows(t, db), []string{"a/b", "c", "d", "x/y"})
	assertServerGroups(t, testServerGroups(t, db), map[string]string{
		"s1": "a/b/c",
		"s2": "d",
		"s3": "prod/us", // 合法旧路径零改动
		"s4": "",
	})

	// 幂等：再次运行不改变任何数据。
	beforeGroups := testGroupRows(t, db)
	beforeServers := testServerGroups(t, db)
	if err := NormalizeLegacyGroupPaths(db); err != nil {
		t.Fatalf("第二次 NormalizeLegacyGroupPaths 失败: %v", err)
	}
	assertGroups(t, testGroupRows(t, db), beforeGroups)
	assertServerGroups(t, testServerGroups(t, db), beforeServers)
}

func TestNormalizeLegacyGroupPathsNoChange(t *testing.T) {
	tree, db := newGroupTreeTestStore(t)
	if err := tree.Add("a/b/c"); err != nil {
		t.Fatal(err)
	}
	if err := tree.Add("x"); err != nil {
		t.Fatal(err)
	}
	saveTestServer(t, db, "s1", "a/b/c")
	beforeGroups := testGroupRows(t, db)
	beforeServers := testServerGroups(t, db)
	if err := NormalizeLegacyGroupPaths(db); err != nil {
		t.Fatal(err)
	}
	assertGroups(t, testGroupRows(t, db), beforeGroups)
	assertServerGroups(t, testServerGroups(t, db), beforeServers)
}

func TestCheckGroupLiftConflict(t *testing.T) {
	if err := CheckGroupLiftConflict([]string{"a", "a/b"}, nil, "a"); err != nil {
		t.Fatalf("无冲突时应通过: %v", err)
	}
	if err := CheckGroupLiftConflict([]string{"a", "a/b", "b"}, nil, "a"); err == nil {
		t.Fatal("目标层级已存在同名分组时应冲突")
	}
	if err := CheckGroupLiftConflict([]string{"a", "a/b"}, []string{"b"}, "a"); err == nil {
		t.Fatal("服务器隐式分组冲突时应报错")
	}
	if err := CheckGroupLiftConflict([]string{"x", "x/y"}, []string{"x/z"}, "x/y"); err != nil {
		t.Fatalf("兄弟分组不应冲突: %v", err)
	}
}
