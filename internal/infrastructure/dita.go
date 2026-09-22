package infrastructure

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/net/html"

	"github.com/yuu61/aironet/internal/domain"
)

// cisco.com の章ページ (DITA から生成された HTML) を Markdown にする。
//
// 本文は <div id="chapterContent"> (冊子の章) か <div id="eot-doc-wrapper"> (1 ページ資料)
// の下にあり、<article class="topic …"> がトピック、その最初の <h*> が見出しになる。
// トピックの種類 (reference / concept / task) と、その中の section のクラス
// (refsyn = 構文、command_default、usage_guidelines …) が本文の構造をそのまま表すので、
// 汎用の HTML→Markdown 変換ではなく、この構造に沿って Markdown を組む。
//
// 見出しの行番号を記録しながら書き、索引 (commands.tsv / sections.tsv) はその行を指す。
// 表・注記・手順は dita_table.go、インラインは dita_inline.go。

// Converted は章 1 本の変換結果。
type Converted struct {
	Title    string
	Markdown string
	Entries  []domain.Entry
	Sections []domain.Section
	Anchors  []string // 章内目次に載る全アンカー。取りこぼしの検証に使う
	Warnings []string
}

// ConvertChapter は章ページを Markdown にする。images は本文の画像 URL → images/ 内のファイル名。
func ConvertChapter(page []byte, ch domain.Chapter, images map[string]string) (Converted, error) {
	doc, err := html.Parse(bytes.NewReader(page))
	if err != nil {
		return Converted{}, err
	}
	base, err := url.Parse(ch.URL)
	if err != nil {
		return Converted{}, err
	}
	root, eot := contentRoot(doc)
	if root == nil {
		return Converted{}, errors.New("本文 (div#chapterContent / div#eot-doc-wrapper) が無い")
	}
	res := &Converted{Anchors: collectAnchors(doc, root, eot)}
	c := &converter{ch: ch, base: base, images: images, res: res, line: 1}
	// 章のタイトル (h1) は article の外にあり、本文側に id が無い。章内目次の先頭が
	// その見出しを指すので、それを章タイトルのアンカーにする。
	if len(res.Anchors) > 0 {
		c.anchor = res.Anchors[0]
	}
	c.children(root)

	res.Markdown = strings.TrimRight(c.out.String(), "\n") + "\n"
	res.Title = ch.Title
	for _, s := range res.Sections {
		if s.Level == 1 {
			res.Title = s.Title
			break
		}
	}
	return *res, nil
}

// CollectImages は本文の画像 URL (絶対) を文書順に集める。テンプレート画像 (note のアイコン等) は除く。
func CollectImages(page []byte, pageURL string) ([]string, error) {
	doc, err := html.Parse(bytes.NewReader(page))
	if err != nil {
		return nil, err
	}
	base, err := url.Parse(pageURL)
	if err != nil {
		return nil, err
	}
	root, _ := contentRoot(doc)
	if root == nil {
		return nil, errors.New("本文 (div#chapterContent / div#eot-doc-wrapper) が無い")
	}
	seen := map[string]bool{}
	var out []string
	for _, img := range findAll(root, func(n *html.Node) bool { return isElem(n, "img") }, nil) {
		u := resolveImage(base, attr(img, "src"))
		if u == "" || seen[u] {
			continue
		}
		seen[u] = true
		out = append(out, u)
	}
	return out, nil
}

func contentRoot(doc *html.Node) (root *html.Node, eot bool) {
	if r := findByID(doc, "chapterContent"); r != nil {
		return r, false
	}
	if r := findByID(doc, "eot-doc-wrapper"); r != nil {
		return r, true
	}
	return nil, false
}

// collectAnchors は章内目次のアンカーを集める。冊子の章は <div id="chapterToc">、
// 1 ページ資料は本文冒頭の <nav> の Contents。
func collectAnchors(doc, root *html.Node, eot bool) []string {
	scope := findByID(doc, "chapterToc")
	if eot || scope == nil {
		scope = find(root, func(n *html.Node) bool { return isElem(n, "nav") })
	}
	if scope == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, a := range findAll(scope, func(n *html.Node) bool { return isElem(n, "a") }, nil) {
		href := attr(a, "href")
		if !strings.HasPrefix(href, "#") || len(href) < 2 || seen[href[1:]] {
			continue
		}
		seen[href[1:]] = true
		out = append(out, href[1:])
	}
	return out
}

