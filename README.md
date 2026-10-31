# canon

複数の情報源（要件定義書・基本設計書・詳細設計書・ソースコード・試験書・人の回答）を照合し、
プロジェクトの**検証済みの正解情報（Canon）**を組み立てる Claude Code プラグインです。

- **根拠のある値だけが Canon になる**：すべての値に出典（文書の位置・コードの位置・人の回答）が付く
- **分からないことは空白として示し、質問に変える**：AI は推測で埋めない
- **食い違いは矛盾として残す**：どちらが正しいかを AI が勝手に選ばない
- **守らないと書けない**：規則はフックと検査ツールで強制される（AI への指示に頼らない）

## 二つのフェーズ
1. **Canon をつくる**：情報源を取り込み、空白と矛盾を質問で埋め、プロジェクトの全体像を確定させる
2. **Canon を使う**：不具合対応・追加開発を、Canon を根拠に AI が行う（着手前に材料と止まるべき条件を確認し、終わったら実装とのズレを検算する）

## インストール
```
/plugin marketplace add bubu-xx/canon
/plugin install canon@bubu-xx
```
開発中はインストールせずに読み込める：
```
cd 対象プロジェクト
claude --plugin-dir <このリポジトリのパス>
```
チームで使う場合は、対象プロジェクトの `.claude/settings.json` に次を書いてコミットする：
```json
{
  "extraKnownMarketplaces": { "bubu-xx": { "source": { "source": "github", "repo": "bubu-xx/canon" } } },
  "enabledPlugins": { "canon@bubu-xx": true }
}
```

## 使い方
| 手順 | コマンド | 内容 |
|---|---|---|
| 1 | `/canon:canon-init` | 対象プロジェクトに `.canon/` を作る |
| 2 | `/canon:sources` | 設計書などを出典の位置情報付きテキストにして `.canon/sources/` に置く（原文どおりかは人が確認） |
| 3 | `/canon:ingest` | 情報源とコードから要素を抽出し、出典付きで Canon に取り込む |
| 4 | `/canon:ask` | 空白・矛盾の質問を整え、人の回答を Canon に取り込む |
| 5 | `/canon:reconcile` | 情報源の新しい版を置いたときに突き合わせ直す |
| 6 | `/canon:observe` | コードから実装を観測し、Canon とのズレを出す |
| 7 | `/canon:render` | Canon から人向け文書を生成する（Markdown、または Excel の標準様式・既存テンプレートへの流し込み） |
| フェーズ2 | `/canon:work <課題>` | Canon を根拠に修正・追加開発を行う |

## 構成
| ディレクトリ | 役割 |
|---|---|
| `skills/` | 手順（canon-init / sources / ingest / reconcile / ask / observe / render / work） |
| `agents/` | 抽出役（extractor）・書き込み役（reconciler）・質問役（questioner） |
| `hooks/` | セッション開始時に原則と現状を伝える／Canon への書き込みを書く前に検査して止める／終了時に全体を検査する |
| `.mcp.json` | MCP サーバー `canon`（query / impact / gaps / context_pack / validate）の登録 |
| `tools/` | Go 製 CLI `canon`。検査ロジックはすべてここ |
| `bin/` | OS ごとの `canon` バイナリと起動スクリプト（プラグイン有効時は PATH に入る） |
| `schema/` | 標準スキーマ（期待構造）と記述例 |
| `evals/` | 検証用サンプルプロジェクトと期待結果 |

## CLI
```
canon init        対象プロジェクトに .canon/ を作る
canon validate    Canon を規則に照らして検査する（違反があれば終了コード 1）
canon gaps        空白・矛盾・リンク不足・未回答の質問を一覧にする（--blocking で必須のみ）
canon drift       観測（実装）と Canon のズレを出す
canon render      Canon から人向け文書を生成する（--format xlsx で Excel、--template と --map で既存様式に流し込み）
canon query       要素を検索する
canon impact ID   要素を変えたときに見直しが要る要素を出す
canon context     課題に着手する前の材料一式（--text "課題文" または要素 ID）
canon next-id T   型の次の ID（bhv / req / tst / question）
canon score       取り込み結果を期待結果と比べて採点する
canon mcp         MCP サーバーとして起動する
```
多くのコマンドは `--json` で JSON を出す。

## Excel 設計書の出力
```
canon render --format xlsx                                   # 標準様式（表紙・要件一覧・処理仕様・試験仕様書・トレーサビリティ・課題管理表）
canon render --format xlsx --template 様式.xlsx --map 様式.map.yaml   # 会社の既存様式に流し込む
```
- LLM を使わず Canon から機械的に生成する。同じ Canon からは必ず同じ設計書が出る
- 未確定は黄、矛盾は橙、該当なしは灰でセルを塗り、質問 ID を示す。確定値の出典はセルのコメント（標準様式）
- 対応表の書き方は `evals/templates/処理仕様書_様式.map.yaml` を参照

## プロジェクトの種類（プロファイル）
`.canon/config.yaml` の `profile` で、スキーマの必須項目を切り替える。`business`（業務システム）／`library`（ライブラリ・デスクトップアプリ）。定義は `schema/profiles/`。

## 検査される規則（抜粋）
- confirmed の値には出典が必須。文書の出典は `.canon/sources/` のアンカー（`<!-- src: ... -->`）に実在しなければならない。コードの出典はファイルと行が実在しなければならない
- 文書・コード・人の回答は同格の情報源。食い違いは conflict として残し、どちらが正しいか（設計書が古いのか、実装の不具合か）は人が決める
- unknown に値を書けない。conflict に値を書けない（主張は claims に残す）。状態は confirmed / unknown / conflict の3つだけ
- unknown / conflict には質問が必須。スキーマの必須項目は値が無くても unknown として置く
- リンクにも出典が必須。既存の情報源は書き換えられない

## 開発
```
cd tools
go test ./...
./build.sh          # bin/ に全 OS 向けのバイナリを作る（Go のソースを直したらコミット前に必ず実行）
```
Go 1.27 以上。
