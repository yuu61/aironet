package infrastructure

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/yuu61/aironet/internal/domain"
)

// 変換結果の書き出し。章ごとのディレクトリとトピックの Markdown、索引 (commands.tsv /
// sections.tsv)、README、図。
//
// 索引の列は ix-toolkit の ix-manual が読む形と同じにしてある。読む側は file のファイルを
// 開き、line の見出し行から読む。トピック 1 つが 1 ファイルなので丸ごと読んでも足りる。

// ResetBookDir は冊子の置き場を空にする。章やトピックの構成が変わったとき、前回の変換結果が
// 残って重複しないようにする。生成物の目印 (README.md) が無い非空のディレクトリは変換結果では
// ないので消さずに止める。
func ResetBookDir(bookDir string) error {
	entries, err := os.ReadDir(bookDir)
	if errors.Is(err, fs.ErrNotExist) {
		return os.MkdirAll(bookDir, 0o755)
	}
	if err != nil {
		return err
	}
	if len(entries) > 0 && !slices.ContainsFunc(entries, func(e fs.DirEntry) bool { return e.Name() == "README.md" }) {
		return fmt.Errorf("%s: 変換結果ではないファイルがある (README.md が無い)。消さずに止める。別の置き場を -manuals で指定する", bookDir)
	}
	if err := os.RemoveAll(bookDir); err != nil {
		return err
	}
	return os.MkdirAll(bookDir, 0o755)
}

