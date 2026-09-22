# AGENTS.md

このリポジトリで作業するときの前提。使い方は README.md を見る。

## 構成

| ディレクトリ | 中身 | 言語 |
|---|---|---|
| `cmd/manualbook/` | manualbook の起動点 | Go |
| `internal/domain/` | 資料・トレイン・章・索引の値と規則 (manifest の検証、索引のキー、出力先、章内目次との突き合わせ、章のトピック分割とリンクの付け替え) | Go |
| `internal/application/` | manifest から取得 → 変換 → 索引まで進める実行手順 | Go |
| `internal/infrastructure/` | HTTP、HTML の解析 (目次、DITA → Markdown)、JSON / Markdown / TSV / 画像の入出力 | Go (x/net/html) |
| `internal/cli/` | サブコマンド、引数解析、エラーの最終表示と終了コード | Go |
| `manifest.json` | 取得する資料の一覧 (train・book・kind・title・url) と train の説明。`build` の唯一の入力 | JSON |
| `src/air_ssh/domain/` | 機器・認証情報・コマンド・WLAN サイクルの値と規則 | Python |
| `src/air_ssh/application/` | 機器の選択 → コマンド実行 → WLAN 復旧 → 保存の手順 | Python |
| `src/air_ssh/infrastructure/` | devices.json の読み込みと netmiko の SSH セッション | Python |
| `src/air_ssh/cli/` | 引数解析、エラーの最終表示と終了コード。air-ssh の入口 | Python |
| `src/air-ssh/wlc-ssh.py` | 旧パス互換の起動点 | Python |
| `tests/` | air-ssh の規則・認証情報解決・疑似セッションによる操作の検証 | Python |
| `skills/` | air-ssh 操作用・マニュアル参照用の skill | Markdown |

```console
$ go build -ldflags="-s -w" -o manualbook.exe ./cmd/manualbook   # -s -w は Defender の誤検知回避
$ ./manualbook.exe build                                          # 変換結果を作り直して確かめる
$ go test ./...                                                   # 規則 (manifest・索引・目次・変換) の検証
$ go vet ./...
$ golangci-lint run ./...                                         # ix-toolkit と同じ設定 (.golangci.yml)。0 issues を保つ
$ uv sync --extra dev
$ uv run python -m unittest
$ uv run ruff check src/ tests/
$ uv run ruff format --check src/ tests/
```

## 層の規則

`domain` は他の層、HTTP、HTML の DOM、ファイル入出力に依存させない。
`infrastructure` は `domain` を使い、`application` / `cli` には依存させない。
`application` は `domain` と `infrastructure` を組み合わせる。`cli` は `application` だけを呼ぶ
(`infrastructure` を直接呼ばない)。プロセス終了は `cli` に置く。原因を表示し終えた失敗は
`application.ReportedError` で終了コードだけ伝える。

Python の `src/air_ssh/` も同じ依存方向にする。`domain` はファイル・環境変数・SSH を直接読まず、
渡された値だけを扱う。`cli` は `application` の公開 API だけを呼ぶ。
接続情報は `--inventory` → `$AIRONET_INVENTORY` → `~/.aironet/devices.json` から読み、
機器は `--device` / `$AIRONET_DEVICE` で明示する。

## cisco.com の取り方

- UA は `manualbook/0.1 (+…)` のまま送り、**ブラウザを装わない**。cisco.com の前段 (Akamai) は
  UA と TLS の指紋を突き合わせていて、Go から Chrome の UA を送ると常に 403 になる。
  正直な UA に `Accept` と `Accept-Language` を添えると通る (3 つ揃って初めて 200)。
- 章の一覧は目次ページの `ul#bookToc` から確定し、リンクを辿って広げない。
- 1 秒おきに取る (`-delay`)。取得済みは取りに行かず、`-force` のときだけ取り直す。
- 定期的に取りに行く仕組みは作らない。Cisco の更新を見て人が `manifest.json` を直し、取り直す。

## 変換結果の契約

`<manuals>/<train>/<book>/` (`$AIRONET_MANUALS` → `~/.aironet/manuals/`) に

- `<章>/<トピック>.md` — 本文。章 (元の 1 ページ) をディレクトリにし、章直下のトピック (コマンド 1 つ、
  設定ガイドの 1 機能) を 1 ファイルにする。`domain.MaxPartBytes` (32 KB) を超えるトピックは子トピックを
  さらに別ファイルにし、元のファイルには子への一覧リンクを残す (再帰)。ファイル名は見出しから
  `domain.TopicSlug` で作り (`config aaa auth` → `config_aaa_auth.md`)、同じ章で重複したら `_2`, `_3` …。
  ファイルの先頭見出しは `#` になるようレベルを引き下げる。図は `../images/` への相対リンク
- `<章>/README.md` — 章タイトル、章直下の本文、トピックの一覧 (グループ見出しは小見出し)
- `commands.tsv` — `command / entry / file / line / source`。コマンドの見出し。コマンドとみなすのは
  reference トピックのうち構文 `section.refsyn` が「構文ブロック `p.synblk` を持つ」か「インラインだけで
  組まれている」もの (`hasCommandSyntax`)。設定ガイドの「Restrictions for …」も refsyn を使うが箇条書き
  なので除く。この条件を緩めると設定ガイドの制約項目がコマンドとして混ざる
- `sections.tsv` — `section / title / file / line / source`。全 topictitle 見出し。`section` は「章#アンカー」で、
  ファイルの分け方に依らない
- `README.md` — 出典、取得日、トレインと最終機種、章の一覧

`file` はトピックのファイル、`line` はその中の見出し行 (1 始まり)。1 ファイルを丸ごと読んで足りる大きさ
なので、読む側は grep や部分読みに頼らなくてよい。この形は ix-toolkit の `ix-manual` skill と同じ列で、
skill 側はこの契約を前提に読む。片方を変えたらもう片方も直す。

分割は `domain.Split` が変換後の Markdown に対して行う。変換器 (`infrastructure.ConvertChapter`) は章 1 本を
1 つの Markdown に書き、見出しごとにトピックの入れ子の深さ (`Section.Depth`、cisco.com の `nestedN` クラス)
と行番号を記録する。分割はその記録だけで範囲を決め、本文を解釈しない。冊子の中へのリンクは分割先が
決まるまで `domain.LinkRef` の目印にし、全章を分けてから `domain.ResolveLinks` で相対パスにする。
書き出しは冊子の置き場 `<manuals>/<train>/<book>/` を丸ごと消してから行う (`ResetBookDir`。前回の構成が
残らないように)。README.md の無い非空ディレクトリだけは消さずに止めるが、それ以上の保護は無いので、
`-manuals` / `$AIRONET_MANUALS` に他の物が入ったディレクトリを指さない。

章内目次 (`div#chapterToc`、1 ページ資料は Contents) の全アンカーが変換結果の見出しに現れることを
build 中に強制している。落ちたら変換器がトピックを読み飛ばしているので、検証を緩めずに変換器を直す。
分割も、記録された見出し行が本文の見出しを指していなければ止める (`Split` のエラー)。

## リポジトリに入れないもの

- 機器の資格情報とインベントリ (`~/.aironet/devices.json`)。
- マニュアル本文・変換結果・図・取得キャッシュ (`cache/`)。Cisco の著作物で、各自の手元で取得・変換する。
- 変換器のテストに本物の章 HTML を置かない。構造を写した断片 (`dita_test.go`) で足りる。
