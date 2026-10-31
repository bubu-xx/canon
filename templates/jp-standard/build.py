"""spec.yaml から、設計書・試験仕様書の Excel テンプレートと対応表を作る。

使い方: python build.py   （openpyxl と PyYAML が必要）
出力  : このフォルダの <file>.xlsx と <file>.map.yaml
"""
import os
import sys

import yaml
from openpyxl import Workbook
from openpyxl.styles import Alignment, Border, Font, PatternFill, Side
from openpyxl.utils import get_column_letter

HERE = os.path.dirname(os.path.abspath(__file__))
FONT = "Meiryo UI"
C_TITLE = "1F3864"
C_HEAD = "D9E1F2"
C_LABEL = "EEF2F8"
C_BAND = "1F3864"

thin = Side(style="thin", color="7F8FA6")
BORDER = Border(left=thin, right=thin, top=thin, bottom=thin)
WRAP = Alignment(wrap_text=True, vertical="top")
CENTER = Alignment(wrap_text=True, vertical="center", horizontal="center")


def font(size=10, bold=False, color="000000"):
    return Font(name=FONT, size=size, bold=bold, color=color)


def fill(c):
    return PatternFill("solid", fgColor=c)


def setup_print(ws, title, landscape):
    ws.page_setup.paperSize = ws.PAPERSIZE_A4
    ws.page_setup.orientation = "landscape" if landscape else "portrait"
    ws.page_setup.fitToWidth = 1
    ws.page_setup.fitToHeight = 0
    ws.sheet_properties.pageSetUpPr.fitToPage = True
    ws.page_margins.left = ws.page_margins.right = 0.5
    ws.oddHeader.right.text = title
    ws.oddHeader.right.size = 8
    ws.oddFooter.center.text = "&P / &N"
    ws.oddFooter.center.size = 8
    ws.sheet_view.showGridLines = False


def header(ws, doc_title, sheet_title, last_col, cells):
    """各シート上部：文書名・シート名・システム名。"""
    ws.row_dimensions[1].height = 6
    ws.merge_cells(f"A2:{last_col}2")
    c = ws["A2"]
    c.value = f"{doc_title}　{sheet_title}"
    c.font = font(13, True, "FFFFFF")
    c.fill = fill(C_BAND)
    c.alignment = Alignment(vertical="center", indent=1)
    ws.row_dimensions[2].height = 24
    ws["A3"].value = "システム名"
    ws["A3"].font = font(9, True)
    ws["B3"].font = font(9)
    cells["B3"] = {"expr": "project", "label": "システム名"}


def cover(wb, doc, cells):
    ws = wb.active
    ws.title = "表紙"
    for col, w in zip("ABCDEFGH", [4, 18, 14, 14, 14, 14, 14, 4]):
        ws.column_dimensions[col].width = w
    ws.merge_cells("B6:G6")
    ws["B6"].value = doc["title"]
    ws["B6"].font = font(26, True, C_TITLE)
    ws["B6"].alignment = Alignment(horizontal="center")
    ws.row_dimensions[6].height = 44
    ws.merge_cells("B8:G8")
    ws["B8"].font = font(14)
    ws["B8"].alignment = Alignment(horizontal="center")
    cells["B8"] = {"expr": "project", "label": "システム名"}
    rows = [
        ("文書名", f"={doc['title']}", "文書名"),
        ("版数", "=", "版数"),
        ("作成日", "today", "作成日"),
        ("生成元", "=Canon（検証済みの正解情報）から canon render で生成", "生成元"),
        ("Canon の確定率", "stat.confirmed_rate", "確定率"),
        ("未回答の質問", "stat.open_questions", "未回答の質問"),
    ]
    r = 12
    for label, expr, lbl in rows:
        ws.cell(r, 2, label).font = font(10, True)
        ws.cell(r, 2).fill = fill(C_LABEL)
        ws.merge_cells(start_row=r, start_column=3, end_row=r, end_column=7)
        for c in range(2, 8):
            ws.cell(r, c).border = BORDER
        ws.cell(r, 3).font = font(10)
        ws.cell(r, 3).alignment = WRAP
        cells[f"C{r}"] = {"expr": expr, "label": lbl}
        r += 1
    # 承認欄
    r += 2
    for i, label in enumerate(["承認", "確認", "作成"]):
        col = 5 + i
        ws.cell(r, col, label).font = font(9, True)
        ws.cell(r, col).alignment = CENTER
        ws.cell(r, col).fill = fill(C_LABEL)
        ws.cell(r, col).border = BORDER
        ws.cell(r + 1, col).border = BORDER
    ws.row_dimensions[r + 1].height = 48
    ws.merge_cells(f"B{r+4}:G{r+4}")
    ws[f"B{r+4}"].value = ("凡例：黄＝未確定（質問の回答待ち）／橙＝情報源が矛盾／灰色斜体＝Canon にこの情報の置き場が無い。"
                           "この文書は手で編集せず、Canon を直して再生成すること。")
    ws[f"B{r+4}"].font = font(8, color="595959")
    ws[f"B{r+4}"].alignment = WRAP
    ws.row_dimensions[r + 4].height = 30
    setup_print(ws, doc["title"], False)