// resolveImage は img の src を絶対 URL にする。テンプレート画像なら空。
func resolveImage(base *url.URL, src string) string {
	if src == "" {
		return ""
	}
	u, err := base.Parse(src)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	p := u.Path
	if strings.Contains(p, "/i/templates/") || strings.Contains(p, "/etc/designs/") ||
		strings.Contains(p, "Feedback") || strings.HasSuffix(p, "/note.gif") {
		return ""
	}
	return u.String()
}

// converter は 1 章分の変換の状態。行番号を数えながら out に書く。
type converter struct {
	base   *url.URL
	images map[string]string
	res    *Converted
	emph   map[string]int // 強調の入れ子 (mark → 深さ)。内側では付け直さない
	ch     domain.Chapter
	tail   string // out の末尾 2 文字。空行の判定に使う
	anchor string // 直近の article の id
	out    strings.Builder
	line   int  // 次に書く行の番号 (1 始まり)
	depth  int  // 直近の topictitle のレベル。sectiontitle はその 1 つ下になる
	inRef  bool // 直近の article がコマンド (reference かつ構文あり) で、見出しをまだ記録していない
	inCell bool // 表のセルの中 (コードブロックを使えない)
}

// sub はリスト項目やセルの中身を別に組むための子 converter。見出しは親に記録しない
// (これらの中に見出しは来ない)。
func (c *converter) sub() *converter {
	return &converter{ch: c.ch, base: c.base, images: c.images, res: c.res, depth: c.depth, anchor: c.anchor, inCell: c.inCell, line: 1}
}

func (c *converter) write(s string) {
	if s == "" {
		return
	}
	c.out.WriteString(s)
	c.line += strings.Count(s, "\n")
	t := c.tail + s
	if len(t) > 2 {
		t = t[len(t)-2:]
	}
	c.tail = t
}

// blank はブロックの前に空行を置く。先頭では置かない。
func (c *converter) blank() {
	if c.out.Len() == 0 {
		return
	}
	if !strings.HasSuffix(c.tail, "\n") {
		c.write("\n")
	}
	if c.tail != "\n\n" {
		c.write("\n")
	}
}

// block は 1 ブロックを空行で区切って書く。
func (c *converter) block(s string) {
	s = strings.TrimRight(s, "\n")
	if strings.TrimSpace(s) == "" {
		return
	}
	c.blank()
	c.write(s + "\n")
}

// text はサブ変換の結果を前後の空白を落として返す。
func (c *converter) text() string { return strings.TrimSpace(c.out.String()) }

func (c *converter) warnf(format string, args ...any) {
	c.res.Warnings = append(c.res.Warnings, fmt.Sprintf(format, args...))
}

// --- ブロック ---

func isInlineNode(n *html.Node) bool {
	switch n.Type {
	case html.TextNode:
		return true
	case html.ElementNode:
		switch n.Data {
		case "a", "abbr", "b", "bdi", "big", "br", "cite", "code", "dfn", "em", "font", "i", "kbd", "label",
			"mark", "q", "s", "samp", "small", "span", "strong", "sub", "sup", "tt", "u", "var", "wbr":
			return true
		}
		return false
	default:
		return false
	}
}

// children は n の子を順に処理する。要素の直下に置かれたテキストやインライン要素の
// 並びは 1 つの段落にまとめる (<li><b>Step 1</b> …</li> のような書き方に対応)。
func (c *converter) children(n *html.Node) {
	var run []*html.Node
	flush := func() {
		if len(run) == 0 {
			return
		}
		if t := tidyParagraph(c.inlines(run)); t != "" {
			c.block(t)
		}
		run = nil
	}
	for x := n.FirstChild; x != nil; x = x.NextSibling {
		if isInlineNode(x) {
			run = append(run, x)
			continue
		}
		flush()
		c.node(x)
	}
	flush()
}

// tidyParagraph は段落の各行の空白を 1 つに畳み、空行を落とす (<br> による改行は残る)。
func tidyParagraph(s string) string {
	var lines []string
	for l := range strings.SplitSeq(s, "\n") {
		if l = strings.Join(strings.Fields(l), " "); l != "" {
			lines = append(lines, l)
		}
	}
	return strings.Join(lines, "\n")
}

// node はブロック要素 1 つを処理する。本文の骨格 (トピック・見出し・段落) はここで、
// 表・リスト・図は compound で分ける。
func (c *converter) node(x *html.Node) {
	if x.Type != html.ElementNode {
		return
	}
	switch x.Data {
	case "script", "style", "nav", "noscript", "button", "form", "input", "select", "template", "iframe", "svg", "head", "hr", "br":
		return
	case "article":
		c.topic(x)
	case "h1", "h2", "h3", "h4", "h5", "h6":
		c.heading(x)
	case "p":
		c.paragraph(x)
	case "pre":
		c.codeBlock(rawTextOf(x))
	default:
		c.compound(x)
	}
}

