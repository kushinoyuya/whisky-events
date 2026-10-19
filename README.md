# whisky-events

Webページから日本国内のウイスキーイベント（試飲会・フェスティバル・セミナーなど）を抽出し、構造化データにするCLIです。

- **安くて確実な方法から順に試す**: ① 埋め込みの JSON-LD（schema.org `Event`）→ ② ラベル（日時・会場・料金）と日本語の日付をルールで読む → ③ 任意でローカルLLM（[Ollama](https://ollama.com)、`-llm`）
- **完全ローカル**: 外部APIやAPIキーは不要。LLMがなくても動きます。
- **どの方法でも同じ検証**: タイトルと開始日の必須チェック、日付形式、終了日の補正、重複除去、終了済みイベントの除外。各イベントに抽出方法（`method`）が付きます。

## 構成

```
sites.json → crawl (robots.txt / ホストごとの間隔 / 一覧→詳細リンク / 内容ハッシュでキャッシュ)
           → fetch → Page(HTML/本文) → JSONLD → Rules → (LLM) → clean + upcoming → stdout
```

| パッケージ | 役割 |
|---|---|
| `internal/llm` | Ollama `/api/generate` クライアント（`Generator` インターフェース） |
| `internal/htmltext` | script/style/nav などを除いて本文テキスト化 |
| `internal/extract` | JSON-LD・ルール・LLMの各抽出器、日付解析、検証、パイプライン |
| `internal/fetch` | User-Agent、タイムアウト、サイズ上限付きの取得 |
| `internal/crawl` | `sites.json` の巡回、robots.txt、アクセス間隔、キャッシュ |

## 使い方

### 登録サイトをまとめて巡回する

```sh
make build
./bin/whisky-events crawl > events.json          # sites.json の enabled なサイトすべて
./bin/whisky-events crawl -site 福岡               # 名前に「福岡」を含むサイトだけ（enabled:false でも実行）
```

- 同じホストへのリクエストは既定で3秒以上あけます（`-delay`）。
- robots.txt で禁止されたURLは取得しません。
- 結果は `data/cache.json` に保存され、次回は内容が変わっていないページの抽出を省きます。
- 進捗とまとめ（取得・抽出・キャッシュ・除外・エラーの件数）は標準エラーに出ます。

`sites.json` の各サイトに `follow`（URLの正規表現）を書くと、そのページを一覧ページとして扱い、一致するリンク先（同じホストのみ、`max_links` 件まで）も巡回します。

### 1ページだけ抽出する

```sh
make sample             # testdata/sample_event.html から抽出（ルール）
go run ./cmd/whisky-events extract -file testdata/jsonld_event.html   # JSON-LD
go run ./cmd/whisky-events extract -url https://example.jp/event
```

LLMを最後の手段として使う場合（任意）:

```sh
ollama serve            # 別ターミナルで起動
make ping
go run ./cmd/whisky-events extract -url https://example.jp/event -llm
```

`-ollama` / `-model` フラグ、または `OLLAMA_HOST_URL` / `WHISKY_MODEL` 環境変数で接続先を変えられます。

## テスト

```sh
make test
```

LLMはフェイクに差し替えてテストするので、Ollamaがなくても通ります。

## ロードマップ

- [x] Stage 1: 1ページからイベントを抽出する（JSON-LD / ルール / 任意のLLM）
- [x] Stage 2: `sites.json` のサイトを巡回、robots.txt、キーワードで事前フィルタ、内容ハッシュでキャッシュ、英語ページ対応
- [ ] Stage 3: JSONLに保存、重複の統合、終了済みイベントの除外
- [ ] Stage 4: 自然文検索（`search "11月 東京 試飲会"`）
- [ ] Stage 5: 正解付きページの評価セットで抽出精度を計測

## 制約

- Go 1.13、標準ライブラリのみ。
- Shift_JIS / EUC-JP のページはまだ扱えません（取得時にエラーになります）。
- ルール抽出は「1ページ1イベント」を想定しています。一覧ページは `follow` で詳細ページをたどるか、JSON-LD / LLM が必要です。
- JavaScript で描画されるページは読めません（HTMLに本文がないため）。
- robots.txt はパスの前方一致のみ対応（`*` や `$` のワイルドカードは未対応）。
