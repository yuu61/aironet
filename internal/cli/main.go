// Package cli はサブコマンドと引数の解釈、エラーの最終表示と終了コードを扱う。
package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/yuu61/aironet/internal/application"
)

const usage = `manualbook — cisco.com の WLC / Mobility Express のマニュアルを Markdown にする

使い方:
  manualbook <サブコマンド> [オプション]

サブコマンド:
  build   manifest.json の資料を取得 → 変換 → 索引まで 1 回で作る (ふつうはこれだけ)
  fetch   マニフェストに書いた資料をまとめて取得キャッシュ (cache/) に取る
  md      取得キャッシュを Markdown と索引に変換する

典型的な流れ:
  manualbook build                 # cache/ に取り、~/.aironet/manuals/<train>/<book>/ に変換する
  manualbook build -only 8-5/cr    # 1 冊だけ

取得済みの資料は取りに行かない (変換だけになる)。取り直すときは -force。
`

// Main はエントリポイント。
func Main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(1)
	}
	switch cmd := os.Args[1]; cmd {
	case "build":
		runWith("build", os.Args[2:], application.Build)
	case "fetch":
		runWith("fetch", os.Args[2:], application.Fetch)
	case "md":
		runWith("md", os.Args[2:], application.Convert)
	case "-h", "--help", "help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "不明なサブコマンド: %s\n\n", cmd)
		fmt.Fprint(os.Stderr, usage)
		os.Exit(1)
	}
}

func runWith(name string, args []string, fn func(w io.Writer, o application.Options) error) {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	var o application.Options
	fs.StringVar(&o.ManifestPath, "manifest", "manifest.json", "マニフェスト JSON")
	fs.StringVar(&o.CacheDir, "cache", "cache", "取得キャッシュの置き場")
	fs.StringVar(&o.ManualsDir, "manuals", application.DefaultManualsDir(), "変換結果の置き場。この下に <train>/<book>/ を作る ($AIRONET_MANUALS があればそれ)")
	fs.StringVar(&o.Only, "only", "", "この資料だけ扱う (<train>/<book>、例: 8-10/cr)")
	fs.BoolVar(&o.Force, "force", false, "取得済みの資料も取り直す")
	fs.DurationVar(&o.Delay, "delay", time.Second, "cisco.com へのリクエストの間隔")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "使い方: manualbook %s [-only <train>/<book>] [-force]\n", name)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "余分な引数: %v\n", fs.Args())
		os.Exit(2)
	}
	if err := fn(os.Stdout, o); err != nil {
		fatal(err)
	}
}

// fatal はエラーを表示して終了する。原因を表示し終えている失敗 (application.ReportedError)
// は黙って終了コードだけ返す。
func fatal(err error) {
	if e, ok := err.(interface{ ExitCode() int }); ok {
		os.Exit(e.ExitCode())
	}
	fmt.Fprintf(os.Stderr, "エラー: %s\n", err)
	os.Exit(1)
}
