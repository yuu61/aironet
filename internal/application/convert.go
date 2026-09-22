package application

import (
	"fmt"
	"io"
	"os"

	"github.com/yuu61/aironet/internal/domain"
	"github.com/yuu61/aironet/internal/infrastructure"
)

// convertBook は取得済みの 1 冊を <manuals>/<train>/<book>/ に変換する。
//
// 章ごとに Markdown を書き、見出しとコマンドを集めて索引にする。章内目次に載る
// トピックが 1 つでも変換結果に無ければ、その冊子は失敗として止める (取りこぼした
// 索引を黙って出すより、変換器を直す方が先)。
func convertBook(w io.Writer, bc infrastructure.BookCache, train domain.Train, cacheDir, bookDir string) error {
	if err := os.MkdirAll(bookDir, 0o755); err != nil {
		return err
	}
	cacheBookDir := infrastructure.CacheDir(cacheDir, bc.Doc)

	var entries []domain.Entry
	var sections []domain.Section
	for _, ch := range bc.Chapters {
		cv, err := convertChapter(w, ch, bc.Images, cacheBookDir, bookDir)
		if err != nil {
			return err
		}
		entries = append(entries, cv.Entries...)
		sections = append(sections, cv.Sections...)
	}

	if err := infrastructure.CopyImages(cacheBookDir, bookDir, bc.Images); err != nil {
		return err
	}
	if err := infrastructure.WriteIndexes(bookDir, entries, sections); err != nil {
		return err
	}
	info := infrastructure.ReadmeInfo{Doc: bc.Doc, Train: train, Cache: bc, NEntries: len(entries), NSections: len(sections)}
	if err := infrastructure.WriteReadme(bookDir, info); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(w, "  ✓ %s (章 %d、見出し %d、コマンド %d、図 %d)\n",
		bookDir, len(bc.Chapters), len(sections), len(entries), len(bc.Images))
	return nil
}

// convertChapter は章 1 本を Markdown に書き、章内目次との突き合わせを通す。
func convertChapter(w io.Writer, ch domain.Chapter, images map[string]string, cacheBookDir, bookDir string) (infrastructure.Converted, error) {
	page, err := infrastructure.ReadChapter(cacheBookDir, ch)
	if err != nil {
		return infrastructure.Converted{}, err
	}
	cv, err := infrastructure.ConvertChapter(page, ch, images)
	if err != nil {
		return cv, fmt.Errorf("%s: %w", ch.File, err)
	}
	if err := domain.ValidateAnchors(ch.File, cv.Anchors, cv.Sections); err != nil {
		return cv, err
	}
	if err := infrastructure.WriteChapter(bookDir, ch, cv.Markdown); err != nil {
		return cv, err
	}
	for _, warn := range cv.Warnings {
		_, _ = fmt.Fprintf(w, "    ! %s: %s\n", ch.File, warn)
	}
	_, _ = fmt.Fprintf(w, "  ✓ %s (見出し %d、コマンド %d)\n", ch.MarkdownFile(), len(cv.Sections), len(cv.Entries))
	return cv, nil
}
