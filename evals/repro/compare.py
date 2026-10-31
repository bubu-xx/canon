"""同じ情報源を複数回取り込んだ結果（run*/.canon）を比べ、検出の揺れを出す。

使い方: python3 compare.py <runs のフォルダ> [--probe 名前=正規表現 ...]
  --probe は「この食い違いが各回でどう扱われたか」を調べる。正規表現は Canon と質問の全文に当てる。
"""
import glob
import os
import re
import sys

import yaml


def load(run):
    base = os.path.join(run, ".canon")
    els, qs = [], []
    for f in glob.glob(os.path.join(base, "canon", "**", "*.yaml"), recursive=True):
        with open(f, encoding="utf-8") as fh:
            els.append(yaml.safe_load(fh))
    for f in glob.glob(os.path.join(base, "questions", "*.yaml")):
        with open(f, encoding="utf-8") as fh:
            qs.append(yaml.safe_load(fh))
    return els, qs


def field_key(e, fname, f):
    """要素名は回ごとに揺れるので、型＋主要な出典で要素を同定する。"""
    anchor = ""
    for cand in ("statement", "summary", "objective"):
        c = e.get("fields", {}).get(cand) or {}
        src = c.get("source") or {}
        if src.get("ref"):
            anchor = src["ref"]
            break
    return f"{e.get('type')}[{anchor}].{fname}"


def summarize(run):
    els, qs = load(run)
    out = {"elements": {}, "status": {}, "conflicts": set(), "unknowns": set(), "questions": len(qs), "blocking": 0}
    for e in els:
        out["elements"][e.get("type")] = out["elements"].get(e.get("type"), 0) + 1
        for fname, f in (e.get("fields") or {}).items():
            st = (f or {}).get("status")
            out["status"][st] = out["status"].get(st, 0) + 1
            k = field_key(e, fname, f)
            if st == "conflict":
                out["conflicts"].add(k)
            elif st == "unknown":
                out["unknowns"].add(k)
    out["blocking"] = sum(1 for q in qs if q.get("blocking"))
    return out


def probe(run, pattern):
    """食い違いの扱い：conflict / question / confirmed（黙って確定）/ absent。"""
    els, qs = load(run)
    rx = re.compile(pattern)
    hit = "absent"
    for e in els:
        for fname, f in (e.get("fields") or {}).items():
            text = yaml.safe_dump(f, allow_unicode=True)
            if rx.search(text):
                if (f or {}).get("status") == "conflict":
                    return "conflict"
                hit = (f or {}).get("status", hit)
    for q in qs:
        if rx.search(yaml.safe_dump(q, allow_unicode=True)):
            return "question" if hit != "conflict" else hit
    return hit


def main():
    args = sys.argv[1:]
    probes = []
    while "--probe" in args:
        i = args.index("--probe")
        name, _, pat = args[i + 1].partition("=")
        probes.append((name, pat))
        del args[i:i + 2]
    root = args[0]
    runs = sorted(d for d in glob.glob(os.path.join(root, "run*")) if os.path.isdir(d))
    sums = {r: summarize(r) for r in runs}
    print("| 回 | 要素 | 確定 | 空白 | 矛盾 | 質問 | 必須 |")
    print("|---|---|---|---|---|---|---|")
    for r, s in sums.items():
        els = " ".join(f"{k}{v}" for k, v in sorted(s["elements"].items()))
        st = s["status"]
        print(f"| {os.path.basename(r)} | {els} | {st.get('confirmed',0)} | {st.get('unknown',0)} | {st.get('conflict',0)} | {s['questions']} | {s['blocking']} |")
    allc = set().union(*(s["conflicts"] for s in sums.values()))
    allu = set().union(*(s["unknowns"] for s in sums.values()))
    print("\n## 矛盾として検出された項目（回ごとの有無）")
    for k in sorted(allc):
        print("-", k, "→", " ".join("○" if k in s["conflicts"] else "×" for s in sums.values()))
    print("\n## 空白として残った項目（回ごとの有無）")
    for k in sorted(allu):
        print("-", k, "→", " ".join("○" if k in s["unknowns"] else "×" for s in sums.values()))
    if probes:
        print("\n## 既知の食い違いの扱い")
        for name, pat in probes:
            print("-", name, "→", " / ".join(f"{os.path.basename(r)}:{probe(r, pat)}" for r in runs))


if __name__ == "__main__":
    main()
