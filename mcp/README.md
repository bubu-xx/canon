# mcp

MCP サーバーの実装は `tools/internal/mcpserver`、起動は `canon mcp`、登録はリポジトリ直下の `.mcp.json`。
Claude Code からはツール名 `mcp__plugin_canon_canon__<ツール>` で呼べる。

| ツール | 返すもの |
|---|---|
| query | ID・型・状態・文字列で要素を検索（値はすべて出典付き） |
| impact | ある要素を変えたときに見直しが要る要素（参照元を推移的にたどる） |
| gaps | 空白・矛盾・リンク不足・未回答の質問 |
| context_pack | 課題に着手する前の材料：対象要素と関連要素、止まるべき条件（必須の空白・矛盾）、未回答の質問、実装とのズレ |
| validate | Canon 全体の検査結果 |

対象プロジェクトは環境変数 `CLAUDE_PROJECT_DIR`（無ければ起動ディレクトリ）から `.canon/` を上へ探して決める。
