package infrastructure

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/yuu61/aironet/internal/domain"
)

// 資料の取得。マニフェストに書いた資料を取得キャッシュ cache/<train>/<book>/ へ取る。
//
// 冊子は目次ページから章の一覧を確定し、章ページを 1 つずつ取る (リンクを辿って
// 広げる動きは無い)。本文の図はページごとに集めて images/ に置く。取り切ったら
// book.json に章の一覧と図の対応を書き、次回はそれを見て取りに行かない。
// 利用者が明示的に叩いたときにだけ動く。定期的に取りに行く仕組みは無い。

// BookCache は取得キャッシュの中の 1 冊の状態 (book.json)。
type BookCache struct {
	Images    map[string]string `json:"images"` // 元 URL → images/ 内のファイル名
	Doc       domain.Doc        `json:"doc"`
	FetchedAt string            `json:"fetchedAt"` // YYYY-MM-DD
	Chapters  []domain.Chapter  `json:"chapters"`
}

const cacheMeta = "book.json"

// CacheDir は冊子の取得キャッシュ cache/<train>/<book>。
func CacheDir(cacheDir string, d domain.Doc) string {
	return filepath.Join(cacheDir, d.Train, d.Book)
}

// ChapterCachePath は章ページの保存先。
func ChapterCachePath(dir string, ch domain.Chapter) string {
	return filepath.Join(dir, ch.File+".html")
}

// ImageCacheDir は図の保存先。
func ImageCacheDir(dir string) string { return filepath.Join(dir, "images") }

// ReadBookCache は取り切った印 (book.json) を読む。無ければ ok=false。
func ReadBookCache(cacheDir string, d domain.Doc) (bc BookCache, ok bool, err error) {
	b, err := os.ReadFile(filepath.Join(CacheDir(cacheDir, d), cacheMeta))
	if errors.Is(err, os.ErrNotExist) {
		return bc, false, nil
	}
	if err != nil {
		return bc, false, err
	}
	if err := json.Unmarshal(b, &bc); err != nil {
		return bc, false, fmt.Errorf("%s: %w", cacheMeta, err)
	}
	if bc.Images == nil {
		bc.Images = map[string]string{}
	}
	return bc, true, nil
}

// Fetched は冊子が取り切ってあるか (book.json があり、全章の HTML が揃っている)。
func Fetched(cacheDir string, d domain.Doc) bool {
	bc, ok, err := ReadBookCache(cacheDir, d)
	if err != nil || !ok || bc.Doc.URL != d.URL {
		return false
	}
	dir := CacheDir(cacheDir, d)
	for _, ch := range bc.Chapters {
		if _, err := os.Stat(ChapterCachePath(dir, ch)); err != nil {
			return false
		}
	}
	return true
}

// FetchBook は冊子を取得キャッシュへ取る。取得済みの章は (force でなければ) 読み直さない。
func FetchBook(ctx context.Context, w io.Writer, c *Client, d domain.Doc, cacheDir string, force bool) (BookCache, error) {
	dir := CacheDir(cacheDir, d)
	if err := os.MkdirAll(ImageCacheDir(dir), 0o755); err != nil {
		return BookCache{}, err
	}

	chapters, err := chapterList(ctx, w, c, d)
	if err != nil {
		return BookCache{}, err
	}

	images := knownImages(cacheDir, d, force)
	for i, ch := range chapters {
		progress := fmt.Sprintf("[%d/%d] %s", i+1, len(chapters), ch.File)
		page, err := fetchChapter(ctx, w, c, ch, dir, progress, force)
		if err != nil {
			return BookCache{}, err
		}
		if err := images.fetch(ctx, w, c, page, ch, dir, force); err != nil {
			return BookCache{}, err
		}
	}

	bc := BookCache{Doc: d, FetchedAt: time.Now().Format("2006-01-02"), Chapters: chapters, Images: images.byURL}
	return bc, writeBookCache(dir, bc)
}

func writeBookCache(dir string, bc BookCache) error {
	b, err := json.MarshalIndent(bc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, cacheMeta), b, 0o644)
}

// imageSet は冊子の図の対応。同じ図が複数の章に出ても 1 度しか取らない。
type imageSet struct {
	byURL  map[string]string // 元 URL → ファイル名
	byName map[string]string // ファイル名 → 元 URL (衝突の検出)
}