func (c *converter) compound(x *html.Node) {
	switch x.Data {
	case "table":
		c.table(x)
	case "ul", "ol":
		if !isMinitoc(x) {
			c.list(x)
		}
	case "section":
		c.section(x)
	case "dl":
		c.dl(x)
	case "img":
		if s := c.image(x); s != "" {
			c.block(s)
		}
	default:
		// div, header, main, aside, figure, figcaption, blockquote, td, li …
		c.children(x)
	}
}

// section は <section>。トピック (古い形式) と構文だけ別に扱い、他は中身をそのまま続ける。
func (c *converter) section(x *html.Node) {
	switch {
	case isLegacyTopic(x):
		c.topic(x)
	case hasClass(x, "refsyn"):
		c.refsyn(x)
	default:
		c.children(x)
	}
}

// isLegacyTopic は古い技術ノート形式のトピック。<article> ではなく <section class="nestedN" id="…"> で組まれる。
func isLegacyTopic(x *html.Node) bool {
	_, nested := hasClassPrefix(x, "nested")
	return nested && attr(x, "id") != ""
}

// isMinitoc は本文に埋め込まれた子トピックの一覧 (<ul> の li が全部 olchildlink) か。
// 新しいページでは <nav> に入っていて node() が捨てるが、古い技術ノート形式では裸で置かれる。
func isMinitoc(ul *html.Node) bool {
	n := 0
	for li := ul.FirstChild; li != nil; li = li.NextSibling {
		if !isElem(li, "li") {
			continue
		}
		if !hasClass(li, "olchildlink") {
			return false
		}
		n++
	}
	return n > 0
}

// topic は <article class="topic …">。id がアンカー、reference かつ構文ありならコマンド。
func (c *converter) topic(x *html.Node) {
	prevAnchor, prevRef, prevDepth := c.anchor, c.inRef, c.depth
	if id := attr(x, "id"); id != "" {
		c.anchor = id
	}
	c.inRef = hasClass(x, "reference") && hasCommandSyntax(x)
	c.children(x)
	c.anchor, c.inRef, c.depth = prevAnchor, prevRef, prevDepth
}

// hasCommandSyntax は article 直下の本文にコマンド構文 (section.refsyn) があるか。入れ子の article は見ない。
//
// 設定ガイドは「Restrictions for …」のような箇条書きにも refsyn を使うので、refsyn が
// あるだけではコマンドと言えない。構文ブロック (p.synblk) があるか、refsyn がインラインの
// 並びだけで組まれているものをコマンドとみなす。
func hasCommandSyntax(article *html.Node) bool {
	refsyns := findAll(article,
		func(n *html.Node) bool { return isElem(n, "section") && hasClass(n, "refsyn") },
		func(n *html.Node) bool { return n == article || !isElem(n, "article") })
	return slices.ContainsFunc(refsyns, isCommandSyntax)
}

func isCommandSyntax(refsyn *html.Node) bool {
	if find(refsyn, func(n *html.Node) bool { return hasClass(n, "synblk") }) != nil {
		return true
	}
	for x := refsyn.FirstChild; x != nil; x = x.NextSibling {
		if x.Type == html.ElementNode && !isInlineNode(x) {
			return false
		}
	}
	return true
}

// refsyn は構文の section。構文ブロック (p.synblk) は paragraph が **Syntax:** にする。
// ブロックを持たずインラインだけで組まれた構文は、section 全体を 1 行の構文にする。
func (c *converter) refsyn(x *html.Node) {
	if find(x, func(n *html.Node) bool { return hasClass(n, "synblk") }) == nil && isCommandSyntax(x) {
		if t := textOf(x); t != "" {
			c.block("**Syntax:** " + codeSpan(t))
		}
		return
	}
	c.children(x)
}

// headingKind は見出し要素の種類。
type headingKind struct {
	level int
	topic bool // topictitleN (トピックの見出し。索引に載せる)
	group bool // <p class="topictitle1"> のグループ見出し (DITA の topichead)
}

func (c *converter) classify(x *html.Node) headingKind {
	k := headingKind{level: c.depth + 1}
	if cls, ok := hasClassPrefix(x, "topictitle"); ok {
		if n, err := strconv.Atoi(strings.TrimPrefix(cls, "topictitle")); err == nil && n > 0 {
			k.level, k.topic = n, true
		}
	}
	// グループ見出しは article を持たず章の直下に置かれ、続く article と同じ深さで並ぶ。
	if k.group = isElem(x, "p"); k.group {
		k.level++
	}
	k.level = max(1, min(k.level, 6))
	return k
}

