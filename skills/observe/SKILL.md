---
name: observe
description: コードから実装の状態を観測して .canon/observations/ に記録し、Canon とのズレ（drift）を出す。コードを変更した後、Canon と実装が一致しているか確かめるときに使う。
---

# observe：実装の観測

観測は Canon ではない。**Canon には書き込まない**。

## 手順
1. `git rev-parse HEAD` で現在のコミットを得る
2. 振る舞い（bhv）ごとに、対応するコードを特定する（コード中の要素 ID コメント、試験コード、名前と処理内容から）
3. スキーマの各項目について、コードが実際にどうしているかを読み取り、`.canon/observations/<ID>.yaml` に書く：
   ```yaml
   element: BHV-001
   commit: <コミット>
   observed_at: "<日時>"
   complete: false          # 全項目を確かめたときだけ true（true にすると未実装の検出が有効になる）
   fields:
     error_cases:
       value: ["該当なしの場合はエラー E-404 を返す"]
       ref: "src/order/search.go:56"
   ```
   値は Canon と比べられるよう、Canon の同じ項目と同じ書き方（リスト・言い回し）に揃える。
   コードで実際に確かめられた事実だけを書く。当てはまらない項目に「なし」と書かない（書かない項目は観測していないものとして扱われる）。Canon で not_applicable の項目は観測しない
4. どの要素にも対応しないコードは `.canon/observations/unmapped.yaml` に書く：
   ```yaml
   commit: <コミット>
   entries:
     - { ref: "src/order/export.go:1-80", note: "CSV 出力。対応する要求・振る舞いが Canon に無い" }
   ```
5. `canon drift` を実行し、結果を利用者に報告する：
   - value_mismatch：Canon と実装が違う（バグか、Canon への未反映の変更）
   - impl_detail：Canon の内容はすべて実装にあり、実装が詳細を足している（足した部分は質問か情報源の追記の候補）
   - impl_only：実装にだけある値 → ask スキルで質問の候補にする（コードの値を意図として採るか）
   - canon_only：Canon にあるが実装に無い（未実装の可能性）
   - conflict_open：Canon が矛盾のまま
   - unmapped_code：対応付けの穴。要素の追加候補
   - 観測していない要素の数（カバレッジ）も必ず伝える。見えていない領域を隠さない
