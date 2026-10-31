# hooks

| イベント | 動き |
|---|---|
| SessionStart | `.canon/` があれば、canon の原則と現状（確定率・必須の空白・未回答の質問）を AI に伝える |
| PreToolUse（Write / Edit / MultiEdit） | `.canon/canon/`・`.canon/questions/` への書き込みを、書き込む前の内容で `validate` し、違反があれば止める。既存の `.canon/sources/` の書き換えと `.canon/views/` の編集も止める |
| Stop | 作業終了時に Canon 全体を `validate` し、違反が残っていれば終了させずに直させる |

すべてのフックは、対象プロジェクトに `.canon/` が無ければ何もしない。

## 制限
Bash ツール経由（スクリプトや `cat >` など）での書き込みは PreToolUse では捕まえられない。その場合も Stop フックの全体検査で違反は検出される。
