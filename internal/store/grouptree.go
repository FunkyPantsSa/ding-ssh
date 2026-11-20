package store

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"ding-ssh/internal/i18n"
	"ding-ssh/internal/logx"
)

// SQL 语句使用 substr(...) 做前缀匹配（不用 LIKE，避免 % / _ 转义问题）。
// length() 在 SQLite 中按字符计数，中文路径同样正确。?1 = 旧路径，?2 = 目标路径/父路径。
const (
	sqlGroupSubtreeExists  = `SELECT 1 FROM groups WHERE name = ?1 OR substr(name, 1, length(?1) + 1) = ?1 || '/' LIMIT 1`
	sqlServerSubtreeExists = `SELECT 1 FROM servers WHERE grp = ?1 OR substr(grp, 1, length(?1) + 1) = ?1 || '/' LIMIT 1`

	// 重命名：把整棵子树的前缀从 ?1 换成 ?2。
	sqlRenameGroupPrefix  = `UPDATE groups SET name = ?2 || substr(name, length(?1) + 1) WHERE name = ?1 OR substr(name, 1, length(?1) + 1) = ?1 || '/'`
	sqlRenameServerPrefix = `UPDATE servers SET grp = ?2 || substr(grp, length(?1) + 1) WHERE grp = ?1 OR substr(grp, 1, length(?1) + 1) = ?1 || '/'`

	// 递归删除：删除分组子树，服务器分组置空。
	sqlDeleteGroupSubtree = `DELETE FROM groups WHERE name = ?1 OR substr(name, 1, length(?1) + 1) = ?1 || '/'`
	sqlClearServerSubtree = `UPDATE servers SET grp = '' WHERE grp = ?1 OR substr(grp, 1, length(?1) + 1) = ?1 || '/'`

	// 保留子分组删除：直属服务器移到父级，子孙分组/服务器整体上移一级（ltrim 去掉根组前导 '/'）。
	sqlMoveDirectServersToParent = `UPDATE servers SET grp = ?2 WHERE grp = ?1`
	sqlLiftGroupSubtree          = `UPDATE groups SET name = ltrim(?2 || substr(name, length(?1) + 1), '/') WHERE substr(name, 1, length(?1) + 1) = ?1 || '/'`
	sqlLiftServerSubtree         = `UPDATE servers SET grp = ltrim(?2 || substr(grp, length(?1) + 1), '/') WHERE substr(grp, 1, length(?1) + 1) = ?1 || '/'`
	sqlDeleteGroupRow            = `DELETE FROM groups WHERE name = ?1`
)

// SQLiteGroupTreeStore 基于 SQLite 的路径化分组存储（事务实现）。
//
// 与旧的扁平 SQLiteGroupStore 不同：路径即身份，重命名/删除会在同一事务内
// 级联更新 groups.name 与 servers.grp，不会出现半更新。
type SQLiteGroupTreeStore struct {
	db *sql.DB
}

// NewSQLiteGroupTreeStore 基于已打开的数据库创建路径化分组存储。
func NewSQLiteGroupTreeStore(db *sql.DB) *SQLiteGroupTreeStore {
	logx.Debugf("[store] 创建 SQLite 路径化分组存储（事务实现）")
	return &SQLiteGroupTreeStore{db: db}
}

// CascadesServers 表示本实现已在事务内级联更新 servers.grp，调用方无需再逐条保存服务器。
func (s *SQLiteGroupTreeStore) CascadesServers() bool { return true }

// List 返回全部分组路径（手动创建的空分组；隐式祖先由 GetGroups 在读取时补齐）。
func (s *SQLiteGroupTreeStore) List() ([]string, error) {
	start := time.Now()
	logx.Debugf("[store] 读取分组列表(SQLite tree)")
	rows, err := s.db.Query(`SELECT name FROM groups`)
	if err != nil {
		logx.Errorf("[store] 查询分组列表失败(SQLite tree): err=%v", err)
		return nil, err
	}
	defer rows.Close()
	paths := []string{}
	seen := map[string]struct{}{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			logx.Errorf("[store] 扫描分组行失败(SQLite tree): err=%v", err)
			return nil, err
		}
		path, nerr := NormalizeLegacyGroupPath(raw)
		if nerr != nil {
			// 无法规范化的旧数据也要能读：按原样展示，避免分组凭空消失。
			logx.Warnf("[store] 分组路径非法，按原样返回: raw=%q err=%v", raw, nerr)
			path = strings.TrimSpace(raw)
		}
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	if err := rows.Err(); err != nil {
		logx.Errorf("[store] 遍历分组列表失败(SQLite tree): err=%v", err)
		return nil, err
	}
	sort.Strings(paths)
	logx.Debugf("[store] 分组列表读取完成(SQLite tree): count=%d elapsed=%s", len(paths), time.Since(start))
	return paths, nil
}