func (c *converter) heading(x *html.Node) {
	title := textOf(x)
	if title == "" {
		return
	}
	k := c.classify(x)
	c.blank()
	line := c.line
	c.write(strings.Repeat("#", k.level) + " " + title + "\n")
	if !k.topic {
		return
	}
	anchor := c.anchor
	if k.group {
		anchor = attr(x, "id")
	} else {
		c.depth = k.level
	}
	c.record(k, title, anchor, line)
}

// record は見出しを索引に載せる。コマンド (reference トピックの見出し) は commands にも載せる。
func (c *converter) record(k headingKind, title, anchor string, line int) {
	src := c.ch.URL
	if anchor != "" {
		src += "#" + anchor
	}
	c.res.Sections = append(c.res.Sections, domain.Section{
		Anchor: anchor, Title: title, Level: k.level, File: c.ch.MarkdownFile(), Line: line, Source: src,
	})
	if c.inRef && !k.group {
		c.inRef = false
		c.res.Entries = append(c.res.Entries, domain.Entry{
			Command: domain.NormalizeCommand(title), Title: title, Anchor: anchor,
			File: c.ch.MarkdownFile(), Line: line, Source: src,
		})
	}
}

func (c *converter) paragraph(x *html.Node) {
	_, groupHeading := hasClassPrefix(x, "topictitle")
	switch {
	case groupHeading:
		c.heading(x)
	case hasClass(x, "synblk"):
		// コマンド構文。[ ] { } | を含むのでコードスパンに入れる。
		if t := textOf(x); t != "" {
			c.block("**Syntax:** " + codeSpan(t))
		}
	case hasClass(x, "lines"):
		// 行の折り返しを保つ段落 (Command Modes など)。
		c.codeBlock(rawTextOf(x))
	default:
		c.children(x)
	}
}

func (c *converter) codeBlock(s string) {
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.Trim(s, "\n")
	s = strings.TrimRight(s, " \t")
	if strings.TrimSpace(s) == "" {
		return
	}
	if c.inCell {
		// 表のセルではコードブロックを開けないので、行ごとのコードスパンにする。
		lines := strings.Split(s, "\n")
		for i, l := range lines {
			lines[i] = codeSpan(l)
		}
		c.block(strings.Join(lines, "\n"))
		return
	}
	fence := "```"
	for strings.Contains(s, fence) {
		fence += "`"
	}
	c.block(fence + "\n" + s + "\n" + fence)
}

func (c *converter) list(x *html.Node) {
	ordered := x.Data == "ol"
	var items []string
	for li := x.FirstChild; li != nil; li = li.NextSibling {
		if !isElem(li, "li") {
			continue
		}
		s := c.sub()
		s.children(li)
		text := s.text()
		if text == "" {
			continue
		}
		prefix := "- "
		if ordered {
			prefix = fmt.Sprintf("%d. ", len(items)+1)
		}
		items = append(items, listItem(prefix, text))
	}
	c.block(strings.Join(items, "\n"))
}

// listItem は複数行の本文をリスト項目にする。2 行目以降は prefix の幅だけ下げる。
func listItem(prefix, text string) string {
	indent := strings.Repeat(" ", len(prefix))
	lines := strings.Split(text, "\n")
	var b strings.Builder
	b.WriteString(prefix + lines[0])
	for _, l := range lines[1:] {
		b.WriteString("\n")
		if l != "" {
			b.WriteString(indent + l)
		}
	}
	return b.String()
}

func (c *converter) dl(x *html.Node) {
	for d := x.FirstChild; d != nil; d = d.NextSibling {
		switch {
		case isElem(d, "dt"):
			if t := textOf(d); t != "" {
				c.block("**" + t + "**")
			}
		case isElem(d, "dd"):
			c.children(d)
		}
	}
}

// image は本文の図。取得済みなら images/ への相対リンク、無ければ元 URL のまま。
func (c *converter) image(x *html.Node) string {
	u := resolveImage(c.base, attr(x, "src"))
	if u == "" {
		return ""
	}
	alt := strings.ReplaceAll(collapse(attr(x, "alt")), "]", "")
	if name, ok := c.images[u]; ok {
		return "![" + alt + "](images/" + name + ")"
	}
	c.warnf("画像を取得していない: %s", u)
	return "![" + alt + "](" + u + ")"
}
