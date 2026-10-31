---
name: render
description: Canon から人向けの文書（要件一覧・振る舞い仕様・試験仕様・未確定事項・トレーサビリティ）を生成する。設計書や未確定事項の一覧が必要なときに使う。
---

# render：文書の生成

`canon render` を実行する（出力先は `.canon/views/`、`--out` で変更可）。

- Excel が欲しいとき：`canon render --format xlsx`（標準様式）
- 会社の様式があるとき：`canon render --format xlsx --template <様式.xlsx> --map <対応表.yaml>`。対応表が無ければ、様式を読んでどのセルに何を入れるかの対応表を作り、利用者に確認してから出力する（書式は `${CLAUDE_PLUGIN_ROOT}/evals/templates/処理仕様書_様式.map.yaml`）

- 文書はテンプレートで機械的に生成される。LLM で書き直したり、生成後に手で直したりしない（直すときは Canon を直して再生成）
- 未確定の項目は「未確定（質問ID）」、矛盾は各情報源の記述を並べて表示される
- 生成したファイルの一覧と、未確定事項の件数を利用者に伝える