// Add 创建分组路径及所有隐式祖先；已存在时幂等。
func (s *SQLiteGroupTreeStore) Add(path string) error {
	start := time.Now()
	normalized, err := NormalizeGroupPath(path)
	if err != nil {
		logx.Warnf("[store] 新增分组被拒（路径非法）: path=%q err=%v", path, err)
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		logx.Errorf("[store] 新增分组失败（开启事务）: path=%q err=%v", normalized, err)
		return fmt.Errorf(i18n.T("新增分组失败: %w"), err)
	}
	defer tx.Rollback()
	created := 0
	for _, p := range append(GroupAncestors(normalized), normalized) {
		res, ierr := tx.Exec(`INSERT INTO groups (name) VALUES (?) ON CONFLICT(name) DO NOTHING`, p)
		if ierr != nil {
			logx.Errorf("[store] 新增分组失败（写入分组行）: path=%q level=%q err=%v", normalized, p, ierr)
			return fmt.Errorf(i18n.T("新增分组失败: %w"), ierr)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			created++
		}
	}
	if err := tx.Commit(); err != nil {
		logx.Errorf("[store] 新增分组失败（提交事务）: path=%q err=%v", normalized, err)
		return fmt.Errorf(i18n.T("新增分组失败: %w"), err)
	}
	logx.Infof("[store] 新增分组成功(SQLite tree): path=%q created=%d depth=%d elapsed=%s",
		normalized, created, GroupDepth(normalized), time.Since(start))
	return nil
}

