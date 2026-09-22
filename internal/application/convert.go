package application

import (
	"fmt"
	"io"

	"github.com/yuu61/aironet/internal/domain"
	"github.com/yuu61/aironet/internal/infrastructure"
)

// convertBook は取得済みの 1 冊を <manuals>/<train>/<book>/ に変換する。
//
// 章ごとに Markdown にして章内目次と突き合わせ、トピックごとのファイルに分け、見出しと
// コマンドを集めて索引にする。章内目次に載るトピックが 1 つでも変換結果に無ければ、その
// 冊子は失敗として止める (取りこぼした索引を黙って出すより、変換器を直す方が先)。
// 冊子の中へのリンクは全章を分けてからファイルに付け替える。書き出しは全章が通ってから
// まとめて行い、置き場は前回の結果を消してから書く。
func convertBook(w io.Writer, bc infrastructure.BookCache, train domain.Train, cacheDir, bookDir string) error {
	cacheBookDir := infrastructure.CacheDir(cacheDir, bc.Doc)

	var parts []domain.Part
	var entries []domain.Entry
	var sections []domain.Section
	for _, ch := range bc.Chapters {
		sp, err := convertChapter(w, ch, bc.Images, cacheBookDir)
		if err != nil {
			return err
		}
		parts = append(parts, sp.Parts...)
		entries = append(entries, sp.Entries...)
		sections = append(sections, sp.Sections...)
	}
	parts = domain.ResolveLinks(parts, sections)

	if err := infrastructure.ResetBookDir(bookDir); err != nil {
		return err
	}
	if err := infrastructure.WriteParts(bookDir, parts); err != nil {
		return err
	}
	if err := infrastructure.CopyImages(cacheBookDir, bookDir, bc.Images); err != nil {
		return err
	}
	if err := infrastructure.WriteIndexes(bookDir, entries, sections); err != nil {
		return err
	}
	info := infrastructure.ReadmeInfo{
		Doc: bc.Doc, Train: train, Cache: bc, NParts: len(parts), NEntries: len(entries), NSections: len(sections),
	}
	if err := infrastructure.WriteReadme(bookDir, info); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(w, "  ✓ %s (章 %d、ファイル %d、見出し %d、コマンド %d、図 %d)\n",
		bookDir, len(bc.Chapters), len(parts), len(sections), len(entries), len(bc.Images))
	return nil
}

// convertChapter は章 1 本を Markdown にし、トピックごとに分ける。
func convertChapter(w io.Writer, ch domain.Chapter, images map[string]string, cacheBookDir string) (domain.SplitChapter, error) {
	page, err := infrastructure.ReadChapter(cacheBookDir, ch)
	if err != nil {
		return domain.SplitChapter{}, err
	}
	cv, err := infrastructure.ConvertChapter(page, ch, images)
	if err != nil {
		return domain.SplitChapter{}, fmt.Errorf("%s: %w", ch.File, err)
	}
	sp, err := splitChapter(ch, cv)
	if err != nil {
		return sp, err
	}
	for _, warn := range cv.Warnings {
		_, _ = fmt.Fprintf(w, "    ! %s: %s\n", ch.File, warn)
	}
	_, _ = fmt.Fprintf(w, "  ✓ %s/ (ファイル %d、見出し %d、コマンド %d)\n", ch.File, len(sp.Parts), len(sp.Sections), len(sp.Entries))
	return sp, nil
}

// splitChapter は章内目次との突き合わせを通してから、トピックごとに分ける。
func splitChapter(ch domain.Chapter, cv infrastructure.Converted) (domain.SplitChapter, error) {
	if err := domain.ValidateAnchors(ch.File, cv.Anchors, cv.Sections); err != nil {
		return domain.SplitChapter{}, err
	}
	return domain.Split(ch, cv.Text())
}
