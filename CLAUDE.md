# canon — 開発時の規則

このリポジトリは、プロジェクトの検証済みの正解情報（Canon）を組み立てるハーネス本体（Claude Code プラグイン）です。

## 原則（すべての部品が守る）
1. 推測で埋めない。根拠（出典）のない値は Canon に入れない。
2. 分からないものは `status: unknown` として残し、質問に変える。
3. 情報源どうしが食い違うものは `status: conflict` として残し、勝手に選ばない。
4. 根拠のある値だけが Canon になる。人の回答は情報源の一つとして扱う。

## 用語
- 情報源（sources）：設計書、コード、人の回答など。複数ある。
- Canon：情報源を照合して確定させた、プロジェクトの正しい情報（Single Version of Truth）。
- 観測（observations）：コードや稼働環境から読み取った「実際の状態」。Canon とは別に持つ。

## 構成
- `skills/` 手順（取り込み・照合・質問・観測・文書生成）
- `agents/` 役割を分けたサブエージェント
- `hooks/` Canon 書き込み時の検証など、守らないと動かない仕組み
- `tools/` Go 製 CLI（validate / gaps / drift / render）
- `mcp/` 他の AI から Canon を読む提供口
- `schema/` 標準スキーマ（期待構造）
- `evals/` ハーネス自体の評価
- `bin/` 配布用バイナリと、OS に合うバイナリを呼ぶ起動スクリプト（Go を直したら `tools/build.sh` で作り直してからコミット）
- `.mcp.json` MCP サーバーの登録

## 実装の約束
- ツールは Go、Canon のデータは YAML。
- 検査ロジックはスキルに埋め込まず、必ず `tools/` に書く。
- 手順は CLAUDE.md ではなく `skills/` に書く。このファイルは短く保つ。
