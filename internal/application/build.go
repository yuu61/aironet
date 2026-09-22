package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/yuu61/aironet/internal/domain"
	"github.com/yuu61/aironet/internal/infrastructure"
)

// Options は build / fetch / md に共通の指定。
type Options struct {
	ManifestPath string
	CacheDir     string
	ManualsDir   string
	Only         string        // "<train>/<book>"。空なら全部
	Force        bool          // 取得済みでも取り直す
	Delay        time.Duration // リクエストの間隔
}

// DefaultManualsDir は変換結果の置き場 ($AIRONET_MANUALS → ~/.aironet/manuals)。
func DefaultManualsDir() string {
	home, _ := os.UserHomeDir()
	return domain.DefaultManualsDir(os.Getenv, home)
}

// Build はマニフェストの資料を取得し、変換し、索引まで作る。
//
// 資料ごとに独立して進め、1 冊が取れなくても残りは作る。取得済みの資料は取りに
// 行かず変換だけになるので、2 回目以降は数秒で終わる。取り直したいときだけ -force。
// 取得は 1 秒おきに数十〜数百リクエストを送るので、初回は冊子によって数分掛かる
// (礼儀の方なので縮めない)。
func Build(w io.Writer, o Options) error {
	return run(w, o, true, true)
}

// Fetch は取得だけ行う。
func Fetch(w io.Writer, o Options) error { return run(w, o, true, false) }

// Convert は取得キャッシュを変換するだけ。
func Convert(w io.Writer, o Options) error { return run(w, o, false, true) }

func run(w io.Writer, o Options, fetch, convert bool) error {
	m, err := infrastructure.ReadManifest(o.ManifestPath)
	if err != nil {
		return err
	}
	docs := m.Select(o.Only)
	if len(docs) == 0 {
		return fmt.Errorf("マニフェストに %q の資料がありません (<train>/<book> で指定する)", o.Only)
	}
	if convert && o.ManualsDir == "" {
		return errors.New("変換結果の置き場が決まりません。-manuals で指定してください")
	}
	if err := os.MkdirAll(o.CacheDir, 0o755); err != nil {
		return err
	}

	ctx := context.Background()
	client := infrastructure.NewClient(o.Delay)
	failed := 0
	for i, d := range docs {
		_, _ = fmt.Fprintf(w, "\n[%d/%d] %s — %s\n", i+1, len(docs), d.Key(), d.Title)
		if err := buildOne(ctx, w, client, m, d, o, fetch, convert); err != nil {
			failed++
			_, _ = fmt.Fprintf(w, "  ✗ %s\n", err)
		}
	}
	_, _ = fmt.Fprintf(w, "\n完了: %d 冊中 %d 冊", len(docs), len(docs)-failed)
	if convert {
		_, _ = fmt.Fprintf(w, " → %s", o.ManualsDir)
	}
	_, _ = fmt.Fprintln(w)
	if failed > 0 {
		return ReportedError(1)
	}
	return nil
}

func buildOne(ctx context.Context, w io.Writer, client *infrastructure.Client, m domain.Manifest, d domain.Doc,
	o Options, fetch, convert bool,
) error {
	var bc infrastructure.BookCache
	switch {
	case fetch && (o.Force || !infrastructure.Fetched(o.CacheDir, d)):
		var err error
		if bc, err = infrastructure.FetchBook(ctx, w, client, d, o.CacheDir, o.Force); err != nil {
			return err
		}
	default:
		var ok bool
		var err error
		if bc, ok, err = infrastructure.ReadBookCache(o.CacheDir, d); err != nil {
			return err
		}
		if !ok {
			return errors.New("取得していない (manualbook fetch を先に流す)")
		}
		if fetch {
			_, _ = fmt.Fprintf(w, "  = 取得済み (%d 章、%s)\n", len(bc.Chapters), bc.FetchedAt)
		}
	}
	if !convert {
		return nil
	}
	train, _ := m.Train(d.Train)
	return convertBook(w, bc, train, o.CacheDir, domain.BookDir(o.ManualsDir, d))
}