// knownImages は前回の book.json から図の対応を引き継ぐ (force なら空から)。
func knownImages(cacheDir string, d domain.Doc, force bool) *imageSet {
	s := &imageSet{byURL: map[string]string{}, byName: map[string]string{}}
	if prev, ok, err := ReadBookCache(cacheDir, d); err == nil && ok && !force {
		s.byURL = prev.Images
	}
	for u, name := range s.byURL {
		s.byName[name] = u
	}
	return s
}

// fetchChapter は章ページを取る。取得済みなら (force でなければ) キャッシュから読む。
func fetchChapter(ctx context.Context, w io.Writer, c *Client, ch domain.Chapter, dir, progress string, force bool) ([]byte, error) {
	p := ChapterCachePath(dir, ch)
	if !force {
		if page, err := os.ReadFile(p); err == nil && len(page) > 0 {
			_, _ = fmt.Fprintf(w, "  = %s (取得済み)\n", progress)
			return page, nil
		}
	}
	page, err := c.Get(ctx, ch.URL)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", ch.URL, err)
	}
	if err := os.WriteFile(p, page, 0o644); err != nil {
		return nil, err
	}
	_, _ = fmt.Fprintf(w, "  ↓ %s\n", progress)
	return page, nil
}

// chapterList は冊子なら目次ページから章の一覧を、1 ページ資料ならそのページ 1 つを返す。
func chapterList(ctx context.Context, w io.Writer, c *Client, d domain.Doc) ([]domain.Chapter, error) {
	if d.Kind == domain.KindPage {
		return []domain.Chapter{{File: domain.ChapterFile(d.URL), Title: d.Title, URL: d.URL}}, nil
	}
	_, _ = fmt.Fprintf(w, "  → %s\n", d.URL)
	page, err := c.Get(ctx, d.URL)
	if err != nil {
		return nil, fmt.Errorf("目次: %w", err)
	}
	chapters, err := ParseBookTOC(page, d.URL)
	if err != nil {
		return nil, fmt.Errorf("目次: %w", err)
	}
	_, _ = fmt.Fprintf(w, "  目次: %d 章\n", len(chapters))
	return chapters, nil
}

// fetch は章の本文の図を images/ に取る。図が 1 枚取れなくても冊子は使えるので、
// 失敗は表示して続ける (変換時に元 URL のまま残る)。
func (s *imageSet) fetch(ctx context.Context, w io.Writer, c *Client, page []byte, ch domain.Chapter, dir string, force bool) error {
	urls, err := CollectImages(page, ch.URL)
	if err != nil {
		return fmt.Errorf("%s: %w", ch.File, err)
	}
	for _, u := range urls {
		if s.have(u, dir, force) {
			continue
		}
		body, err := c.Get(ctx, u)
		if err != nil {
			_, _ = fmt.Fprintf(w, "    ✗ 図 %s: %s\n", u, err)
			continue
		}
		if IsHTML(body) {
			_, _ = fmt.Fprintf(w, "    ✗ 図 %s: 画像ではなく HTML が返った\n", u)
			continue
		}
		name := s.name(u)
		if err := os.WriteFile(filepath.Join(ImageCacheDir(dir), name), body, 0o644); err != nil {
			return err
		}
		s.byURL[u], s.byName[name] = name, u
		_, _ = fmt.Fprintf(w, "    ↓ 図 %s\n", name)
	}
	return nil
}

// have は図を取ってあるか (対応があり、実体もある)。
func (s *imageSet) have(u, dir string, force bool) bool {
	name, ok := s.byURL[u]
	if !ok || force {
		return false
	}
	_, err := os.Stat(filepath.Join(ImageCacheDir(dir), name))
	return err == nil
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// name は図のファイル名。URL の basename を使い、別の URL と衝突したら URL のハッシュを前に付ける。
func (s *imageSet) name(rawURL string) string {
	base := "image"
	if u, err := url.Parse(rawURL); err == nil {
		base = path.Base(u.Path)
	}
	base = unsafeName.ReplaceAllString(base, "_")
	if prev, ok := s.byName[base]; !ok || prev == rawURL {
		return base
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(rawURL))
	return fmt.Sprintf("%08x-%s", h.Sum32(), base)
}

// ReadChapter は取得キャッシュから章ページを読む。
func ReadChapter(dir string, ch domain.Chapter) ([]byte, error) {
	b, err := os.ReadFile(ChapterCachePath(dir, ch))
	if err != nil {
		return nil, fmt.Errorf("章 %s を取得していない (manualbook fetch を先に流す): %w", ch.File, err)
	}
	if !strings.Contains(string(b), "<") {
		return nil, fmt.Errorf("章 %s のキャッシュが HTML ではない", ch.File)
	}
	return b, nil
}
