package domain

import (
	"fmt"
	"path"
	"slices"
	"strings"
)

// 章の Markdown をトピックごとのファイルに分ける。
//
// 章 1 本は数百 KB になり (コマンドリファレンスの章は 400 KB 超)、grep や部分読みでは
// 取りこぼす。章をディレクトリにし、章直下のトピック (コマンド 1 つ、設定ガイドの 1 機能) を
// 1 ファイルにする。それでも MaxPartBytes を超えるトピックは、その子トピックをさらに別ファイルに
// 切り出す (再帰)。切り出した元には子への一覧リンクを残す。章の README.md には章タイトル・
// 章直下の本文・トピックの一覧が残る。
//
// 分ける位置は変換器が記録した見出し (Section の Depth と Line) だけで決め、本文の Markdown は
// 解釈しない。ファイルの先頭見出しが # になるようにレベルを引き下げる。

// IndexName は章ディレクトリの索引ファイル名。
const IndexName = "README.md"

// MaxPartBytes は 1 ファイルの目安。これを超えるトピックは子トピックを別ファイルにする。
const MaxPartBytes = 32 * 1024

// Part は分割後の 1 ファイル。
type Part struct {
	Path     string // 冊子内相対パス "<章>/<slug>.md"
	Markdown string
}

// ChapterText は変換器が出した章 1 本の Markdown と、その見出しの記録。
type ChapterText struct {
	Markdown string
	Headings []int // 全見出し行 (1 始まり)。索引に載らない sectiontitle も含む。レベルの付け直しに使う
	Sections []Section
	Entries  []Entry
}

// SplitChapter は分割の結果。Sections / Entries は File と Line を分割後のものに置き換えてある。
type SplitChapter struct {
	Parts    []Part
	Sections []Section
	Entries  []Entry
}

// topic は分割の候補 (章直下以下のトピック)。範囲は 0 始まりの行番号で、end は含まない。
type topic struct {
	path   string
	kids   []int // 別ファイルにした子 (topics の添字)
	sec    int   // Sections の添字
	start  int
	end    int
	size   int // 範囲のバイト数 (子を含む)
	parent int // 親トピック (topics の添字)。章直下は -1
	file   bool
}

// Split は章 1 本を README.md とトピックのファイルに分ける。
func Split(ch Chapter, t ChapterText) (SplitChapter, error) {
	lines := strings.Split(strings.TrimRight(t.Markdown, "\n"), "\n")
	s := &splitter{secs: t.Sections, lines: lines, headings: map[int]bool{}, where: map[int]place{}}
	for _, h := range t.Headings {
		// 記録が本文とずれていると、見出しでない行に # を付けてしまう。変換器の不具合なので止める。
		if h < 1 || h > len(lines) || headingLevel(lines[h-1]) == 0 {
			return SplitChapter{}, fmt.Errorf("%s: 見出しの記録 (%d 行) が本文の見出しを指していない", ch.File, h)
		}
		s.headings[h-1] = true
	}
	s.topics = collectTopics(t.Sections, lines)
	assignFiles(ch, s.topics, t.Sections)
	var rootKids []int
	for i, tp := range s.topics {
		if tp.parent < 0 {
			rootKids = append(rootKids, i)
		}
	}
	s.compose(ch.IndexFile(), 0, len(lines), rootKids, 0, true)
	for _, tp := range s.topics {
		if tp.file {
			s.compose(tp.path, tp.start, tp.end, tp.kids, headingLevel(lines[tp.start])-1, false)
		}
	}
	return s.result(ch, t)
}

// collectTopics は索引の見出しからトピックの範囲を出す。
func collectTopics(secs []Section, lines []string) []topic {
	var topics []topic
	for i, s := range secs {
		if s.Group || s.Depth < 1 || s.Line < 1 || s.Line > len(lines) {
			continue
		}
		tp := topic{sec: i, start: s.Line - 1, end: topicEnd(secs[i+1:], s, len(lines))}
		tp.parent = topicParent(topics, secs, s.Depth, tp.start)
		for _, l := range lines[tp.start:tp.end] {
			tp.size += len(l) + 1
		}
		topics = append(topics, tp)
	}
	return topics
}

// topicEnd はトピックの終わり (含まない行番号)。次の「自分と同じか浅いトピック」か
// 「自分より浅いグループ見出し」の直前まで続く。
func topicEnd(rest []Section, s Section, n int) int {
	for _, u := range rest {
		if u.Line-1 <= s.Line-1 {
			continue
		}
		if (!u.Group && u.Depth <= s.Depth) || (u.Group && u.Depth < s.Depth) {
			return u.Line - 1
		}
	}
	return n
}

