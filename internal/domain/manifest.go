package domain

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// Kind は資料の取り方。cisco.com の資料は 2 種類の組み方で配られている。
type Kind string

const (
	// KindBook は目次ページ (ul#bookToc) から章ページを 1 つずつ辿る冊子。
	KindBook Kind = "book"
	// KindPage は 1 ページで完結する資料 (リリースノートなどの eot テンプレート)。
	KindPage Kind = "page"
)

// Train はソフトウェアのトレイン (8.5 / 8.10)。変換結果の第 1 階層になる。
//
// 2504 は 8.5 で、3504 / 5520 / 8540 / vWLC は 8.10 で最終になる。読む側は
// 機種からトレインを選び、そのトレインの冊子だけを引く。
type Train struct {
	ID        string   `json:"id"`        // "8-5" のようにディレクトリ名に使える形
	Title     string   `json:"title"`     // 表示名 "Cisco Wireless Release 8.5"
	Note      string   `json:"note"`      // README に転記する補足
	Platforms []string `json:"platforms"` // このトレインが最終リリースとなる機種
}

// Doc は取得する資料 1 冊。URL は配布ページを見て人が転記する (推測しない)。
type Doc struct {
	Train string `json:"train"`
	Book  string `json:"book"`
	Kind  Kind   `json:"kind"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

// Key は "<train>/<book>"。-only の指定と進捗表示に使う。
func (d Doc) Key() string { return d.Train + "/" + d.Book }

// Manifest は取得する資料の一覧。build の唯一の入力。
type Manifest struct {
	Trains []Train `json:"trains"`
	Docs   []Doc   `json:"docs"`
}

// Train は id のトレインを返す。
func (m Manifest) Train(id string) (Train, bool) {
	for _, t := range m.Trains {
		if t.ID == id {
			return t, true
		}
	}
	return Train{}, false
}

// Select は key ("<train>/<book>") に一致する資料だけ返す。key が空なら全部。
func (m Manifest) Select(key string) []Doc {
	if key == "" {
		return m.Docs
	}
	var out []Doc
	for _, d := range m.Docs {
		if d.Key() == key {
			out = append(out, d)
		}
	}
	return out
}

// Validate は読み込んだマニフェストの整合を確かめる。
//
// train と book はそのままディレクトリ名になるので、区切り文字や相対参照を含めない。
// 同じ key が 2 度出ると出力先が衝突するので断る。
func (m Manifest) Validate() error {
	if len(m.Docs) == 0 {
		return errors.New("マニフェストに docs がありません")
	}
	for _, t := range m.Trains {
		if err := validateDirName("trains[].id", t.ID); err != nil {
			return err
		}
	}
	seen := map[string]bool{}
	for i, d := range m.Docs {
		where := fmt.Sprintf("docs[%d]", i)
		if err := m.validateDoc(where, d); err != nil {
			return err
		}
		if seen[d.Key()] {
			return fmt.Errorf("%s: %s が重複", where, d.Key())
		}
		seen[d.Key()] = true
	}
	return nil
}

func (m Manifest) validateDoc(where string, d Doc) error {
	if err := validateDirName(where+".train", d.Train); err != nil {
		return err
	}
	if err := validateDirName(where+".book", d.Book); err != nil {
		return err
	}
	if _, ok := m.Train(d.Train); !ok {
		return fmt.Errorf("%s: train %q が trains にありません", where, d.Train)
	}
	if err := d.Kind.validate(); err != nil {
		return fmt.Errorf("%s: %w", where, err)
	}
	if d.Title == "" {
		return fmt.Errorf("%s: title が空", where)
	}
	if u, err := url.Parse(d.URL); err != nil || u.Scheme != "https" || u.Host == "" {
		return fmt.Errorf("%s: url は https の絶対 URL にする: %q", where, d.URL)
	}
	return nil
}

func (k Kind) validate() error {
	switch k {
	case KindBook, KindPage:
		return nil
	case "":
		return errors.New(`kind が無い。"book" (目次から章を辿る) か "page" (1 ページ) を書く`)
	default:
		return fmt.Errorf("kind %q は知らない", k)
	}
}

func validateDirName(where, s string) error {
	if s == "" || s == "." || s == ".." || strings.ContainsAny(s, `/\: \t`) {
		return fmt.Errorf("%s: ディレクトリ名として使えない: %q", where, s)
	}
	return nil
}
