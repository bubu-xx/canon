---
name: verify
description: 情報源（設計書）のすべての箇所を文の単位で、実現しているコードと突き合わせ、一致・不一致・未実装を .canon/verify.yaml に記録する。設計書とコードの食い違いを洗い出すとき、取り込み（ingest）の後に使う。
---

# verify：箇所単位の照合

設計書の記述とコードが同じことを言っているかを、**情報源のすべての箇所（アンカー）について、文の単位で**確かめて記録する。Canon の要素の作り方に左右されずに、食い違いを漏れなく洗い出すための手順。

## 原則
- **全部の箇所をやる**。`.canon/sources/` のすべての `<!-- src: ... -->` に記録を作る。`canon verify` が漏れを検出する
- **どちらが正しいかは決めない**。食い違いを見つけたら記録して質問にする（設計書が古いのか、実装の不具合かは人が決める）
- **言い換えて一致にしない**。記述が仕組み（「X が名前を示す」「X を読む」「X の順で探す」等）を述べているなら、コードがその仕組みどおりに動いているかを確かめる。結果が同じでも仕組みが違えば `inconsistent`
- **迷ったら inconsistent か not_checkable**。一致と判定するのは、コードの該当行を読んで記述のすべての要素が確かめられたときだけ
- **読み方が複数ある記述**（「else」が何に掛かるか、など）は、コードと合わない読み方が1つでもあれば inconsistent。曖昧さそのものを人に返す
- **条件で挙動が分かれるコード**（OS ごと、種類ごと、入力の大きさごと）は、記述が条件を書いていなければ、すべての条件で記述どおりかを確かめる。1つの条件でも違えば inconsistent
- **注記に差を書いたら一致にしない**。「ただし」「〜しない」「〜だけ」「記述に無い」など、コードと記述の差を note に書いた時点で、その文は consistent ではない。記述どおりに動かない場合があるなら inconsistent、記述どおりに動いたうえで記述に無いことも起きるなら incomplete
- **矛盾と記述不足の見分け**：「記述を信じてコードを使った人が、記述と違う結果に出会うか」。出会うなら inconsistent、記述の範囲では期待どおりで、記述に無い場面の振る舞いが書かれていないだけなら incomplete

## 文の分け方
- 箇条書きは1項目を1文、表は1行を1文（列が複数の主張を持つなら列ごとに分けてよい）、段落は主張ごとに分ける
- 1つの文に複数の主張（「A であり、B のときは C」）があれば分ける

## 判定
| verdict | 意味 | 必要なもの |
|---|---|---|
| consistent | コードが記述どおりに動いている（仕組みも含め、記述の要素をすべて確かめた） | code |
| inconsistent | **矛盾**：コードが記述と違うことをしている（記述どおりに動かない場合がある）。曖昧で、コードと合わない読み方ができる記述もここ | code、note（何が違うか）、question |
| incomplete | **記述不足**：コードは記述と矛盾しないが、記述に無い条件・例外・追加の挙動がある（記述は正しいが足りない） | code、note（記述に無いもの） |
| not_implemented | 対象範囲のコードに、記述にあたる実装が見当たらない | note、question |
| not_checkable | コードでは確かめられない（UI の見た目、運用、将来の予定、他の文書への案内など） | note |

`scope: out` は、記述が対象範囲のコードではない部分（他のアプリ、書き込み側、別のクレート）の話であるとき。note に何の話かを書く。scope: out の文には verdict は不要。

## 書式（`.canon/verify.yaml`）
```yaml
entries:
  - anchor: "studio-interchange 2026-10-17 §Renditions 表"
    statements:
      - text: ".pcraft のレンディションは composite/full.png で、manifest.json › rendition が名前を示す"
        scope: in
        code: ["libs/crates/interchange/src/lib.rs:199-222"]
        verdict: inconsistent
        note: "コードは固定名 composite/full.png を読み、manifest.json の rendition を参照しない"
        question: Q-010
      - text: ".pcraft の合成画像は atelier_photo_io::interchange::rendition_png が保存時に書く"
        scope: out
        note: "書き込み側（atelier Photo）の話"
```

## 手順
1. `.canon/sources/` を読み、アンカーの一覧を作る
2. アンカーごとに文に分け、対象範囲のコードを grep・Read で探して判定する
3. inconsistent / not_implemented は、`canon next-id question` で質問を起票する（ask スキルの書式。context に記述とコードの位置、options に「設計書どおりに実装を直す」「実装どおりに設計書を直す」）。関係する Canon の項目があれば conflict にする
4. **見直し**：consistent で note のある文を、**照合をしていない別のエージェント**（Agent ツールで新しく起動する。これまでの判断を渡さず、記述・note・コードの位置だけを渡す）に、上の原則で見直させる。差があれば verdict を inconsistent か incomplete に変えて `review: changed`、本当に一致なら `review: kept` を書く。inconsistent に変えたものは質問を起票する
5. `canon verify` を実行し、記録の不備が 0 になるまで直す
6. 利用者に報告する：照合した箇所と文の数、判定の内訳、見直しで変わった件数、矛盾・未実装の一覧（人の判断が要る）、記述不足の一覧（設計書の追記候補）
