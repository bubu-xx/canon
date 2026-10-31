# schema（v0）

標準スキーマ（期待構造）。型ごとに必須フィールド・着手条件（必須／注意）・必須リンクを定義する。
プロジェクト固有の追加は対象プロジェクトの `.canon/schema-ext/` に置く。

v0 の対象：REQ（要求）・BHV（振る舞い）・TST（試験）

全フィールド共通の形：
```yaml
<field>:
  value: ...
  unit: ...                # 任意
  status: confirmed | unknown | conflict
  source: { kind: document | code | answer | derived, ref: ... }
  claims: [...]            # conflict のときのみ
  question: Q-xxx          # unknown / conflict のとき
```

## ファイル
| ファイル | 内容 |
|---|---|
| `common.yaml` | 要素・フィールド・出典・リンクの共通の形、状態ごとの規則 |
| `types/req.yaml` | 要求 |
| `types/bhv.yaml` | 振る舞い |
| `types/tst.yaml` | 試験 |
| `types/question.yaml` | 質問（Canon の値ではない） |
| `examples/` | 各型の記述例。validate の試験データにも使う |

`examples/` は Q-001・Q-002 を参照しているが実体を置いていない。参照切れを validate が検出できるかの確認に使う。
