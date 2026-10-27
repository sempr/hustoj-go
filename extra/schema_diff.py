#!/usr/bin/env python3
"""按线上实际结构与 schema/schema.sql 的差异生成对齐用的 DDL。

背景
----
`schema/schema.sql` 是结构的权威定义，`extra/check_schema.py` 负责比对。
对**存量库**（老版 HUSTOJ：MyISAM、缺失索引、其它 collation、列定义偏差），
不能直接重放 schema.sql —— 它含 `DROP TABLE`，会清空数据。需要一组等价的
`ALTER`。

本脚本把「线上 vs schema.sql」的差异翻译成 DDL 打到 stdout，供人工审阅后
按编号落入 `migrations/`（见 `migrations/00001_baseline.sql` 的说明）。
输出刻意只用 MariaDB 的幂等写法（`IF [NOT] EXISTS`、`MODIFY` 天然幂等），
使迁移脚本可中断、可重跑。

生成后请务必用 check_schema.py 复核：

    python3 extra/schema_diff.py > /tmp/0000x.sql
    mysql <目标库> < /tmp/0000x.sql
    DB_... python3 extra/check_schema.py        # 必须退 0

用法
----
    DB_HOST=127.0.0.1 DB_USER=hustoj DB_PASS=... DB_NAME=hustoj \
        python3 extra/schema_diff.py [选项]
    # 未给 DB_* 时复用 phpweb/include/db_info.inc.php 的凭据

选项
----
    --comments   连列注释一起对齐（默认忽略；注释是文档，通常无需统一）
    --table T    只生成该表（可重复），便于逐表核对
    -h, --help   显示本帮助
"""

from __future__ import annotations

import argparse
import re
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import check_schema as cs  # noqa: E402  复用解析器/取址/归一化，避免两套实现漂移

ROOT = Path(__file__).resolve().parent.parent
SCHEMA = ROOT / "schema" / "schema.sql"

IGNORED_TABLES = set(cs.IGNORED_TABLES)
INCLUDE_COMMENTS = False


# ── 解析 schema.sql：保留列的原始定义文本，供 MODIFY/ADD 原样回填 ──────
_CREATE = re.compile(r"CREATE TABLE `(\w+)`\s*\((.*?)\n\)\s*ENGINE=(\w+)([^;]*);",
                     re.S)
_COL = re.compile(r"`(\w+)`\s+(.+)$")
_KEY_PREFIXES = ("PRIMARY KEY", "UNIQUE KEY", "KEY ", "FULLTEXT KEY", "INDEX ")


def parse_schema(path: Path) -> dict:
    """{表: {engine, collate, cols:[(名, 原始定义)], keys:[原始定义], raw: 原始 CREATE}}。"""
    sql = path.read_text(encoding="utf-8")
    tables: dict = {}
    for m in _CREATE.finditer(sql):
        name, body, engine, tail = m.groups()
        cols: list[tuple[str, str]] = []
        keys: list[str] = []
        for line in body.split("\n"):
            s = line.strip().rstrip(",")
            if not s or s.startswith("--"):
                continue
            if s.startswith(_KEY_PREFIXES):
                keys.append(cs.WS.sub(" ", s))
                continue
            cm = _COL.match(s)
            if cm:
                cols.append((cm.group(1), cs.WS.sub(" ", cm.group(2))))
        mc = re.search(r"COLLATE=(\w+)", tail)
        tables[name] = {
            "engine": engine.upper(),
            "collate": mc.group(1) if mc else None,
            "cols": cols,
            "keys": sorted(keys),
            "raw": cs.WS.sub(" ", m.group(0)),
        }
    return tables


def norm_key(k: str) -> str:
    """索引比较用的归一化（与 check_schema 两侧保持同一形态）。"""
    return cs.WS.sub(" ", k).replace("`", "")


def _sig_want(rawdef: str) -> tuple:
    d = cs.parse_column(rawdef)
    return (d["type"], d["nullable"], cs.canon_default(d["default"]),
            d["auto_increment"], d["comment"] if INCLUDE_COMMENTS else None)


def _sig_live(d: dict) -> tuple:
    return (d["type"], d["nullable"], cs.canon_default(d["default"]),
            d["auto_increment"], d["comment"] if INCLUDE_COMMENTS else None)


def add_key_clause(raw: str) -> str:
    """把 schema.sql 里的原始索引定义改写成 ALTER 的 ADD 子句（幂等）。"""
    m = re.match(r"PRIMARY KEY\s+\((.*)\)$", raw)
    if m:
        return f"ADD PRIMARY KEY IF NOT EXISTS ({m.group(1)})"
    m = re.match(r"UNIQUE KEY\s+`?(\w+)`?\s+\((.*)\)$", raw)
    if m:
        return f"ADD UNIQUE KEY IF NOT EXISTS `{m.group(1)}` ({m.group(2)})"
    m = re.match(r"(?:KEY|INDEX)\s+`?(\w+)`?\s+\((.*)\)$", raw)
    if m:
        return f"ADD INDEX IF NOT EXISTS `{m.group(1)}` ({m.group(2)})"
    raise ValueError(f"无法改写的索引定义: {raw!r}")


