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
- **矛盾と記述不足の見分け**：「記述を信じてコードを使った人が、記述と違う結果に出会うか」。出会うなら inconsistent、記述の範囲では期待どおりで、記述に無い場面の振る舞いが書かれていないだけなら incomplete。具体的には：
  - 記述が**言い切っている**こと（「〜する」「〜を返す」「never」「every」「〜の順で探す」「〜が名前を示す」）に対し、そうならない入力・状態・環境が1つでもある → inconsistent
  - 記述が**列挙**しているもの（探す場所、扱う形式）は、条件が書かれていなければ、すべての環境で列挙どおりに行われると読む。1つでも行わない環境があれば → inconsistent
  - 記述が**触れていない**入力・状態（ファイルが無いとき、判定の方法、追加の形式）でコードが何かをする → incomplete
  - 記述が判定の方法を書いていなければ、方法の違いは incomplete。方法を書いていて、それと違えば inconsistent

## 区切り（unit）
- 区切りは道具が決める。`canon verify --init` が情報源を機械的に区切った雛形（`.canon/verify.yaml`）を作る：表はデータ行・列ごと、箇条書きは項目ごと、段落は文ごと、コードブロックは全体で1つ
- 各文の `unit` と `source` は書き換えない。`canon verify` が、すべての区切りに記録があるか、文面が情報源と同じかを確かめる
- 1つの区切りに複数の主張があれば、同じ `unit`・`source` の文を増やし、`text` で主張ごとに分ける（「A であり、B のときは C」→ 2文）。**まとめてはいけない**
- 区切りを自分で作り直さない。回ごとに細かさが変わると、照合の深さも、別の回との突き合わせも揺れる

## 判定
| verdict | 意味 | 必要なもの |
|---|---|---|
| consistent | コードが記述どおりに動いている（仕組みも含め、記述の要素をすべて確かめた） | code |
| inconsistent | **矛盾**：コードが記述と違うことをしている（記述どおりに動かない場合がある）。曖昧で、コードと合わない読み方ができる記述もここ | code、note（何が違うか）、question |
| incomplete | **記述不足**：コードは記述と矛盾しないが、記述に無い条件・例外・追加の挙動がある（記述は正しいが足りない） | code、note（記述に無いもの） |
| not_implemented | 対象範囲のコードに、記述にあたる実装が見当たらない | note、question |
| not_checkable | コードでは確かめられない（UI の見た目、運用、将来の予定、他の文書への案内など） | note |

consistent と incomplete には、手順4の反例探しの結果（`review: kept / changed`、changed なら `review_note`）が要る。

`scope: out` は、記述が対象範囲のコードではない部分（他のアプリ、書き込み側、別のクレート）の話であるとき。note に何の話かを書く。scope: out の文には verdict は不要。

## 書式（`.canon/verify.yaml`）
```yaml
entries:
  - anchor: "studio-interchange 2026-10-17 §Renditions 表"
    statements:
      - unit: 2
        source: "`.pcraft` (atelier Photo, zip or folder bundle) › Where in the file: `composite/full.png`, named by `manifest.json` › `rendition`"
        text: ".pcraft のレンディションは composite/full.png で、manifest.json › rendition が名前を示す"
        scope: in
        code: ["libs/crates/interchange/src/lib.rs:199-222"]
        verdict: inconsistent
        note: "コードは固定名 composite/full.png を読み、manifest.json の rendition を参照しない"
        question: Q-010
      - unit: 3
        source: "`.pcraft` (atelier Photo, zip or folder bundle) › Written by: `atelier_photo_io::interchange::rendition_png`, on every `.pcraft` save from the app, CLI, MCP and engine"
        text: ".pcraft の合成画像は atelier_photo_io::interchange::rendition_png が保存時に書く"
        scope: out
        note: "書き込み側（atelier Photo）の話"
```

## 手順
1. `canon verify --init` で雛形を作る（既にあれば作り直さず続きを埋める）
2. 区切りごとに、対象範囲のコードを grep・Read で探して判定する。複数の主張があれば text で分ける
3. inconsistent / not_implemented は、`canon next-id question` で質問を起票する（ask スキルの書式。context に記述とコードの位置、options に「設計書どおりに実装を直す」「実装どおりに設計書を直す」）。関係する Canon の項目があれば conflict にする
4. **反例探し**：consistent と incomplete の**すべての文**を、照合をしていない別のエージェント（Agent ツールで refuter エージェントを新しく起動する。文書ごと、または10〜20文ずつに分けて並行で起動してよい）に渡す。渡すのは anchor・unit・text・verdict・code だけで、note やこれまでの判断は渡さない。返ってきた結果を記録する：
   - kept → `review: kept`
   - changed → `review: changed`、`review_note` に反例、verdict を返ってきた判定に変える（note も反例に合わせて直す）。inconsistent に変えたものは質問を起票する
   - 返ってきた反例は、自分でもコードの行を読んで確かめる。行で示せないものは採らない（そのときは kept とし、理由を review_note に書く）
5. `canon verify` を実行し、記録の不備が 0 になるまで直す
6. 利用者に報告する：照合した箇所・区切り・文の数、判定の内訳、反例探しで変わった件数、矛盾・未実装の一覧（人の判断が要る）、記述不足の一覧（設計書の追記候補）

別の回の照合（同じ情報源で作ったもの）と比べるときは `canon verify --compare <もう一方の verify.yaml>`。区切りごとに判定を突き合わせ、矛盾・未実装の和集合を出す。
