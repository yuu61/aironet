package domain

import (
	"path"
	"strings"
)

// Chapter は冊子の 1 章 (= 1 HTML ページ = 1 Markdown ファイル)。
type Chapter struct {
	File  string `json:"file"` // 拡張子を除いた basename。"config_commands_a_to_i"
	Title string `json:"title"`
	Part  string `json:"part"` // 入れ子の目次で章を束ねるパート名。無ければ空
	URL   string `json:"url"`  // 絶対 URL
}

// MarkdownFile は章の Markdown の冊子内相対パス。
func (c Chapter) MarkdownFile() string { return c.File + ".md" }

// ChapterFile は章ページの URL から File を導く。
func ChapterFile(pageURL string) string {
	base := path.Base(pageURL)
	if i := strings.IndexAny(base, "?#"); i >= 0 {
		base = base[:i]
	}
	return strings.TrimSuffix(base, path.Ext(base))
}

// Entry はコマンドリファレンスの 1 コマンド項目。
//
// cisco.com の DITA では reference トピックのうち構文 (section.refsyn) を持つものが
// コマンド。見出しのクラス (CRC_CmdRefCommand-*) は冊子によって付いたり付かなかったり
// するので判定に使わない。
type Entry struct {
	Command string // 空白を畳んだ見出し = 索引のキー
	Title   string // 見出しそのまま
	Anchor  string
	File    string // 冊子内の Markdown 相対パス
	Source  string // 元ページの URL とアンカー
	Line    int    // 見出し行 (1 始まり)
}

// Section は本文の見出し (topictitle)。
type Section struct {
	Anchor string
	Title  string
	File   string // 冊子内の Markdown 相対パス
	Source string // 元ページの URL とアンカー
	Level  int
	Line   int // 見出し行 (1 始まり)
}

// ID は sections.tsv の section 列。Cisco の資料には節番号が無いので
// 「章ファイル#アンカー」で位置を表す。
func (s Section) ID() string { return strings.TrimSuffix(s.File, ".md") + "#" + s.Anchor }