// WriteParts は分割済みの Markdown を <章>/ の下に書く。
func WriteParts(bookDir string, parts []domain.Part) error {
	for _, p := range parts {
		dst := filepath.Join(bookDir, filepath.FromSlash(p.Path))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, []byte(p.Markdown), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// WriteIndexes は commands.tsv と sections.tsv を書く。
func WriteIndexes(bookDir string, entries []domain.Entry, sections []domain.Section) error {
	sorted := append([]domain.Entry(nil), entries...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Command != sorted[j].Command {
			return sorted[i].Command < sorted[j].Command
		}
		if sorted[i].File != sorted[j].File {
			return sorted[i].File < sorted[j].File
		}
		return sorted[i].Line < sorted[j].Line
	})
	var t strings.Builder
	t.WriteString("command\tentry\tfile\tline\tsource\n")
	for _, e := range sorted {
		fmt.Fprintf(&t, "%s\t%s\t%s\t%d\t%s\n", domain.TSVCell(e.Command), domain.TSVCell(e.Title), e.File, e.Line, e.Source)
	}
	if err := os.WriteFile(filepath.Join(bookDir, "commands.tsv"), []byte(t.String()), 0o644); err != nil {
		return err
	}

	var s strings.Builder
	s.WriteString("section\ttitle\tfile\tline\tsource\n")
	for _, sec := range sections {
		fmt.Fprintf(&s, "%s\t%s\t%s\t%d\t%s\n", sec.ID(), domain.TSVCell(sec.Title), sec.File, sec.Line, sec.Source)
	}
	return os.WriteFile(filepath.Join(bookDir, "sections.tsv"), []byte(s.String()), 0o644)
}

// CopyImages は取得キャッシュの図を冊子の images/ に写す。
func CopyImages(cacheBookDir, bookDir string, images map[string]string) error {
	if len(images) == 0 {
		return nil
	}
	dst := filepath.Join(bookDir, "images")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	for _, name := range images {
		if err := copyFile(filepath.Join(ImageCacheDir(cacheBookDir), name), filepath.Join(dst, name)); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// ReadmeInfo は README に書く内容。
type ReadmeInfo struct {
	Cache     BookCache
	Doc       domain.Doc
	Train     domain.Train
	NParts    int
	NEntries  int
	NSections int
}

// WriteReadme は冊子の README.md を書く。出典・トレイン・機種・引き方・章の一覧。
func WriteReadme(bookDir string, info ReadmeInfo) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s (Markdown 変換版)\n\n", info.Doc.Title)
	b.WriteString("この配下は cisco.com の資料から機械変換した生成物であり、著作権は Cisco Systems, Inc. に帰属する。\n")
	b.WriteString("**再配布しないこと。** リポジトリでは `.gitignore` により除外されている。\n")
	writeOrigin(&b, info)
	writeUsage(&b, info)
	writeChapterList(&b, info.Cache.Chapters)
	return os.WriteFile(filepath.Join(bookDir, "README.md"), []byte(b.String()), 0o644)
}

func writeOrigin(b *strings.Builder, info ReadmeInfo) {
	b.WriteString("\n## 生成条件\n\n")
	fmt.Fprintf(b, "- 元資料: %s\n", info.Doc.URL)
	fmt.Fprintf(b, "- 取得日: %s\n", info.Cache.FetchedAt)
	train := info.Train.ID
	if info.Train.Title != "" {
		train += " (" + info.Train.Title + ")"
	}
	fmt.Fprintf(b, "- トレイン: %s\n", train)
	if len(info.Train.Platforms) > 0 {
		fmt.Fprintf(b, "- このトレインが最終リリースとなる機種: %s\n", strings.Join(info.Train.Platforms, ", "))
	}
	if info.Train.Note != "" {
		fmt.Fprintf(b, "- 補足: %s\n", info.Train.Note)
	}
	fmt.Fprintf(b, "- 章数: %d / ファイル数: %d / 見出し数: %d / コマンド項目数: %d\n", len(info.Cache.Chapters), info.NParts, info.NSections, info.NEntries)
	b.WriteString("- 変換経路: cisco.com の章ページ (DITA 由来の HTML) をトピックと section の構造どおりに読んだもの (表は Markdown の表、図は images/ に取得)\n")
}

func writeUsage(b *strings.Builder, info ReadmeInfo) {
	b.WriteString("\n## 引き方\n\n")
	b.WriteString("- `<章>/<トピック>.md` — 本文。章 (元の 1 ページ) をディレクトリにし、章直下のトピック (コマンド 1 つ、\n")
	fmt.Fprintf(b, "  1 機能) を 1 ファイルにしてある。%d KB を超えるトピックは子トピックをさらに別ファイルにし、元のファイルに一覧リンクを残す。\n", domain.MaxPartBytes/1024)
	b.WriteString("  ファイル名は見出しから作る (`config aaa auth` → `config_aaa_auth.md`)。1 ファイルを丸ごと読んで足りる大きさにしてある。\n")
	b.WriteString("- `<章>/README.md` — 章タイトル、章直下の本文、章のトピック一覧。\n")
	if info.NEntries > 0 {
		b.WriteString("- `commands.tsv` — `command / entry / file / line / source` のタブ区切り索引。コマンド名から引く。\n")
		b.WriteString("  `file` はそのコマンドのファイル、`line` はその中の見出し行 (1 始まり)。\n")
	}
	b.WriteString("- `sections.tsv` — `section / title / file / line / source` のタブ区切り索引。見出し語から引く。\n")
	b.WriteString("  `section` は「章#アンカー」(ファイルの分け方に依らない位置)。`source` は元ページの URL とアンカーで、ブラウザでそのまま開ける。\n")
	b.WriteString("- 図は `images/` にあり、本文からは `../images/` で参照する。\n")
}

// writeChapterList は章の一覧。入れ子の目次から来たパート名は小見出しにする。
func writeChapterList(b *strings.Builder, chapters []domain.Chapter) {
	b.WriteString("\n## 章\n")
	part := ""
	for i, ch := range chapters {
		switch {
		case ch.Part != "" && (i == 0 || ch.Part != part):
			fmt.Fprintf(b, "\n### %s\n\n", ch.Part)
		case i == 0 || (ch.Part == "" && part != ""):
			b.WriteString("\n")
		}
		part = ch.Part
		fmt.Fprintf(b, "- [%s](%s)\n", ch.Title, ch.IndexFile())
	}
}
