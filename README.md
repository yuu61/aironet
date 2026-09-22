# aironet

Cisco AireOS WLC / Mobility Express を扱うための道具立て。

## manualbook — マニュアル変換ツール

cisco.com の WLC / Mobility Express のマニュアル (HTML) を取得し、章ごとの Markdown と索引
(`commands.tsv` / `sections.tsv`) に変換する Go 製のツール。

```console
$ go build -ldflags="-s -w" -o manualbook.exe ./cmd/manualbook
$ ./manualbook.exe build
```

- `manifest.json` の資料を `cache/` に取り、`~/.aironet/manuals/<train>/<book>/` に変換する
  (`$AIRONET_MANUALS` があればそこ)。
- 2 回目以降は取得済みの資料を取りに行かず、変換だけになる。取り直すときは `-force`。
- 1 冊だけなら `-only 8-10/cr` のように指定する。

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
「Controller CLI Commands」章 (`8-5/me-ug/ctrlr_cli.md`) に手順として書かれている。
コマンド項目ではないので `commands.tsv` には載らず、`sections.tsv` の `ctrlr_cli#…` から引く。

### 変換結果の構成

```
~/.aironet/manuals/8-10/cr/
  README.md                  出典・取得日・トレイン・最終機種・章の一覧
  config_commands_a_to_i.md  章ごとの本文
  …
  images/                    本文の図
  commands.tsv               command / entry / file / line / source
  sections.tsv               section / title / file / line / source
```

`file` と `line` で見出し行を直接指すので、章のファイルを丸ごと開かずにそこから数十行だけ読めばよい。

### 個別実行

```
manualbook fetch   資料を取得キャッシュ (cache/) に取るだけ
manualbook md      取得キャッシュを Markdown と索引に変換するだけ
```

## ライセンス

MIT。変換結果 (マニュアル本文・図) の著作権は Cisco Systems, Inc. に帰属し、再配布しない。
