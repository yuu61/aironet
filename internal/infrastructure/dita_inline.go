package infrastructure

import (
	"path"
	"strings"

	"golang.org/x/net/html"

	"github.com/yuu61/aironet/internal/domain"
)

// インライン要素の変換。強調・コード・リンク・図。

func (c *converter) inlines(nodes []*html.Node) string {
	var b strings.Builder
	for _, n := range nodes {
		b.WriteString(c.inline(n))
	}
	return b.String()
}

func (c *converter) inlineChildren(n *html.Node) string {
	var b strings.Builder
	for x := n.FirstChild; x != nil; x = x.NextSibling {
		b.WriteString(c.inline(x))
	}
	return b.String()
}

func (c *converter) inline(n *html.Node) string {
	switch n.Type {
	case html.TextNode:
		return collapse(n.Data)
	case html.ElementNode:
		return c.inlineElem(n)
	default:
		return ""
	}
}

func (c *converter) inlineElem(n *html.Node) string {
	if mark, ok := emphasisMark(n.Data); ok {
		return c.emphasis(n, mark)
	}
	switch n.Data {
	case "br":
		return "\n"
	case "img":
		return c.image(n)
	case "a":
		return c.link(n)
	case "code", "samp", "tt":
		return c.codeInline(n)
	case "kbd":
		return c.kbdInline(n)
	case "span":
		return c.spanInline(n)
	case "sup", "sub":
		return "<" + n.Data + ">" + c.inlineChildren(n) + "</" + n.Data + ">"
	default:
		// ブロック要素がインラインの並びに紛れていたら、中身だけ続ける。
		return c.inlineChildren(n)
	}
}

func emphasisMark(tag string) (string, bool) {
	switch tag {
	case "b", "strong":
		return "**", true
	case "i", "em", "var", "cite", "dfn":
		return "*", true
	default:
		return "", false
	}
}

// kbdInline は <kbd>。sep (構文の区切り記号) はそのまま、userinput は太字、他はコード。
func (c *converter) kbdInline(n *html.Node) string {
	switch {
	case hasClass(n, "sep"):
		return c.inlineChildren(n)
	case hasClass(n, "userinput"):
		return c.emphasis(n, "**")
	default:
		return c.codeInline(n)
	}
}

// spanInline は <span>。DITA の役割クラス (kwd = キーワード、uicontrol = 画面の部品 …) で見分ける。
func (c *converter) spanInline(n *html.Node) string {
	switch {
	case hasClass(n, "kwd"), hasClass(n, "uicontrol"), hasClass(n, "b"), hasClass(n, "wintitle"), hasClass(n, "userinput"):
		return c.emphasis(n, "**")
	case hasClass(n, "i"), hasClass(n, "varname"), hasClass(n, "cite"):
		return c.emphasis(n, "*")
	case hasClass(n, "codeph"), hasClass(n, "filepath"), hasClass(n, "systemoutput"):
		return c.codeInline(n)
	default:
		return c.inlineChildren(n)
	}
}

// emphasis は mark で囲む。前後の空白は外に出し、入れ子は外側だけ付ける。
func (c *converter) emphasis(n *html.Node, mark string) string {
	if c.emph == nil {
		c.emph = map[string]int{}
	}
	if c.emph[mark] > 0 {
		return c.inlineChildren(n)
	}
	c.emph[mark]++
	inner := c.inlineChildren(n)
	c.emph[mark]--
	return wrap(inner, mark)
}

func wrap(inner, mark string) string {
	core := strings.TrimSpace(inner)
	if core == "" {
		return inner
	}
	core = strings.ReplaceAll(core, "\n", " ")
	lead := inner[:len(inner)-len(strings.TrimLeft(inner, " \n"))]
	trail := inner[len(strings.TrimRight(inner, " \n")):]
	return lead + mark + core + mark + trail
}

func (c *converter) codeInline(n *html.Node) string {
	raw := collapse(rawTextOf(n))
	core := strings.TrimSpace(raw)
	if core == "" {
		return raw
	}
	lead := raw[:len(raw)-len(strings.TrimLeft(raw, " "))]
	trail := raw[len(strings.TrimRight(raw, " ")):]
	return lead + codeSpan(core) + trail
}

func codeSpan(s string) string {
	if strings.Contains(s, "`") {
		return "`` " + s + " ``"
	}
	return "`" + s + "`"
}

// link は <a href>。同じ章の中はテキストだけ、同じ冊子の別の章は <章>.md、それ以外は絶対 URL。
func (c *converter) link(n *html.Node) string {
	text := c.inlineChildren(n)
	href := attr(n, "href")
	t := strings.TrimSpace(text)
	if href == "" || t == "" || strings.HasPrefix(href, "#") {
		return text
	}
	u, err := c.base.Parse(href)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return text
	}
	dest, ok := c.chapterLink(u.String())
	if !ok {
		return text
	}
	return "[" + t + "](" + dest + ")"
}

// chapterLink はリンク先。同じ冊子の別の章なら <章>.md、同じ章なら無し (ok=false)、それ以外は URL のまま。
func (c *converter) chapterLink(abs string) (string, bool) {
	u, err := c.base.Parse(abs)
	if err != nil {
		return abs, true
	}
	if u.Host != c.base.Host || path.Dir(u.Path) != path.Dir(c.base.Path) || !strings.HasSuffix(u.Path, ".html") {
		return abs, true
	}
	file := domain.ChapterFile(abs)
	if file == c.ch.File {
		return "", false
	}
	return file + ".md", true
}
