package store

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"ding-ssh/internal/i18n"

	"golang.org/x/text/unicode/norm"
)

// 分组路径约定：路径即身份，用 '/' 分隔层级；空字符串表示「未分组」。
//
//   - 新建最多 6 级由前端约束，后端放宽到 8 级以兼容旧数据；
//   - 单段 1-32 个字符只约束新建/重命名，旧数据超长仍可读取；
//   - 写入前统一做 NFC 规范化，避免视觉相同、编码不同的两个分组。
const (
	// MaxGroupDepth 后端接受的最大分组层级（旧数据兼容上限）。
	MaxGroupDepth = 8
	// MaxGroupSegmentRunes 新建分组路径时单段允许的最大字符数。
	MaxGroupSegmentRunes = 32
	// MaxGroupPathRunes 新建分组路径总长度的建议上限。
	MaxGroupPathRunes = 255
)

// NormalizeGroupPath 校验并规范化「新建」分组路径。
// 规则：去首尾空白与 NFC；段不能为空/不能是 . 或 ..；段长 1-32；层级不超过 MaxGroupDepth。
func NormalizeGroupPath(raw string) (string, error) {
	raw = strings.TrimSpace(norm.NFC.String(raw))
	if raw == "" {
		return "", errors.New(i18n.T("分组名称不能为空"))
	}
	parts := strings.Split(raw, "/")
	segments := make([]string, 0, len(parts))
	for _, part := range parts {
		segment := strings.TrimSpace(part)
		if segment == "" {
			return "", errors.New(i18n.T("分组路径不能包含空层级"))
		}
		if err := validateGroupSegment(segment, true); err != nil {
			return "", err
		}
		segments = append(segments, segment)
	}
	if len(segments) > MaxGroupDepth {
		return "", fmt.Errorf(i18n.T("分组层级不能超过 %d 级"), MaxGroupDepth)
	}
	path := strings.Join(segments, "/")
	if utf8.RuneCountInString(path) > MaxGroupPathRunes {
		return "", fmt.Errorf(i18n.T("分组路径过长（最多 %d 个字符）"), MaxGroupPathRunes)
	}
	return path, nil
}

// NormalizeLegacyGroupPath 宽容地规范化「已有」分组路径（旧数据读取 / 操作入口使用）。
// 与 NormalizeGroupPath 的区别：折叠空段（a//b → a/b）、去掉多余的首尾分隔符，
// 但不再限制单段长度，只拒绝无法自动修复的非法内容（控制字符、. / ..、超过 8 级）。
// 返回空字符串表示该路径规范化为「未分组」。
func NormalizeLegacyGroupPath(raw string) (string, error) {
	raw = strings.TrimSpace(norm.NFC.String(raw))
	if raw == "" {
		return "", nil
	}
	parts := strings.Split(raw, "/")
	segments := make([]string, 0, len(parts))
	for _, part := range parts {
		segment := strings.TrimSpace(part)
		if segment == "" {
			continue
		}
		if err := validateGroupSegment(segment, false); err != nil {
			return "", err
		}
		segments = append(segments, segment)
	}
	if len(segments) == 0 {
		return "", nil
	}
	if len(segments) > MaxGroupDepth {
		return "", fmt.Errorf(i18n.T("分组层级不能超过 %d 级"), MaxGroupDepth)
	}
	return strings.Join(segments, "/"), nil
}

// normalizeStoredGroup uses the canonical path form for future server writes.
// Unrepairable legacy values remain readable and are preserved on unrelated edits.
func normalizeStoredGroup(raw string) string {
	if normalized, err := NormalizeLegacyGroupPath(raw); err == nil {
		return normalized
	}
	return raw
}