// Rename 只修改最后一段；在单事务内先检查目标冲突，再级联更新 groups.name 与 servers.grp。
func (s *SQLiteGroupTreeStore) Rename(path, newLeaf string) error {
	start := time.Now()
	oldPath, targetPath, err := ResolveGroupRename(path, newLeaf)
	if err != nil {
		logx.Warnf("[store] 重命名分组被拒（参数非法）: path=%q newLeaf=%q err=%v", path, newLeaf, err)
		return err
	}
	if oldPath == targetPath {
		logx.Debugf("[store] 重命名分组跳过（路径未变）: path=%q", oldPath)
		return nil
	}
	logx.Debugf("[store] 重命名分组(SQLite tree): old=%q new=%q", oldPath, targetPath)
	tx, err := s.db.Begin()
	if err != nil {
		logx.Errorf("[store] 重命名分组失败（开启事务）: old=%q err=%v", oldPath, err)
		return fmt.Errorf(i18n.T("重命名分组失败: %w"), err)
	}
	defer tx.Rollback()

	// 1) 冲突检查：目标路径（含目标子树）在 groups 或 servers 中已存在则拒绝。
	groupConflict, err := groupSubtreeExists(tx, sqlGroupSubtreeExists, targetPath)
	if err != nil {
		logx.Errorf("[store] 重命名分组失败（检查目标分组冲突）: old=%q new=%q err=%v", oldPath, targetPath, err)
		return fmt.Errorf(i18n.T("重命名分组失败: %w"), err)
	}
	serverConflict, err := groupSubtreeExists(tx, sqlServerSubtreeExists, targetPath)
	if err != nil {
		logx.Errorf("[store] 重命名分组失败（检查目标服务器冲突）: old=%q new=%q err=%v", oldPath, targetPath, err)
		return fmt.Errorf(i18n.T("重命名分组失败: %w"), err)
	}
	if groupConflict || serverConflict {
		logx.Warnf("[store] 重命名分组被拒（目标已存在）: old=%q new=%q groupConflict=%v serverConflict=%v",
			oldPath, targetPath, groupConflict, serverConflict)
		return errors.New(i18n.T("目标分组已存在"))
	}

	// 2) 旧路径必须存在（分组行或服务器分组任一即可，隐式分组也允许重命名）。
	sourceInGroups, err := groupSubtreeExists(tx, sqlGroupSubtreeExists, oldPath)
	if err != nil {
		logx.Errorf("[store] 重命名分组失败（检查源分组）: old=%q err=%v", oldPath, err)
		return fmt.Errorf(i18n.T("重命名分组失败: %w"), err)
	}
	sourceInServers, err := groupSubtreeExists(tx, sqlServerSubtreeExists, oldPath)
	if err != nil {
		logx.Errorf("[store] 重命名分组失败（检查源服务器）: old=%q err=%v", oldPath, err)
		return fmt.Errorf(i18n.T("重命名分组失败: %w"), err)
	}
	if !sourceInGroups && !sourceInServers {
		logx.Warnf("[store] 重命名分组被拒（分组不存在）: old=%q", oldPath)
		return errors.New(i18n.T("分组不存在"))
	}

	// 3) 同步前缀：分组行 + 服务器分组。
	groupsRes, err := tx.Exec(sqlRenameGroupPrefix, oldPath, targetPath)
	if err != nil {
		logx.Errorf("[store] 重命名分组失败（更新分组行）: old=%q new=%q err=%v", oldPath, targetPath, err)
		return fmt.Errorf(i18n.T("重命名分组失败: %w"), err)
	}
	serversRes, err := tx.Exec(sqlRenameServerPrefix, oldPath, targetPath)
	if err != nil {
		logx.Errorf("[store] 重命名分组失败（更新服务器分组）: old=%q new=%q err=%v", oldPath, targetPath, err)
		return fmt.Errorf(i18n.T("重命名分组失败: %w"), err)
	}
	groupsAffected, _ := groupsRes.RowsAffected()
	serversAffected, _ := serversRes.RowsAffected()
	if err := tx.Commit(); err != nil {
		logx.Errorf("[store] 重命名分组失败（提交事务）: old=%q new=%q err=%v", oldPath, targetPath, err)
		return fmt.Errorf(i18n.T("重命名分组失败: %w"), err)
	}
	logx.Infof("[store] 重命名分组成功(SQLite tree): old=%q new=%q groups=%d servers=%d elapsed=%s",
		oldPath, targetPath, groupsAffected, serversAffected, time.Since(start))
	return nil
}

