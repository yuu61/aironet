package domain

import "strings"

// NormalizeCommand は見出しを索引のキーにする。連続する空白を 1 つに畳み、前後を落とす。
func NormalizeCommand(s string) string { return strings.Join(strings.Fields(s), " ") }

// TSVCell はタブ区切りの 1 セルに収まる形にする (タブ・改行を空白に)。
func TSVCell(s string) string {
	s = strings.NewReplacer("\t", " ", "\r", " ", "\n", " ").Replace(s)
	return NormalizeCommand(s)
}