// NormalizeGroupLeaf 校验并规范化「重命名」时输入的最后一段名称。
// 只允许单段：不得为空、不得包含 '/'、不得是 . 或 ..、不得含控制字符，长度 1-32。
func NormalizeGroupLeaf(raw string) (string, error) {
	leaf := strings.TrimSpace(norm.NFC.String(raw))
	if leaf == "" {
		return "", errors.New(i18n.T("分组名称不能为空"))
	}
	if strings.ContainsRune(leaf, '/') {
		return "", errors.New(i18n.T("分组名称不能包含 /"))
	}
	if err := validateGroupSegment(leaf, true); err != nil {
		return "", err
	}
	return leaf, nil
}

// CanonicalGroupPath 把已有路径规范化为可查询的规范路径；空路径视为非法（未分组不是分组）。
func CanonicalGroupPath(raw string) (string, error) {
	path, err := NormalizeLegacyGroupPath(raw)
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", errors.New(i18n.T("分组名称不能为空"))
	}
	return path, nil
}

// ResolveGroupRename 计算重命名后的规范旧路径与目标路径。
// 旧路径按 CanonicalGroupPath 规范化，newLeaf 只作为最后一段；目标路径 = 父路径 + "/" + newLeaf。
func ResolveGroupRename(path, newLeaf string) (oldPath, targetPath string, err error) {
	oldPath, err = CanonicalGroupPath(path)
	if err != nil {
		return "", "", err
	}
	leaf, err := NormalizeGroupLeaf(newLeaf)
	if err != nil {
		return "", "", err
	}
	targetPath = JoinGroupPath(GroupParent(oldPath), leaf)
	if GroupDepth(targetPath) > MaxGroupDepth {
		return "", "", fmt.Errorf(i18n.T("分组层级不能超过 %d 级"), MaxGroupDepth)
	}
	return oldPath, targetPath, nil
}

func validateGroupSegment(segment string, limitLength bool) error {
	if segment == "." || segment == ".." {
		return errors.New(i18n.T("分组名称不能为 . 或 .."))
	}
	if hasGroupControlRune(segment) {
		return errors.New(i18n.T("分组名称不能包含控制字符"))
	}
	if limitLength && utf8.RuneCountInString(segment) > MaxGroupSegmentRunes {
		return fmt.Errorf(i18n.T("分组名称过长（单段最多 %d 个字符）"), MaxGroupSegmentRunes)
	}
	return nil
}

func hasGroupControlRune(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

// GroupParent 返回父路径；根分组与未分组的父路径都是空字符串。
func GroupParent(path string) string {
	if path == "" {
		return ""
	}
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[:i]
	}
	return ""
}

// GroupBase 返回最后一段名称；未分组返回空字符串。
func GroupBase(path string) string {
	if path == "" {
		return ""
	}
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[i+1:]
	}
	return path
}

// GroupAncestors 返回从根到父级的全部祖先路径（不含自身）；根分组返回空。
// 例如 a/b/c → ["a", "a/b"]。
func GroupAncestors(path string) []string {
	if path == "" {
		return nil
	}
	segments := strings.Split(path, "/")
	if len(segments) < 2 {
		return nil
	}
	ancestors := make([]string, 0, len(segments)-1)
	for i := 1; i < len(segments); i++ {
		ancestors = append(ancestors, strings.Join(segments[:i], "/"))
	}
	return ancestors
}

// GroupDepth 返回层级数：根分组为 1，未分组为 0。
func GroupDepth(path string) int {
	if path == "" {
		return 0
	}
	return strings.Count(path, "/") + 1
}

// IsGroupDescendant 判断 path 是否是 ancestor 的子孙（不含本身）。
// 空 ancestor 代表「未分组」，它不是一个分组，因此始终返回 false。
func IsGroupDescendant(path, ancestor string) bool {
	if path == "" || ancestor == "" {
		return false
	}
	return strings.HasPrefix(path, ancestor+"/")
}

// JoinGroupPath 拼接父路径与最后一段；父路径为空时直接返回最后一段。
func JoinGroupPath(parent, leaf string) string {
	if parent == "" {
		return leaf
	}
	if leaf == "" {
		return parent
	}
	return parent + "/" + leaf
}
