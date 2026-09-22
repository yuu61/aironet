package infrastructure

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"golang.org/x/net/html"

	"github.com/yuu61/aironet/internal/domain"
)

// 冊子の目次ページ (b-cr85.html など) の読み方。
//
// 章の一覧は <ul id="bookToc"> にある。8.5 のコマンドリファレンスは
// <li><a href>章</a></li> が平らに並び、8.10 以降と設定ガイドは
// <li><button>パート名</button><ul><li><a>章</a></li>…</ul></li> と入れ子になる。
// 両方を同じ関数で読み、入れ子のパート名は Chapter.Part に持つ。
// 末尾の Index (…_CLT_chapter.html) は索引ページなので章に数えない。

// ParseBookTOC は目次ページから章の一覧を読む。pageURL は相対リンクの起点。
func ParseBookTOC(page []byte, pageURL string) ([]domain.Chapter, error) {
	doc, err := html.Parse(bytes.NewReader(page))
	if err != nil {
		return nil, err
	}
	base, err := url.Parse(pageURL)
	if err != nil {
		return nil, err
	}
	toc := findByID(doc, "bookToc")
	if toc == nil {
		return nil, errors.New("目次 (ul#bookToc) が無い。冊子の目次ページの URL か確かめる")
	}
	w := &tocWalker{base: base}
	w.walk(toc, "")
	if len(w.chapters) == 0 {
		return nil, errors.New("目次 (ul#bookToc) に章のリンクが無い")
	}
	if err := checkDistinctFiles(w.chapters); err != nil {
		return nil, err
	}
	return w.chapters, nil
}

type tocWalker struct {
	base     *url.URL
	chapters []domain.Chapter
}

// walk は <ul> の直下の <li> を順に読む。<button> を持つ li はパートで、配下の <ul> に章が並ぶ。
func (w *tocWalker) walk(ul *html.Node, part string) {
	for li := ul.FirstChild; li != nil; li = li.NextSibling {
		if !isElem(li, "li") {
			continue
		}
		if b := firstChildElem(li, "button"); b != nil {
			if sub := firstChildElem(li, "ul"); sub != nil {
				w.walk(sub, textOf(b))
			}
			continue
		}
		if a := firstChildElem(li, "a"); a != nil {
			w.add(a, part)
		}
		// 章の下にさらに <ul> があっても (見たことは無いが) 読む。
		if sub := firstChildElem(li, "ul"); sub != nil {
			w.walk(sub, part)
		}
	}
}

func (w *tocWalker) add(a *html.Node, part string) {
	href := attr(a, "href")
	if href == "" || strings.Contains(href, "_CLT_chapter") {
		return
	}
	u, err := w.base.Parse(href)
	if err != nil {
		return
	}
	u.Fragment, u.RawQuery = "", ""
	w.chapters = append(w.chapters, domain.Chapter{
		File:  domain.ChapterFile(u.String()),
		Title: textOf(a),
		Part:  part,
		URL:   u.String(),
	})
}

// checkDistinctFiles は章の basename が衝突しないことを確かめる (出力ファイル名になるため)。
func checkDistinctFiles(chapters []domain.Chapter) error {
	seen := map[string]string{}
	for _, c := range chapters {
		if prev, ok := seen[c.File]; ok && prev != c.URL {
			return fmt.Errorf("章のファイル名 %q が衝突: %s と %s", c.File, prev, c.URL)
		}
		seen[c.File] = c.URL
	}
	return nil
}

// PageTitle は <title> から " - Cisco" を除いたもの。マニフェストに title が無いときの補い。
func PageTitle(page []byte) string {
	doc, err := html.Parse(bytes.NewReader(page))
	if err != nil {
		return ""
	}
	t := find(doc, func(n *html.Node) bool { return isElem(n, "title") })
	if t == nil {
		return ""
	}
	return strings.TrimSpace(strings.TrimSuffix(textOf(t), " - Cisco"))
}
