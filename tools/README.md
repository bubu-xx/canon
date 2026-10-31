# tools

Go 製 CLI `canon`。フック・スキル・MCP サーバーから共通で呼ばれる検査ロジックはすべてここに置く。

| パッケージ | 内容 |
|---|---|
| `internal/model` | 要素・フィールド・出典・リンク・質問の読み込み |
| `internal/schema` | 標準スキーマとプロジェクト拡張（schema-ext）の読み込み |
| `internal/project` | `.canon/` の発見と読み込み、情報源アンカーの収集、観測の読み込み |
| `internal/check` | validate（規則の検査） |
| `internal/gaps` | 空白・矛盾・リンク不足の一覧 |
| `internal/graph` | リンクのたどり（関連・影響範囲） |
| `internal/drift` | 観測と Canon のズレ |
| `internal/render` | 人向け文書の生成（テンプレートのみ） |
| `internal/mcpserver` | MCP サーバー（stdio、JSON-RPC） |
| `internal/hook` | Claude Code のフック（session-start / pre-write / stop） |
| `internal/score` | 取り込み結果の採点 |

## ビルド
```
go test ./...
./build.sh      # bin/ に OS ごとのバイナリと bin/canon.exe（Windows の MCP 起動用）を作る
```
標準スキーマの場所は `--schema`、環境変数 `CANON_SCHEMA`、`CLAUDE_PLUGIN_ROOT/schema`、実行ファイルの `../schema` の順に探す。
