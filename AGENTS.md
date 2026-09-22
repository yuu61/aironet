# AGENTS.md

このリポジトリで作業するときの前提。使い方は README.md を見る。

## 構成

| ディレクトリ | 中身 | 言語 |
|---|---|---|
| `cmd/manualbook/` | manualbook の起動点 | Go |
| `internal/domain/` | 資料・トレイン・章・索引の値と規則 (manifest の検証、索引のキー、出力先、章内目次との突き合わせ) | Go |
| `internal/application/` | manifest から取得 → 変換 → 索引まで進める実行手順 | Go |
| `internal/infrastructure/` | HTTP、HTML の解析 (目次、DITA → Markdown)、JSON / Markdown / TSV / 画像の入出力 | Go (x/net/html) |
| `internal/cli/` | サブコマンド、引数解析、エラーの最終表示と終了コード | Go |
| `manifest.json` | 取得する資料の一覧 (train・book・kind・title・url) と train の説明。`build` の唯一の入力 | JSON |
| `src/air-ssh/` | WLC への SSH 操作 (netmiko) | Python |
| `skills/` | SKILL.md 形式の skill (これから) | Markdown |

```console
$ go build -ldflags="-s -w" -o manualbook.exe ./cmd/manualbook   # -s -w は Defender の誤検知回避
$ ./manualbook.exe build                                          # 変換結果を作り直して確かめる
$ go test ./...                                                   # 規則 (manifest・索引・目次・変換) の検証
$ go vet ./...
$ golangci-lint run ./...                                         # ix-toolkit と同じ設定 (.golangci.yml)。0 issues を保つ
```

## 層の規則

`domain` は他の層、HTTP、HTML の DOM、ファイル入出力に依存させない。
`infrastructure` は `domain` を使い、`application` / `cli` には依存させない。
`application` は `domain` と `infrastructure` を組み合わせる。`cli` は `application` だけを呼ぶ
(`infrastructure` を直接呼ばない)。プロセス終了は `cli` に置く。原因を表示し終えた失敗は
`application.ReportedError` で終了コードだけ伝える。

## cisco.com の取り方

- UA は `manualbook/0.1 (+…)` のまま送り、**ブラウザを装わない**。cisco.com の前段 (Akamai) は
  UA と TLS の指紋を突き合わせていて、Go から Chrome の UA を送ると常に 403 になる。
  正直な UA に `Accept` と `Accept-Language` を添えると通る (3 つ揃って初めて 200)。
- 章の一覧は目次ページの `ul#bookToc` から確定し、リンクを辿って広げない。
- 1 秒おきに取る (`-delay`)。取得済みは取りに行かず、`-force` のときだけ取り直す。
- 定期的に取りに行く仕組みは作らない。Cisco の更新を見て人が `manifest.json` を直し、取り直す。

## 変換結果の契約

`<manuals>/<train>/<book>/` (`$AIRONET_MANUALS` → `~/.aironet/manuals/`) に

- `<章>.md` — 章ごとの本文。図は `images/` への相対リンク
- `commands.tsv` — `command / entry / file / line / source`。コマンドの見出し。コマンドとみなすのは
  reference トピックのうち構文 `section.refsyn` が「構文ブロック `p.synblk` を持つ」か「インラインだけで
  組まれている」もの (`hasCommandSyntax`)。設定ガイドの「Restrictions for …」も refsyn を使うが箇条書き
  なので除く。この条件を緩めると設定ガイドの制約項目がコマンドとして混ざる
- `sections.tsv` — `section / title / file / line / source`。全 topictitle 見出し。`section` は「章ファイル#アンカー」
- `README.md` — 出典、取得日、トレインと最終機種、章の一覧

`line` は本文の見出し行 (1 始まり)。この形は ix-toolkit の `ix-manual` skill と同じで、
skill 側はこの契約を前提に読む。片方を変えたらもう片方も直す。

章内目次 (`div#chapterToc`、1 ページ資料は Contents) の全アンカーが変換結果の見出しに現れることを
build 中に強制している。落ちたら変換器がトピックを読み飛ばしているので、検証を緩めずに変換器を直す。

## リポジトリに入れないもの

- 機器の資格情報。
- マニュアル本文・変換結果・図・取得キャッシュ (`cache/`)。Cisco の著作物で、各自の手元で取得・変換する。
- 変換器のテストに本物の章 HTML を置かない。構造を写した断片 (`dita_test.go`) で足りる。
