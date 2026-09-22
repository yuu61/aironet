package domain

import (
	"path"
	"regexp"
)

// 冊子の中へのリンクは、変換の時点ではリンク先がどのファイルに入るか分からない (分割は
// 章ごと、リンク先は別の章のことがある)。変換器は LinkRef の目印を置き、冊子の全章を
// 分けた後に ResolveLinks が相対パスへ置き換える。目印は改行を含まないので行番号は変わらない。

// LinkRef は同じ冊子の中へのリンクの目印。chapter は Chapter.File、anchor は無ければ空、
// url は元の絶対 URL (冊子に無い章だったときに使う)。
func LinkRef(chapter, anchor, url, text string) string {
	return "\x00" + chapter + "\x01" + anchor + "\x01" + url + "\x01" + text + "\x00"
}

var linkRef = regexp.MustCompile("\x00([^\x00\x01]*)\x01([^\x00\x01]*)\x01([^\x00\x01]*)\x01([^\x00]*)\x00")

// ResolveLinks は LinkRef の目印を、リンク先の見出しが入ったファイルへの相対パスにする。
// 同じファイルの中ならテキストだけ、アンカーが見出しに無ければ章の README、
// 章が冊子に無ければ元 URL へのリンクにする。
func ResolveLinks(parts []Part, sections []Section) []Part {
	files := make(map[string]string, len(sections))
	for _, s := range sections {
		files[s.ID()] = s.File
	}
	have := make(map[string]bool, len(parts))
	for _, p := range parts {
		have[p.Path] = true
	}
	out := make([]Part, len(parts))
	for i, p := range parts {
		out[i] = Part{Path: p.Path, Markdown: linkRef.ReplaceAllStringFunc(p.Markdown, func(m string) string {
			g := linkRef.FindStringSubmatch(m)
			chapter, anchor, url, text := g[1], g[2], g[3], g[4]
			dest, ok := files[chapter+"#"+anchor]
			if anchor == "" || !ok {
				dest = chapter + "/" + IndexName
			}
			switch {
			case !have[dest]:
				return "[" + text + "](" + url + ")"
			case dest == p.Path:
				return text
			case path.Dir(dest) == path.Dir(p.Path):
				return "[" + text + "](" + path.Base(dest) + ")"
			default:
				return "[" + text + "](../" + dest + ")"
			}
		})}
	}
	return out
}
