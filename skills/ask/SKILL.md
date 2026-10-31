---
name: ask
description: Canon の空白（unknown）と矛盾（conflict）について人に聞く質問を整え、回答を Canon に取り込む。質問を出すとき、利用者が質問に答えたときに使う。
---

# ask：質問と回答

## 質問を出す前に
各 unknown / conflict について、**全情報源とコードをもう一度探す**（別の言い回し・別の文書）。根拠が見つかれば質問にせず reconcile の手順で埋める。見つからなかったものだけを質問にする。

## 質問の書式（`.canon/questions/Q-xxx.yaml`）
```yaml
id: Q-002
type: question
asks_for: [BHV-001.error_cases]   # この回答で埋まる空白。複数可
blocking: true                     # asks_for に着手条件（スキーマの blocking: true）の項目があれば true
to: "注文チーム"                    # config.yaml の question_owners を参照
context: "該当なしの場合の挙動が、詳細設計書（空の一覧）と実装（E-404）で食い違っている。他の情報源に記載なし"
options: ["空の一覧を返す（設計書どおり）", "E-404 を返す（実装どおり）"]   # 候補。既定値ではない
status: open
```
- **1問1判断**。1つの質問は1つの要素の1つの決めごとを聞く。「要求4件の種類・優先度・受入条件」のような束ね方はしない
- まとめてよいのは、1つの答えで本当に全部が決まるとき（例：上流の要求の答えで下流の振る舞いの項目も決まる）だけ。asks_for は3項目まで
- 答えやすい形にする：context に「何が分かっていて何が無いか」と根拠の位置、options に具体的な候補（コードの値など）を書く
- 項目がその要素に当てはまらない可能性があれば、options に「該当なし」を入れる
- 利用者に見せるときは `canon gaps --blocking` と `canon render` の「未確定事項」を使い、必須のものから並べる

## 文書とコードの矛盾への回答は、次の行動を決める
- **コードが正しい**（設計書が古い）→ Canon はコードの値で確定。設計書は Canon から再生成すれば直る。課題は不要
- **設計書が正しい**（実装の不具合）→ Canon は設計書の値で確定し、**課題として起票する**（フェーズ2の修正対象）。質問の answer に `action: fix_code` と課題の参照を書く
- 回答の `action` は `none` / `fix_code` / `fix_doc` のいずれか

## 回答を取り込む
回答は**利用者がこの会話で答えたもの、または質問ファイルに人が書いたものだけ**。AI が回答を作らない。回答者の名前が分からなければ聞く。
1. 質問ファイルに `answer: { value: ..., by: "回答者", at: "日時", action: none | fix_code | fix_doc }` と `status: answered` を書く
2. asks_for の各項目を更新する：
   ```yaml
   error_cases:
     value: ["該当なしの場合は空の一覧を返す"]
     status: confirmed
     source: { kind: answer, by: "鈴木", at: "2026-10-31T13:40+09:00", question: Q-002 }
   ```
   conflict だった項目は claims を消して confirmed にする。回答が「該当なし」なら `status: not_applicable` にし、source は同じく answer にする
3. 回答で新たに生じた空白があれば、新しい質問を起票する
4. `canon validate` を実行し、反映結果を利用者に報告する。回答が実装と食い違う場合（例：設計どおりに決まったが実装は違う）は、修正が必要なことも伝える
