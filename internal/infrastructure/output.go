package infrastructure

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/yuu61/aironet/internal/domain"
)

// 変換結果の書き出し。章の Markdown、索引 (commands.tsv / sections.tsv)、README、図。
//
// 索引の列は ix-toolkit の ix-manual が読む形と同じにしてある。読む側は 1 章の
// ファイルを丸ごと開かず、file と line で見出し行に飛んでそこから数十行だけ読む。

// WriteChapter は章の Markdown を書く。
func WriteChapter(bookDir string, ch domain.Chapter, markdown string) error {
	return os.WriteFile(filepath.Join(bookDir, ch.MarkdownFile()), []byte(markdown), 0o644)
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
	fmt.Fprintf(b, "- 章数: %d / 見出し数: %d / コマンド項目数: %d\n", len(info.Cache.Chapters), info.NSections, info.NEntries)
	b.WriteString("- 変換経路: cisco.com の章ページ (DITA 由来の HTML) をトピックと section の構造どおりに読んだもの (表は Markdown の表、図は images/ に取得)\n")
}

func writeUsage(b *strings.Builder, info ReadmeInfo) {
	b.WriteString("\n## 引き方\n\n")
	if info.NEntries > 0 {
		b.WriteString("- `commands.tsv` — `command / entry / file / line / source` のタブ区切り索引。コマンド名から引く。\n")
		b.WriteString("  `line` は本文ファイル中の見出し行 (1 始まり)。そこから数十行読めば 1 項目に足りる。\n")
	}
	b.WriteString("- `sections.tsv` — `section / title / file / line / source` のタブ区切り索引。見出し語から引く。\n")
	b.WriteString("  `section` は「章ファイル#アンカー」。`source` は元ページの URL とアンカーで、ブラウザでそのまま開ける。\n")
	b.WriteString("- `<章>.md` — 本文。図は `images/` への相対リンク。\n")
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
		fmt.Fprintf(b, "- [%s](%s)\n", ch.Title, ch.MarkdownFile())
	}
}