// Remove 删除分组。
//   - keepChildren=false：删除该组及所有子孙分组，子树内服务器分组置空；
//   - keepChildren=true：删除该组，直属服务器移到父级（根组置空），子孙分组/服务器整体上移一级；
//     上移后与已有分组冲突时拒绝，保证不出现半更新。
func (s *SQLiteGroupTreeStore) Remove(path string, keepChildren bool) error {
	start := time.Now()
	canonical, err := CanonicalGroupPath(path)
	if err != nil {
		logx.Warnf("[store] 删除分组被拒（路径非法）: path=%q err=%v", path, err)
		return err
	}
	logx.Debugf("[store] 删除分组(SQLite tree): path=%q keepChildren=%v", canonical, keepChildren)
	tx, err := s.db.Begin()
	if err != nil {
		logx.Errorf("[store] 删除分组失败（开启事务）: path=%q err=%v", canonical, err)
		return fmt.Errorf(i18n.T("删除分组失败: %w"), err)
	}
	defer tx.Rollback()

	hasGroups, err := groupSubtreeExists(tx, sqlGroupSubtreeExists, canonical)
	if err != nil {
		logx.Errorf("[store] 删除分组失败（检查分组）: path=%q err=%v", canonical, err)
		return fmt.Errorf(i18n.T("删除分组失败: %w"), err)
	}
	hasServers, err := groupSubtreeExists(tx, sqlServerSubtreeExists, canonical)
	if err != nil {
		logx.Errorf("[store] 删除分组失败（检查服务器）: path=%q err=%v", canonical, err)
		return fmt.Errorf(i18n.T("删除分组失败: %w"), err)
	}
	if !hasGroups && !hasServers {
		logx.Warnf("[store] 删除分组(SQLite tree)未命中: path=%q", canonical)
		return nil
	}

	var groupsAffected, serversAffected int64
	if keepChildren {
		parent := GroupParent(canonical)
		if err := checkLiftConflict(tx, canonical, parent); err != nil {
			logx.Warnf("[store] 删除分组被拒（子分组上移冲突）: path=%q parent=%q err=%v", canonical, parent, err)
			return err
		}
		// 1) 直属服务器移到父级（根组时置为未分组）。
		res, err := tx.Exec(sqlMoveDirectServersToParent, canonical, parent)
		if err != nil {
			logx.Errorf("[store] 删除分组失败（移动直属服务器）: path=%q err=%v", canonical, err)
			return fmt.Errorf(i18n.T("删除分组失败: %w"), err)
		}
		n, _ := res.RowsAffected()
		serversAffected += n
		// 2) 子孙分组整体上移一级。
		res, err = tx.Exec(sqlLiftGroupSubtree, canonical, parent)
		if err != nil {
			logx.Errorf("[store] 删除分组失败（上移子分组）: path=%q err=%v", canonical, err)
			return fmt.Errorf(i18n.T("删除分组失败: %w"), err)
		}
		n, _ = res.RowsAffected()
		groupsAffected += n
		// 3) 子孙分组下的服务器路径同步上移一级。
		res, err = tx.Exec(sqlLiftServerSubtree, canonical, parent)
		if err != nil {
			logx.Errorf("[store] 删除分组失败（上移服务器分组）: path=%q err=%v", canonical, err)
			return fmt.Errorf(i18n.T("删除分组失败: %w"), err)
		}
		n, _ = res.RowsAffected()
		serversAffected += n
		// 4) 删除该分组自身。
		res, err = tx.Exec(sqlDeleteGroupRow, canonical)
		if err != nil {
			logx.Errorf("[store] 删除分组失败（删除分组行）: path=%q err=%v", canonical, err)
			return fmt.Errorf(i18n.T("删除分组失败: %w"), err)
		}
		n, _ = res.RowsAffected()
		groupsAffected += n
	} else {
		// 1) 子树内服务器分组置空。
		res, err := tx.Exec(sqlClearServerSubtree, canonical)
		if err != nil {
			logx.Errorf("[store] 删除分组失败（清空服务器分组）: path=%q err=%v", canonical, err)
			return fmt.Errorf(i18n.T("删除分组失败: %w"), err)
		}
		n, _ := res.RowsAffected()
		serversAffected = n
		// 2) 删除分组及全部子孙分组。
		res, err = tx.Exec(sqlDeleteGroupSubtree, canonical)
		if err != nil {
			logx.Errorf("[store] 删除分组失败（删除分组子树）: path=%q err=%v", canonical, err)
			return fmt.Errorf(i18n.T("删除分组失败: %w"), err)
		}
		n, _ = res.RowsAffected()
		groupsAffected = n
	}
	if err := tx.Commit(); err != nil {
		logx.Errorf("[store] 删除分组失败（提交事务）: path=%q err=%v", canonical, err)
		return fmt.Errorf(i18n.T("删除分组失败: %w"), err)
	}
	logx.Infof("[store] 删除分组成功(SQLite tree): path=%q keepChildren=%v groups=%d servers=%d elapsed=%s",
		canonical, keepChildren, groupsAffected, serversAffected, time.Since(start))
	return nil
}

// checkLiftConflict 预检查「保留子分组删除」上移后是否与已有分组重名。
// 已有分组 = groups 表路径 ∪ servers.grp 去重；子树内（即将上移的）路径不参与比较。
func checkLiftConflict(tx *sql.Tx, path, parent string) error {
	groupNames, err := queryGroupStrings(tx, `SELECT name FROM groups`)
	if err != nil {
		return fmt.Errorf(i18n.T("删除分组失败: %w"), err)
	}
	serverGroups, err := queryGroupStrings(tx, `SELECT DISTINCT grp FROM servers WHERE grp <> ''`)
	if err != nil {
		return fmt.Errorf(i18n.T("删除分组失败: %w"), err)
	}
	return CheckGroupLiftConflict(groupNames, serverGroups, path)
}

