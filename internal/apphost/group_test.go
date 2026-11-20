package apphost

import (
	"database/sql"
	"path/filepath"
	"slices"
	"testing"

	"ding-ssh/internal/models"
	"ding-ssh/internal/store"
)

func newGroupTestApp(t *testing.T) (*App, *sql.DB) {
	t.Helper()
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	app := &App{
		db:     db,
		store:  store.NewSQLiteStore(db),
		groups: store.NewSQLiteGroupTreeStore(db),
	}
	return app, db
}

func saveAppTestServer(t *testing.T, app *App, id, group string) {
	t.Helper()
	node := models.ServerNode{ID: id, Name: id, Host: "127.0.0.1", Port: 22, Group: group}
	if err := app.store.Save(node); err != nil {
		t.Fatalf("保存测试服务器失败: id=%s err=%v", id, err)
	}
}

func assertAppGroups(t *testing.T, app *App, want []string) {
	t.Helper()
	got := app.GetGroups()
	if !slices.Equal(got, want) {
		t.Fatalf("GetGroups = %v，期望 %v", got, want)
	}
}

func TestAppGetGroupsImplicitAncestors(t *testing.T) {
	app, _ := newGroupTestApp(t)
	saveAppTestServer(t, app, "s1", "a/b/c")
	assertAppGroups(t, app, []string{"a", "a/b", "a/b/c"})

	// 手动创建的空分组及其祖先同样出现在列表里。
	if err := app.AddGroup("x/y"); err != nil {
		t.Fatalf("AddGroup 失败: %v", err)
	}
	assertAppGroups(t, app, []string{"a", "a/b", "a/b/c", "x", "x/y"})

	// 旧数据里的非法路径按规范化后的层级展开；合法旧数据零改动。
	saveAppTestServer(t, app, "s2", " p//q ")
	assertAppGroups(t, app, []string{"a", "a/b", "a/b/c", "p", "p/q", "x", "x/y"})
}

func TestAppGroupLifecycleSQLite(t *testing.T) {
	app, _ := newGroupTestApp(t)
	if err := app.AddGroup("proj/web/1"); err != nil {
		t.Fatalf("AddGroup 失败: %v", err)
	}
	if err := app.AddGroup("proj/web/1"); err != nil {
		t.Fatalf("重复 AddGroup 应幂等: %v", err)
	}
	if err := app.AddGroup(""); err == nil {
		t.Fatal("空路径 AddGroup 应返回错误")
	}
	saveAppTestServer(t, app, "s1", "proj/web")

	// 重命名中间层：分组与服务器前缀同步。
	if err := app.RenameGroup("proj/web", "app"); err != nil {
		t.Fatalf("RenameGroup 失败: %v", err)
	}
	assertAppGroups(t, app, []string{"proj", "proj/app", "proj/app/1"})
	nodes, err := app.store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0].Group != "proj/app" {
		t.Fatalf("重命名后服务器分组 = %+v", nodes)
	}

	// 重命名根层。
	if err := app.RenameGroup("proj", "p"); err != nil {
		t.Fatalf("RenameGroup 根分组失败: %v", err)
	}
	assertAppGroups(t, app, []string{"p", "p/app", "p/app/1"})
	nodes, _ = app.store.List()
	if len(nodes) != 1 || nodes[0].Group != "p/app" {
		t.Fatalf("根分组重命名后服务器分组 = %+v", nodes)
	}

	// newLeaf 含 '/' 必须拒绝。
	if err := app.RenameGroup("p/app", "x/y"); err == nil {
		t.Fatal("newLeaf 含 / 应返回错误")
	}

	// 保留子分组删除：子树整体上移一级，直属服务器移到父级。
	if err := app.RemoveGroup("p/app", true); err != nil {
		t.Fatalf("RemoveGroup(keepChildren) 失败: %v", err)
	}
	assertAppGroups(t, app, []string{"p", "p/1"})
	nodes, _ = app.store.List()
	if len(nodes) != 1 || nodes[0].Group != "p" {
		t.Fatalf("保留子分组删除后服务器分组 = %+v", nodes)
	}

	// 递归删除：服务器变未分组。
	if err := app.RemoveGroup("p", false); err != nil {
		t.Fatalf("RemoveGroup 失败: %v", err)
	}
	assertAppGroups(t, app, []string{})
	nodes, _ = app.store.List()
	if len(nodes) != 1 || nodes[0].Group != "" {
		t.Fatalf("递归删除后服务器分组 = %+v", nodes)
	}
}

func TestAppGroupJSONFallback(t *testing.T) {
	dir := t.TempDir()
	serverStore, err := store.NewJSONStore(filepath.Join(dir, "servers.json"))
	if err != nil {
		t.Fatal(err)
	}
	groupStore, err := store.NewJSONGroupStore(filepath.Join(dir, "groups.json"))
	if err != nil {
		t.Fatal(err)
	}
	app := &App{store: serverStore, groups: groupStore}

	saveAppTestServer(t, app, "s1", "a/b/c")
	if err := app.AddGroup("a/b"); err != nil {
		t.Fatalf("AddGroup 失败: %v", err)
	}

	// JSON 兜底：分组文件与服务器文件由 app 层分别更新（非事务）。
	if err := app.RenameGroup("a", "x"); err != nil {
		t.Fatalf("JSON 兜底 RenameGroup 失败: %v", err)
	}
	assertAppGroups(t, app, []string{"x", "x/b", "x/b/c"})
	nodes, _ := app.store.List()
	if len(nodes) != 1 || nodes[0].Group != "x/b/c" {
		t.Fatalf("JSON 兜底重命名后服务器分组 = %+v", nodes)
	}

	// 冲突：目标已有服务器隐式分组时拒绝。
	saveAppTestServer(t, app, "s2", "y")
	before, _ := app.store.List()
	if err := app.RenameGroup("x", "y"); err == nil {
		t.Fatal("JSON 兜底重命名目标冲突应返回错误")
	}
	after, _ := app.store.List()
	if len(after) != len(before) {
		t.Fatalf("冲突重命名后服务器数量变化: before=%d after=%d", len(before), len(after))
	}

	// 保留子分组删除：直属服务器到父级、子分组上移。
	if err := app.RemoveGroup("x", true); err != nil {
		t.Fatalf("JSON 兜底 RemoveGroup(keepChildren) 失败: %v", err)
	}
	assertAppGroups(t, app, []string{"b", "b/c", "y"})
	nodes, _ = app.store.List()
	groupsByID := map[string]string{}
	for _, n := range nodes {
		groupsByID[n.ID] = n.Group
	}
	if groupsByID["s1"] != "b/c" || groupsByID["s2"] != "y" {
		t.Fatalf("JSON 兜底上移后服务器分组 = %+v", groupsByID)
	}

	// 递归删除：子树服务器全部变未分组。
	if err := app.RemoveGroup("b", false); err != nil {
		t.Fatalf("JSON 兜底 RemoveGroup 失败: %v", err)
	}
	assertAppGroups(t, app, []string{"y"})
	nodes, _ = app.store.List()
	for _, n := range nodes {
		if n.ID == "s1" && n.Group != "" {
			t.Fatalf("递归删除后服务器分组 = %q", n.Group)
		}
	}
}
