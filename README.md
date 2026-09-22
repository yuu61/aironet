# aironet

Cisco AireOS WLC / Mobility Express を扱うための道具立て。

## air-ssh — WLC の SSH 操作

Python 3.10 以降と uv を使う。

```console
$ uv tool install -e .
$ air-ssh --list
$ air-ssh --device wlc "show sysinfo"
$ air-ssh --device wlc "show ap summary" "show client summary"
$ air-ssh --device wlc --cycle-wlan 1 "config wlan max-associated-clients 50 1" --save
$ air-ssh --device wlc --save
```

接続情報は `~/.aironet/devices.json` に置く。リポジトリには入れない。
以下の値を実際の機器に合わせて設定する。

```json
{
  "devices": {
    "wlc": {
      "host": "192.0.2.1",
      "username": "admin",
      "password": "YOUR_PASSWORD",
      "port": 22
    }
  }
}
```

- ファイルの選択順は `--inventory PATH` → `$AIRONET_INVENTORY` → `~/.aironet/devices.json`。
  `--list` は参照先と機器名・ホスト・ユーザー名を表示し、パスワードは表示しない。
- 機器は `--device NAME` (短縮形 `-d`) または `$AIRONET_DEVICE` で明示する。
  ソースコードに固定の接続先や認証情報は持たない。
- ix-toolkit と同様、`devices` で包まない `{ "wlc": { ... } }` 形式も使える。
  `_` で始まる項目はコメントとして無視する。ホストの別名キーは `hostname` / `address` / `ip`、
  ユーザー名は `user` も使える。
- パスワードは機器の `password` → `password_env` が指す環境変数 → `$WLC_PASS` の順で読む。
  例えば `"password_env": "LAB_WLC_PASS"`。パスワードが無ければ接続前にエラーになる。
  インベントリは本人だけが読み書きできる権限にする (POSIX では `chmod 600`)。
- 確認プロンプトには `y`、ページ送りには Enter を自動で返し、出力を逐次表示する。
  120 秒無出力なら次のコマンドへ進むが、終了コードは 1 になり `--save` は実行しない。
- `--cycle-wlan ID` はその位置で WLAN を無効化し、次のサイクルの直前または最後に再有効化する。
  例外時も、無効化を試みた WLAN の再有効化を試みる。無効化・再有効化がタイムアウトした場合は
  次のサイクルへ進まない。`--save` はすべての再有効化が完了してから行う。

開発時は `uv sync --extra dev` で依存関係を入れ、`uv run air-ssh ...` または
`uv run python -m air_ssh ...` で実行する。以前の `src/air-ssh/wlc-ssh.py` も入口として使えるが、
接続先は `--device` または `$AIRONET_DEVICE` で指定する。

```console
$ uv run python -m unittest
$ uv run ruff check src/ tests/
$ uv run ruff format --check src/ tests/
```

## manualbook — マニュアル変換ツール

cisco.com の WLC / Mobility Express のマニュアル (HTML) を取得し、章ごとのディレクトリに
トピック単位の Markdown と索引 (`commands.tsv` / `sections.tsv`) を作る Go 製のツール。

```console
$ go build -ldflags="-s -w" -o manualbook.exe ./cmd/manualbook
$ ./manualbook.exe build
```

- `manifest.json` の資料を `cache/` に取り、`~/.aironet/manuals/<train>/<book>/` に変換する
  (`$AIRONET_MANUALS` があればそこ)。
- 2 回目以降は取得済みの資料を取りに行かず、変換だけになる。取り直すときは `-force`。
- 1 冊だけなら `-only 8-10/cr` のように指定する。
- 変換のたびに冊子の置き場 (`<manuals>/<train>/<book>/`) を消して書き直す。

### 対象

| train | book | 冊子 |
|---|---|---|
| `8-5` | `cr` | Cisco Wireless Controller Command Reference, Release 8.5 |
| `8-5` | `cg` | Cisco Wireless Controller Configuration Guide, Release 8.5 |
| `8-5` | `me-ug` | Cisco Mobility Express User Guide, Release 8.5 |
| `8-5` | `me-dg` | Cisco Mobility Express Deployment Guide, Release 8.5 |
| `8-5` | `me-rn` | Release Notes for Cisco Mobility Express, Release 8.5 |
| `8-10` | `cr` | Cisco Wireless Controller Command Reference, Release 8.10 |
| `8-10` | `cg` | Cisco Wireless Controller Configuration Guide, Release 8.10 |
| `8-10` | `ap-cr` | Cisco Aironet Wave 2 and Catalyst Wi-Fi6 Access Point Command Reference, Release 8.10 |
| `8-10` | `me-ug` | Cisco Mobility Express User Guide, Release 8.10 |
| `8-10` | `me-cr` | Cisco Mobility Express Command Reference, Release 8.10 |
| `8-10` | `me-rn` | Release Notes for Cisco Mobility Express, Release 8.10 |

`8-5` は Cisco 2504 WLC (と 5508 / 7510 / WiSM2 / 8510) の最終トレイン、`8-10` は
3504 / 5520 / 8540 / vWLC / Mobility Express の最終トレイン (AireOS の最終)。
Mobility Express 8.5 には独立したコマンドリファレンスが無く、CLI は User Guide の
「Controller CLI Commands」章 (`8-5/me-ug/ctrlr_cli/`) に手順として書かれている。
コマンド項目ではないので `commands.tsv` には載らず、`sections.tsv` の `ctrlr_cli#…` から引く。

### 変換結果の構成

```
~/.aironet/manuals/8-10/cr/
  README.md                          出典・取得日・トレイン・最終機種・章の一覧
  config_commands_a_to_i/            章 (元の 1 ページ) ごとのディレクトリ
    README.md                        章タイトル・章直下の本文・トピックの一覧
    config_aaa_auth.md               トピック (コマンド 1 つ・1 機能) ごとの本文
    config_aaa_auth_mgmt.md
    …
  …
  images/                            本文の図 (本文からは ../images/ で参照)
  commands.tsv                       command / entry / file / line / source
  sections.tsv                       section / title / file / line / source
```

章 1 本は数百 KB になり grep や部分読みでは取りこぼすので、章直下のトピックを 1 ファイルにする。
32 KB を超えるトピックは子トピックをさらに別ファイルにし、元のファイルに一覧リンクを残す。
`commands.tsv` / `sections.tsv` の `file` はそのトピックのファイル、`line` はその中の見出し行。
ファイル名は見出しから作る (`config aaa auth` → `config_aaa_auth.md`) ので、`ls` でも見つかる。

### 個別実行

```
manualbook fetch   資料を取得キャッシュ (cache/) に取るだけ
manualbook md      取得キャッシュを Markdown と索引に変換するだけ
```

## Skills

| Skill | 用途 |
|---|---|
| [air-ssh](skills/air-ssh/SKILL.md) | 実機の状態確認、設定変更、WLAN サイクル、設定保存 |
| [air-manual](skills/air-manual/SKILL.md) | ローカルの変換済み資料から構文・手順・制約を調べる（機器には接続しない） |

Codex で使う場合は、必要な skill のディレクトリを `$CODEX_HOME/skills/`
（未設定なら `~/.codex/skills/`）へ配置する。`air-ssh` は CLI とインベントリ、
`air-manual` は `manualbook` で生成した資料を利用する。

例: `$air-ssh で wlc の AP 一覧を確認して`、
`$air-manual で Mobility Express 8.5 の WLAN 設定手順を調べて`。

## ライセンス

MIT。変換結果 (マニュアル本文・図) の著作権は Cisco Systems, Inc. に帰属し、再配布しない。