// CheckGroupLiftConflict 基于分组路径集合（手动分组 + 服务器分组）判断
// 「保留子分组删除」上移一级后是否与已有分组冲突。path 子树内的路径属于本次上移对象，
// 不参与冲突比较；空字符串代表未分组，忽略。
func CheckGroupLiftConflict(groupPaths, serverGroups []string, path string) error {
	if path == "" {
		return nil
	}
	parent := GroupParent(path)
	existing := map[string]struct{}{}
	collect := func(raw string) {
		name, err := NormalizeLegacyGroupPath(raw)
		if err != nil {
			name = strings.TrimSpace(raw)
		}
		if name == "" || name == path || IsGroupDescendant(name, path) {
			return
		}
		existing[name] = struct{}{}
	}
	for _, name := range groupPaths {
		collect(name)
	}
	for _, name := range serverGroups {
		collect(name)
	}
	for _, raw := range groupPaths {
		name, err := NormalizeLegacyGroupPath(raw)
		if err != nil {
			name = strings.TrimSpace(raw)
		}
		if !IsGroupDescendant(name, path) {
			continue
		}
		relative := strings.TrimPrefix(name, path+"/")
		target := JoinGroupPath(parent, relative)
		if groupPathCollision(existing, target) {
			logx.Warnf("[store] 分组上移目标已存在: path=%q child=%q target=%q", path, name, target)
			return errors.New(i18n.T("目标层级已存在同名分组，请先重命名"))
		}
	}
	return nil
}