def history(wb, doc, cells):
    ws = wb.create_sheet("改訂履歴")
    widths = [8, 14, 60, 14]
    for i, w in enumerate(widths, 1):
        ws.column_dimensions[get_column_letter(i)].width = w
    header(ws, doc["title"], "改訂履歴", "D", cells)
    for i, h in enumerate(["版数", "改訂日", "改訂内容", "改訂者"], 1):
        c = ws.cell(5, i, h)
        c.font = font(10, True)
        c.fill = fill(C_HEAD)
        c.border = BORDER
        c.alignment = CENTER
    for r in range(6, 21):
        for i in range(1, 5):
            ws.cell(r, i).border = BORDER
            ws.cell(r, i).font = font(10)
            ws.cell(r, i).alignment = WRAP
    cells["A6"] = {"expr": "=", "label": "版数"}
    cells["B6"] = {"expr": "today", "label": "改訂日"}
    cells["C6"] = {"expr": "=Canon から canon render で生成", "label": "改訂内容"}
    cells["D6"] = {"expr": "=canon", "label": "改訂者"}
    setup_print(ws, doc["title"], False)


def form(ws, doc, sheet, cells, start=5):
    ws.column_dimensions["A"].width = 20
    for col in "BCDEFGH":
        ws.column_dimensions[col].width = 14
    r = start
    for label, expr in sheet["items"]:
        ws.cell(r, 1, label).font = font(10, True)
        ws.cell(r, 1).fill = fill(C_LABEL)
        ws.cell(r, 1).alignment = WRAP
        ws.merge_cells(start_row=r, start_column=2, end_row=r, end_column=8)
        for c in range(1, 9):
            ws.cell(r, c).border = BORDER
        ws.cell(r, 2).font = font(10)
        ws.cell(r, 2).alignment = WRAP
        cells[f"B{r}"] = {"expr": expr, "label": label}
        r += 1


def table(ws, doc, sheet, columns, start=5):
    for i, (label, expr, width) in enumerate(columns, 1):
        col = get_column_letter(i)
        ws.column_dimensions[col].width = width
        c = ws.cell(start, i, label)
        c.font = font(10, True)
        c.fill = fill(C_HEAD)
        c.border = BORDER
        c.alignment = CENTER
        d = ws.cell(start + 1, i)
        d.border = BORDER
        d.font = font(10)
        d.alignment = WRAP
    ws.freeze_panes = ws.cell(start + 1, 1)
    return {get_column_letter(i): {"expr": expr, "label": label} for i, (label, expr, _) in enumerate(columns, 1)}


QUESTION_COLUMNS = [
    ["質問ID", "id", 9], ["区分", "blocking", 8], ["宛先", "to", 12], ["対象項目", "asks_for", 26],
    ["内容", "context", 50], ["回答候補", "options", 34], ["状態", "status", 9],
    ["回答", "answer.value", 28], ["回答者", "answer.by", 12], ["回答日", "answer.at", 14],
]


def build(doc):
    wb = Workbook()
    smap = []
    for sheet in doc["sheets"]:
        cells = {}
        kind = sheet["kind"]
        if kind == "cover":
            cover(wb, doc, cells)
            smap.append({"template": "表紙", "cells": cells})
        elif kind == "history":
            history(wb, doc, cells)
            smap.append({"template": "改訂履歴", "cells": cells})
        elif kind == "form":
            ws = wb.create_sheet(sheet["name"])
            header(ws, doc["title"], sheet["name"], "H", cells)
            form(ws, doc, sheet, cells)
            setup_print(ws, doc["title"], False)
            smap.append({"template": sheet["name"], "cells": cells})
        elif kind == "each":
            ws = wb.create_sheet(sheet["name"])
            header(ws, doc["title"], sheet["name"], "H", cells)
            form(ws, doc, sheet, cells)
            setup_print(ws, doc["title"], False)
            smap.append({"template": sheet["name"], "each": sheet["each"], "name": "{id}", "cells": cells})
        elif kind in ("table", "questions"):
            name = sheet.get("name", "課題管理表")
            columns = sheet.get("columns", QUESTION_COLUMNS)
            ws = wb.create_sheet(name)
            last = get_column_letter(len(columns))
            header(ws, doc["title"], name, last, cells)
            cols = table(ws, doc, sheet, columns)
            setup_print(ws, doc["title"], True)
            smap.append({"template": name, "cells": cells})
            entry = {"template": name, "rows": sheet.get("rows", "questions"), "start": 6, "columns": cols}
            if sheet.get("where"):
                entry["where"] = sheet["where"]
            smap.append(entry)
        else:
            sys.exit(f"unknown kind {kind}")
    out = os.path.join(HERE, doc["file"])
    wb.save(out + ".xlsx")
    m = {"document": doc["title"], "sheets": merge(smap)}
    with open(out + ".map.yaml", "w", encoding="utf-8") as f:
        f.write(f"# {doc['title']} の対応表（build.py が spec.yaml から生成。直接編集しないこと）\n")
        yaml.safe_dump(m, f, allow_unicode=True, sort_keys=False, width=200)
    print("built", doc["file"])


def merge(smap):
    """同じシートの cells だけの指定と rows の指定を1つにまとめる。"""
    out = []
    for e in smap:
        if out and out[-1]["template"] == e["template"] and "rows" in e and "rows" not in out[-1] and "each" not in out[-1]:
            out[-1].update({k: v for k, v in e.items() if k != "template"})
        else:
            out.append(e)
    return out


if __name__ == "__main__":
    with open(os.path.join(HERE, "spec.yaml"), encoding="utf-8") as f:
        spec = yaml.safe_load(f)
    for d in spec["documents"]:
        build(d)
