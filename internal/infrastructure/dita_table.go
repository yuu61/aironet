package infrastructure

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// 表の変換。cisco.com は本物の表のほかに、注記 (table.olh_note) と手順 (table.stepTable) も
// <table> で組んでいるので、クラスで見分けて別の形にする。

func (c *converter) table(x *html.Node) {
	switch {
	case hasClass(x, "olh_note") || attr(x, "role") == "note":
		c.note(x)
	case hasClass(x, "stepTable"):
		c.steps(x)
	default:
		c.gfmTable(x)
	}
}

// cells は tr 直下の td / th。
func cells(tr *html.Node) []*html.Node {
	var out []*html.Node
	for td := tr.FirstChild; td != nil; td = td.NextSibling {
		if isElem(td, "td") || isElem(td, "th") {
			out = append(out, td)
		}
	}
	return out
}

// note は <table class="olh_note"> (Note / Caution / Warning …)。1 行目の左セルがラベル、右セルが本文。
func (c *converter) note(x *html.Node) {
	tr := find(x, func(n *html.Node) bool { return isElem(n, "tr") })
	if tr == nil {
		return
	}
	tds := cells(tr)
	if len(tds) == 0 {
		return
	}
	label, body := "Note", tds[0]
	if len(tds) >= 2 {
		if l := textOf(tds[0]); l != "" {
			label = l
		}
		body = tds[1]
	}
	s := c.sub()
	s.children(body)
	if text := s.text(); text != "" {
		c.block(quote(label, text))
	}
}

var listMarker = regexp.MustCompile(`^(?:[-*] |\d+\. |` + "```" + `|\| )`)

// quote は本文を「> **Note:** …」の引用にする。本文がリストやコードで始まるときは、
// ラベルを 1 行に分けないと先頭の項目が地の文になる。
func quote(label, text string) string {
	lines := strings.Split(text, "\n")
	var b strings.Builder
	if listMarker.MatchString(lines[0]) {
		b.WriteString("> **" + label + ":**\n>\n> " + lines[0])
	} else {
		b.WriteString("> **" + label + ":** " + lines[0])
	}
	for _, l := range lines[1:] {
		b.WriteString("\n>")
		if l != "" {
			b.WriteString(" " + l)
		}
	}
	return b.String()
}

// steps は手順の表 <table class="stepTable">。左の "Step N" は落とし、右セルを番号付きリストにする。
func (c *converter) steps(x *html.Node) {
	var items []string
	for _, tr := range findAll(x, func(n *html.Node) bool { return isElem(n, "tr") }, notNestedTable(x)) {
		tds := cells(tr)
		if len(tds) == 0 {
			continue
		}
		s := c.sub()
		s.children(tds[len(tds)-1])
		if text := s.text(); text != "" {
			items = append(items, listItem(fmt.Sprintf("%d. ", len(items)+1), text))
		}
	}
	c.block(strings.Join(items, "\n"))
}

// notNestedTable は x 自身の行だけを辿り、入れ子の表には潜らない。
func notNestedTable(x *html.Node) func(*html.Node) bool {
	return func(n *html.Node) bool { return n == x || !isElem(n, "table") }
}

// gfmTable は表を GitHub Flavored Markdown の表にする。
// colspan は空セルで埋め、rowspan は下の行に空セルを補う。thead が無い表は空の見出し行を置く。
func (c *converter) gfmTable(x *html.Node) {
	var caption string
	if capt := firstChildElem(x, "caption"); capt != nil {
		caption = textOf(capt)
	}
	g := &tableGrid{pending: map[int]int{}}
	for part := x.FirstChild; part != nil; part = part.NextSibling {
		switch {
		case isElem(part, "thead"), isElem(part, "tbody"), isElem(part, "tfoot"):
			for tr := part.FirstChild; tr != nil; tr = tr.NextSibling {
				if isElem(tr, "tr") {
					g.addRow(c, tr, isElem(part, "thead"))
				}
			}
		case isElem(part, "tr"):
			g.addRow(c, part, false)
		}
	}
	if s := g.render(caption); s != "" {
		c.block(s)
	}
}

// tableGrid は表の行を集める。rowspan の続きは下の行に空セルとして補う。
type tableGrid struct {
	pending    map[int]int // 列 → まだ埋める行数 (rowspan)
	rows       [][]string
	headerRows int
}

func (g *tableGrid) addRow(c *converter, tr *html.Node, header bool) {
	var row []string
	col := 0
	fill := func() {
		for g.pending[col] > 0 {
			row = append(row, "")
			g.pending[col]--
			col++
		}
	}
	for _, cell := range cells(tr) {
		fill()
		text := c.cellText(cell)
		cs := max(1, atoi(attr(cell, "colspan")))
		rs := max(1, atoi(attr(cell, "rowspan")))
		for k := range cs {
			if k == 0 {
				row = append(row, text)
			} else {
				row = append(row, "")
			}
			if rs > 1 {
				g.pending[col] = rs - 1
			}
			col++
		}
	}
	fill()
	if header && len(g.rows) == g.headerRows {
		g.headerRows++
	}
	g.rows = append(g.rows, row)
}

func (g *tableGrid) render(caption string) string {
	ncols := 0
	for _, r := range g.rows {
		ncols = max(ncols, len(r))
	}
	if ncols == 0 {
		return ""
	}
	for i := range g.rows {
		for len(g.rows[i]) < ncols {
			g.rows[i] = append(g.rows[i], "")
		}
	}
	header, body := make([]string, ncols), g.rows
	if g.headerRows > 0 {
		header, body = g.rows[0], g.rows[1:]
	}
	var b strings.Builder
	if caption != "" {
		b.WriteString("**" + caption + "**\n\n")
	}
	b.WriteString("| " + strings.Join(header, " | ") + " |\n")
	b.WriteString("|" + strings.Repeat(" --- |", ncols) + "\n")
	for _, r := range body {
		b.WriteString("| " + strings.Join(r, " | ") + " |\n")
	}
	return b.String()
}

// cellText はセルの中身を 1 行にする。段落は <br> で区切り、| は逃がす。
func (c *converter) cellText(cell *html.Node) string {
	s := c.sub()
	s.inCell = true
	s.children(cell)
	var lines []string
	for l := range strings.SplitSeq(s.out.String(), "\n") {
		if l = strings.Join(strings.Fields(l), " "); l != "" {
			lines = append(lines, l)
		}
	}
	return strings.ReplaceAll(strings.Join(lines, "<br>"), "|", `\|`)
}

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}
