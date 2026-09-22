package infrastructure

import (
	"regexp"
	"slices"
	"strings"

	"golang.org/x/net/html"
)

// DOM を歩くための小さな道具。x/net/html の Node をそのまま使う。

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func hasClass(n *html.Node, class string) bool {
	return slices.Contains(strings.Fields(attr(n, "class")), class)
}

// hasClassPrefix は "topictitle2" のような接尾辞付きのクラスを接頭辞で探す。
func hasClassPrefix(n *html.Node, prefix string) (string, bool) {
	for c := range strings.FieldsSeq(attr(n, "class")) {
		if strings.HasPrefix(c, prefix) {
			return c, true
		}
	}
	return "", false
}

func isElem(n *html.Node, name string) bool {
	return n != nil && n.Type == html.ElementNode && n.Data == name
}

// findByID は id の要素を深さ優先で探す。
func findByID(n *html.Node, id string) *html.Node {
	return find(n, func(x *html.Node) bool { return x.Type == html.ElementNode && attr(x, "id") == id })
}

// find は pred を満たす最初の子孫 (自身を含む) を返す。
func find(n *html.Node, pred func(*html.Node) bool) *html.Node {
	if pred(n) {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if r := find(c, pred); r != nil {
			return r
		}
	}
	return nil
}

// findAll は pred を満たす子孫 (自身を含む) を文書順に集める。
// descend が false を返す要素の下には潜らない。
func findAll(n *html.Node, pred, descend func(*html.Node) bool) []*html.Node {
	var out []*html.Node
	var walk func(*html.Node)
	walk = func(x *html.Node) {
		if pred(x) {
			out = append(out, x)
		}
		if descend != nil && !descend(x) {
			return
		}
		for c := x.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return out
}

// firstChildElem は name の最初の子要素。
func firstChildElem(n *html.Node, name string) *html.Node {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if isElem(c, name) {
			return c
		}
	}
	return nil
}

var spaces = regexp.MustCompile(`[ \t\r\n\f]+`)

// collapse は HTML の空白 (改行を含む) を 1 つの空白に畳む。前後は残す。
func collapse(s string) string { return spaces.ReplaceAllString(s, " ") }

// textOf は子孫のテキストを (タグを外して) 連結し、空白を畳んで前後を落とす。
func textOf(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(x *html.Node) {
		switch x.Type {
		case html.TextNode:
			b.WriteString(x.Data)
		case html.ElementNode:
			if x.Data == "br" {
				b.WriteString(" ")
			}
			for c := x.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
		}
	}
	walk(n)
	return strings.TrimSpace(collapse(b.String()))
}

// rawTextOf は <pre> の中身のように、改行をそのまま残してテキストを取る。
func rawTextOf(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(x *html.Node) {
		switch x.Type {
		case html.TextNode:
			b.WriteString(x.Data)
		case html.ElementNode:
			if x.Data == "br" {
				b.WriteString("\n")
			}
			for c := x.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
		}
	}
	walk(n)
	return b.String()
}