// topicParent は自分を含む直近の浅いトピック。無ければ -1 (章直下)。
func topicParent(topics []topic, secs []Section, depth, start int) int {
	for j, tp := range slices.Backward(topics) {
		if secs[tp.sec].Depth < depth && tp.start <= start && start < tp.end {
			return j
		}
	}
	return -1
}

// assignFiles はどのトピックを別ファイルにするか決める。章直下は必ず。それより深いトピックは、
// 親がファイルで MaxPartBytes を超えるときだけ (親の中には子への一覧リンクが残る)。
func assignFiles(ch Chapter, topics []topic, secs []Section) {
	used := map[string]bool{strings.TrimSuffix(strings.ToLower(IndexName), ".md"): true}
	for i := range topics {
		tp := &topics[i]
		tp.file = tp.parent < 0 || (topics[tp.parent].file && topics[tp.parent].size > MaxPartBytes)
		if !tp.file {
			continue
		}
		tp.path = ch.PartFile(uniqueSlug(used, TopicSlug(secs[tp.sec].Title)))
		if tp.parent >= 0 {
			topics[tp.parent].kids = append(topics[tp.parent].kids, i)
		}
	}
}

func uniqueSlug(used map[string]bool, slug string) string {
	name := slug
	for n := 2; used[name]; n++ {
		name = fmt.Sprintf("%s_%d", slug, n)
	}
	used[name] = true
	return name
}

// place は元の見出し行が分割後どこへ行ったか。
type place struct{ part, line, level int }

type splitter struct {
	headings map[int]bool  // 元の見出し行 (0 始まり)
	where    map[int]place // 元の見出し行 → 分割後の位置
	secs     []Section
	lines    []string
	topics   []topic
	parts    []Part
}

// compose は lines[start:end] から kids の範囲を抜いて 1 ファイルにする。抜いた子の位置には
// 一覧リンクを置く。offset は見出しレベルの引き下げ幅。index (章の README) では、章タイトル
// 以外の # を ## にする (1 ページ資料の nested0 トピックが章タイトルと並ぶため)。
func (s *splitter) compose(filePath string, start, end int, kids []int, offset int, index bool) {
	part := len(s.parts)
	var out []string
	lastLink, first := false, true
	k := 0
	for i := start; i < end; i++ {
		if k < len(kids) && i == s.topics[kids[k]].start {
			tp := s.topics[kids[k]]
			out = append(out, "- ["+linkText(s.secs[tp.sec].Title)+"]("+path.Base(tp.path)+")")
			lastLink = true
			i = tp.end - 1
			k++
			continue
		}
		l := s.lines[i]
		if lastLink && l != "" {
			out = append(out, "")
		}
		lastLink = false
		if s.headings[i] {
			var lv int
			l, lv = relevel(l, offset, index && !first)
			s.where[i] = place{part: part, line: len(out) + 1, level: lv}
			first = false
		}
		out = append(out, l)
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	s.parts = append(s.parts, Part{Path: filePath, Markdown: strings.Join(out, "\n") + "\n"})
}

func (s *splitter) result(ch Chapter, t ChapterText) (SplitChapter, error) {
	res := SplitChapter{Parts: s.parts, Sections: slices.Clone(t.Sections), Entries: slices.Clone(t.Entries)}
	for i := range res.Sections {
		sec := &res.Sections[i]
		p, ok := s.where[sec.Line-1]
		if !ok {
			return res, fmt.Errorf("%s: 見出し %q (%d 行) が分割後のどのファイルにも無い", ch.File, sec.Title, sec.Line)
		}
		sec.File, sec.Line, sec.Level = s.parts[p.part].Path, p.line, p.level
	}
	for i := range res.Entries {
		e := &res.Entries[i]
		p, ok := s.where[e.Line-1]
		if !ok {
			return res, fmt.Errorf("%s: コマンド %q (%d 行) が分割後のどのファイルにも無い", ch.File, e.Title, e.Line)
		}
		e.File, e.Line = s.parts[p.part].Path, p.line
	}
	return res, nil
}

// relevel は見出し行のレベルを offset だけ引き下げる。demote は # を ## にする (章の README で
// 章タイトル以外の # を並べないため)。
func relevel(l string, offset int, demote bool) (string, int) {
	lv := headingLevel(l) - offset
	if demote && lv <= 1 {
		lv = 2
	}
	lv = max(1, min(lv, 6))
	return strings.Repeat("#", lv) + l[headingLevel(l):], lv
}

// headingLevel は Markdown の見出し行の # の数。
func headingLevel(l string) int {
	return len(l) - len(strings.TrimLeft(l, "#"))
}

func linkText(title string) string {
	return strings.NewReplacer("[", `\[`, "]", `\]`).Replace(title)
}
