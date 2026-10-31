---
name: canon-init
description: 対象プロジェクトに .canon/ を作り、Canon を使い始める準備をする。canon を初めて使うプロジェクトで使う。
disable-model-invocation: true
---

# canon-init

1. プロジェクトのルートで `canon init` を実行する（既にあれば足りないものだけ追加される）。
2. 作成された `.canon/config.yaml` を利用者に確認して埋める。分からない項目は空のままでよい。
   - `profile`：プロジェクトの種類。`business`（業務システム。画面・権限・業務の前提がある）か `library`（ライブラリ・デスクトップアプリ。API・関数が中心）。コードを見て提案し、利用者に決めてもらう
   - `project`・`scope`（対象範囲）・`question_owners`（質問の宛先）
3. 次の手順を利用者に伝える：
   - 情報源（要件定義書・設計書・試験書など）を `/canon:sources` で位置情報付きテキストにして `.canon/sources/` に置く
   - `/canon:ingest` で Canon に取り込む

`.canon/` の構成は `.canon/README.md` を参照。