// groupPathCollision 判断目标路径本身或其后代是否已存在于集合中。
func groupPathCollision(existing map[string]struct{}, target string) bool {
	if _, ok := existing[target]; ok {
		return true
	}
	prefix := target + "/"
	for name := range existing {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func groupSubtreeExists(tx *sql.Tx, query, path string) (bool, error) {
	var one int
	err := tx.QueryRow(query, path).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func queryGroupStrings(tx *sql.Tx, query string) ([]string, error) {
	rows, err := tx.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []string{}
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

// NormalizeLegacyGroupPaths 对旧数据做一次幂等规范化：折叠空段、去掉首尾分隔符、
// 统一 NFC/空白，并同步更新 groups.name 与 servers.grp。
//
//   - 正常路径零改动；
//   - 规范化后与已有分组重名时合并并 WARN；
//   - 无法自动修复的非法路径（控制字符、. / ..、超过 8 级）保留原值并 WARN，避免丢数据。
func NormalizeLegacyGroupPaths(db *sql.DB) error {
	start := time.Now()
	tx, err := db.Begin()
	if err != nil {
		logx.Errorf("[store] 规范化旧分组路径失败（开启事务）: err=%v", err)
		return fmt.Errorf(i18n.T("规范化旧分组路径失败: %w"), err)
	}
	defer tx.Rollback()

	groupNames, err := queryGroupStrings(tx, `SELECT name FROM groups`)
	if err != nil {
		logx.Errorf("[store] 规范化旧分组路径失败（读取分组）: err=%v", err)
		return fmt.Errorf(i18n.T("规范化旧分组路径失败: %w"), err)
	}
	groupsChanged, groupsMerged, groupsSkipped := 0, 0, 0
	for _, raw := range groupNames {
		if raw == "" {
			if _, err := tx.Exec(`DELETE FROM groups WHERE name = ''`); err != nil {
				logx.Errorf("[store] 规范化旧分组路径失败（删除空名称分组）: err=%v", err)
				return fmt.Errorf(i18n.T("规范化旧分组路径失败: %w"), err)
			}
			logx.Warnf("[store] 旧分组路径为空（等同于未分组），已删除该分组行")
			groupsMerged++
			continue
		}
		normalized, nerr := NormalizeLegacyGroupPath(raw)
		if nerr != nil {
			logx.Warnf("[store] 旧分组路径无法规范化，保留原值: raw=%q err=%v", raw, nerr)
			groupsSkipped++
			continue
		}
		if normalized == raw {
			continue
		}
		if normalized == "" {
			if _, err := tx.Exec(`DELETE FROM groups WHERE name = ?`, raw); err != nil {
				logx.Errorf("[store] 规范化旧分组路径失败（删除未分组行）: raw=%q err=%v", raw, err)
				return fmt.Errorf(i18n.T("规范化旧分组路径失败: %w"), err)
			}
			logx.Warnf("[store] 旧分组路径规范化为未分组，已删除分组行: raw=%q", raw)
			groupsMerged++
			continue
		}
		var existing int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM groups WHERE name = ?`, normalized).Scan(&existing); err != nil {
			logx.Errorf("[store] 规范化旧分组路径失败（检查重名）: raw=%q err=%v", raw, err)
			return fmt.Errorf(i18n.T("规范化旧分组路径失败: %w"), err)
		}
		if _, err := tx.Exec(`INSERT INTO groups (name) VALUES (?) ON CONFLICT(name) DO NOTHING`, normalized); err != nil {
			logx.Errorf("[store] 规范化旧分组路径失败（写入规范路径）: raw=%q normalized=%q err=%v", raw, normalized, err)
			return fmt.Errorf(i18n.T("规范化旧分组路径失败: %w"), err)
		}
		if _, err := tx.Exec(`DELETE FROM groups WHERE name = ?`, raw); err != nil {
			logx.Errorf("[store] 规范化旧分组路径失败（删除旧路径）: raw=%q err=%v", raw, err)
			return fmt.Errorf(i18n.T("规范化旧分组路径失败: %w"), err)
		}
		if existing > 0 {
			logx.Warnf("[store] 旧分组路径规范化后与已有分组重名，已合并: raw=%q normalized=%q", raw, normalized)
			groupsMerged++
		} else {
			logx.Debugf("[store] 旧分组路径已规范化: raw=%q normalized=%q", raw, normalized)
			groupsChanged++
		}
	}

	serverGroups, err := queryGroupStrings(tx, `SELECT DISTINCT grp FROM servers WHERE grp <> ''`)
	if err != nil {
		logx.Errorf("[store] 规范化旧分组路径失败（读取服务器分组）: err=%v", err)
		return fmt.Errorf(i18n.T("规范化旧分组路径失败: %w"), err)
	}
	serversChanged, serversSkipped := 0, 0
	for _, raw := range serverGroups {
		normalized, nerr := NormalizeLegacyGroupPath(raw)
		if nerr != nil {
			logx.Warnf("[store] 旧服务器分组路径无法规范化，保留原值: raw=%q err=%v", raw, nerr)
			serversSkipped++
			continue
		}
		if normalized == raw {
			continue
		}
		if _, err := tx.Exec(`UPDATE servers SET grp = ? WHERE grp = ?`, normalized, raw); err != nil {
			logx.Errorf("[store] 规范化旧分组路径失败（更新服务器分组）: raw=%q normalized=%q err=%v", raw, normalized, err)
			return fmt.Errorf(i18n.T("规范化旧分组路径失败: %w"), err)
		}
		if normalized == "" {
			logx.Warnf("[store] 旧服务器分组路径规范化为未分组: raw=%q", raw)
		} else {
			logx.Debugf("[store] 旧服务器分组路径已规范化: raw=%q normalized=%q", raw, normalized)
		}
		serversChanged++
	}

	if err := tx.Commit(); err != nil {
		logx.Errorf("[store] 规范化旧分组路径失败（提交事务）: err=%v", err)
		return fmt.Errorf(i18n.T("规范化旧分组路径失败: %w"), err)
	}
	if groupsChanged+groupsMerged+serversChanged == 0 {
		logx.Debugf("[store] 旧分组路径检查完成，无需规范化: skipped=%d elapsed=%s",
			groupsSkipped+serversSkipped, time.Since(start))
		return nil
	}
	logx.Infof("[store] 旧分组路径已规范化: groupsChanged=%d groupsMerged=%d serversChanged=%d skipped=%d elapsed=%s",
		groupsChanged, groupsMerged, serversChanged, groupsSkipped+serversSkipped, time.Since(start))
	return nil
}

// 编译期断言。
var (
	_ GroupStore              = (*SQLiteGroupTreeStore)(nil)
	_ GroupServerCascadeStore = (*SQLiteGroupTreeStore)(nil)
)
