---
name: ingest
description: .canon/sources/ の情報源とコードから要素（要求・振る舞い・試験）を抽出し、出典付きで Canon に取り込む。設計書やコードを Canon に取り込むとき、Canon を作るときに使う。
---

# ingest：Canon への取り込み

## 原則（必ず守る）
- **根拠（出典）のない値は書かない。推測で埋めない。**
- 値の出典は `.canon/sources/` のアンカー（`<!-- src: ... -->`）と**同じ表記**で書く。アンカーに無い参照は validate で拒否される
- 情報源どうしが一致 → `confirmed`。食い違い → `conflict`（すべての主張を claims に残し、どちらも選ばない）。どこにも無い → `unknown`
- 文書・コード・人の回答は**同格の情報源**。どれが正しいかを AI が決めない
  - 文書に記載なし＋コードに値あり → コードを根拠に `confirmed`（`kind: code`、`パス:行`）。質問にしない
  - 文書とコードが食い違う → `conflict`（コードの主張は `kind: code` の claim）。設計書が古いのか実装が間違っているのかは人が決める
  - どちらにも無い → `unknown`
- スキーマ（`${CLAUDE_PLUGIN_ROOT}/schema/types/`）の必須項目は、値が無くても `unknown` として必ず置く。どの項目が必須かはプロファイル（`.canon/config.yaml` の `profile`、定義は `${CLAUDE_PLUGIN_ROOT}/schema/profiles/`）で変わる
- 必須でない項目は、根拠があるときだけ書く。根拠が無い任意項目を unknown で埋めて質問を増やさない
- 項目がその要素に当てはまらない場合（例：純粋な関数の「状態の変化」）は `status: not_applicable`。ただし根拠が要る：文書にそう書かれていれば document、無ければ AI が決めずに質問にする（選択肢に「該当なし」を入れる）

## 解釈を固定する規則（同じ入力から毎回同じ Canon にするため）
- **振る舞いの単位**：コードの公開関数・処理の入口（API、画面の操作、バッチ）1つにつき1つ。内部の関数は親の振る舞いの処理に含める。コードが無ければ文書の機能の単位
- **試験の単位**：試験関数（`#[test]`、`func TestXxx`、`test_xxx` 等）1つにつき1つ。文書の試験項目が試験関数と対応しなければ別の要素にする
- **要求の単位**：文書の要求1文につき1つ。コードからは要求を作らない（コードは「どうなっているか」で、要求は「なぜ・何を」）
- **必須項目に該当する記述が無いとき**：文書にその項目の記述（受入条件なら受入条件・合格基準として書かれた文）が無ければ、他の記述を読み替えて埋めない。`unknown` にし、候補になる記述（テストの期待結果、関連する文）は質問の options と context に載せる
- **裏付けは全部書く**：同じ値を述べている情報源が複数あれば、1つを `source` に、残りを `also` に書く（文書とコードの両方にあるなら両方）。片方だけにしか書かないと、後の照合ができない
- **情報源の箇所はすべて使う**：取り込み後の `canon gaps` で「出典として使われていない情報源の箇所」が出たら、その箇所の内容を Canon のどこに入れるかを決めて取り込む。入れる場所が無い（スキーマに型・項目が無い）なら、取り込まずにその旨を報告する（スキーマ改修候補）

## 手順
1. `canon gaps` で現状を見る。`.canon/sources/` を読み、対象範囲の要素候補（要求・振る舞い・試験）を洗い出す
2. 要素ごとに `canon next-id <req|bhv|tst>` で ID を取る（既存の要素と同じものなら新しく作らず更新する）
3. スキーマの項目ごとに、全情報源とコードから根拠を探し、上の原則で状態を決める
4. **先に質問を作る**：`canon next-id question` で ID を取り、`.canon/questions/Q-xxx.yaml` を書く（書式は ask スキル）。**1問1判断**：1つの質問は1つの要素の1つの決めごとを聞く。まとめてよいのは、1つの答えで本当に全部が決まるときだけ（asks_for は3項目まで。超えると validate が警告する）
5. 要素を `.canon/canon/<型>/<ID>.yaml` に書く。リンク（derived_from・verified_by・verifies 等）にも出典を付ける（トレーサビリティ表など）
6. `canon validate` を実行し、エラーが 0 になるまで直す（書き込み時にもフックが検査する）
7. `canon gaps` で「出典として使われていない情報源の箇所」を確認し、上の規則に従って取り込むか報告する
8. `canon crosscheck` を実行する。設計書の識別子のうち、同じ要素が出典にしているコードに見当たらないものが `.canon/crosscheck.yaml` に一覧になる。**AI は一致・不一致を判断しない**（resolution は open のまま。人が決める）
9. 利用者に報告する：取り込んだ要素数、確定率、必須の空白・矛盾と質問、情報源の網羅、crosscheck の確認待ちの件数

## 要素の書式
```yaml
id: BHV-001
type: bhv
name: 注文履歴の検索処理
fields:
  summary:
    value: "条件に合う会員本人の注文を新しい順に返す"
    status: confirmed
    source: { kind: document, ref: "詳細設計書 v2.0 シート7 B3:F5" }
    also:
      - { kind: code, ref: "src/order/search.go:40-60" }
  timeout:
    status: unknown
    question: Q-003
  error_cases:
    status: conflict
    claims:
      - value: ["該当なしの場合は空の一覧を返す"]
        source: { kind: document, ref: "詳細設計書 v2.0 シート7 B30" }
      - value: ["該当なしの場合はエラー E-404 を返す"]
        source: { kind: code, ref: "src/order/search.go:56" }
    question: Q-002
links:
  - type: derived_from
    target: REQ-001
    source: { kind: document, ref: "基本設計書 v1.2 トレーサビリティ表" }
```
- 単位がある項目（timeout など）は `unit` を付ける（使える単位はスキーマ参照）
- コードの位置は対象プロジェクトのルートからの相対パス `パス:行` または `パス:開始-終了`