def drop_key_clause(raw: str) -> str:
    """把线上独有的索引定义改写成 ALTER 的 DROP 子句（幂等）。"""
    if raw.startswith("PRIMARY KEY"):
        return "DROP PRIMARY KEY"
    m = re.match(r"(?:UNIQUE KEY|KEY)\s+(\w+)", raw)
    if not m:
        raise ValueError(f"无法改写的索引定义: {raw!r}")
    return f"DROP INDEX IF EXISTS `{m.group(1)}`"


def gen_table(t: str, live: tuple, want: dict, out: list[str]) -> None:
    _, _, lcols, lkeys = live
    w_engine, w_coll = want["engine"], want["collate"]
    wcols, wkeys = want["cols"], want["keys"]
    wkeys_norm = [norm_key(k) for k in wkeys]

    # 1) 引擎 / 排序规则
    #    无条件对齐：对已经是目标值的是 no-op（MariaDB 检测到引擎不变即跳过），
    #    这样迁移不依赖“起始状态恰好是某个已知的旧版本”，更稳。
    opts = [f"ENGINE={w_engine}"]
    if w_coll:
        opts.append(f"DEFAULT CHARACTER SET utf8mb4 COLLATE={w_coll}")
    out.append(f"ALTER TABLE `{t}` " + ", ".join(opts) + ";")

    # 2) 列：先补缺、再删多、最后按目标顺序重排 / 改定义
    lnames = [c for c, _ in lcols]
    wnames = [c for c, _ in wcols]
    lmap = dict(lcols)
    wmap = dict(wcols)

    for name, raw in wcols:
        if name not in lmap:
            out.append(f"ALTER TABLE `{t}` ADD COLUMN IF NOT EXISTS `{name}` {raw};")
    for name in lnames:
        if name not in wmap:
            out.append(f"ALTER TABLE `{t}` DROP COLUMN IF EXISTS `{name}`;")

    if lnames != wnames:
        # 顺序不一致：按目标顺序整表重排（ADD 追加在末尾，也需要重排归位）。
        prev = None
        for name, raw in wcols:
            pos = "FIRST" if prev is None else f"AFTER `{prev}`"
            out.append(f"ALTER TABLE `{t}` MODIFY COLUMN `{name}` {raw} {pos};")
            prev = name
    else:
        for name, raw in wcols:
            if _sig_live(lmap[name]) != _sig_want(raw):
                out.append(f"ALTER TABLE `{t}` MODIFY COLUMN `{name}` {raw};")

    # 3) 索引：缺失的补齐（无条件 IF NOT EXISTS，已是目标值即 no-op）；
    #    线上多出的索引删掉。
    for raw in wkeys:
        out.append(f"ALTER TABLE `{t}` {add_key_clause(raw)};")
    for raw in lkeys:
        if norm_key(raw) not in wkeys_norm:
            out.append(f"ALTER TABLE `{t}` {drop_key_clause(raw)};")


def main() -> int:
    global INCLUDE_COMMENTS
    ap = argparse.ArgumentParser(add_help=False)
    ap.add_argument("--comments", action="store_true")
    ap.add_argument("--table", action="append", default=[])
    ap.add_argument("-h", "--help", action="store_true")
    args = ap.parse_args()

    if args.help:
        print(__doc__.strip())
        return 0
    INCLUDE_COMMENTS = args.comments

    if not SCHEMA.exists():
        sys.stderr.write(f"缺少 {SCHEMA}\n")
        return 2
    want = parse_schema(SCHEMA)
    live = cs.fetch_live(cs.credentials())

    only = set(args.table)
    want = {t: v for t, v in want.items() if t not in IGNORED_TABLES and (not only or t in only)}
    live = {t: v for t, v in live.items() if t not in IGNORED_TABLES and (not only or t in only)}

    header = [
        "-- 由 extra/schema_diff.py 依据线上实际结构与 schema/schema.sql 的差异生成。",
        "-- 语句均为幂等写法，可中断、可重跑；请勿手工编辑（改动请改 schema.sql 后重新生成）。",
    ]
    if not INCLUDE_COMMENTS:
        header.append("-- 未包含列注释的对齐（注释是文档，不参与结构语义；需要时加 --comments）。")
    out = ["SET NAMES utf8mb4;", ""]

    # 缺失表
    missing = sorted(set(want) - set(live))
    if missing:
        out.append("-- ── 线上缺失的表 ──")
        for t in missing:
            block = re.sub(r"^CREATE TABLE ", "CREATE TABLE IF NOT EXISTS ",
                           want[t]["raw"], count=1)
            out.append(block)
        out.append("")

    # 线上多出的表：只提示，不删（删表会丢数据，应由人工决策）
    extra = sorted(set(live) - set(want))
    if extra:
        out.append("-- ── 线上多出的表（本脚本不删，请人工确认）──")
        for t in extra:
            out.append(f"-- ALTER TABLE `{t}` ... 或 DROP TABLE `{t}`;")
        out.append("")

    for t in sorted(set(live) & set(want)):
        before = len(out)
        gen_table(t, live[t], want[t], out)
        if len(out) > before:
            out.append("")

    body = "\n".join(out).rstrip() + "\n"
    if body.strip() in ("SET NAMES utf8mb4;",):
        sys.stderr.write("线上结构与 schema/schema.sql 已一致，无需迁移。\n")
        return 0
    print("\n".join(header) + "\n")
    print(body, end="")
    return 0


if __name__ == "__main__":
    sys.exit(main())
