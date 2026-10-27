#!/usr/bin/env python3
"""比对生产库实际结构与 schema/schema.sql 的权威定义。

背景
----
schema/schema.sql 由 mysqldump 机械导出（见该文件头部的说明）。手写 DDL
曾在第一次尝试中产生 48 处偏差（列名、类型、索引、引擎全都有错），所以
"schema 文件与线上一致"这件事不能靠肉眼，必须有机器检查。

比较方式
--------
两侧都解析成**结构化元组**再比，而不是拿 SQL 文本做字符串比较。原因是两侧
天然存在写法差异，直接比文本会产生大量假阳性：

    mysqldump:  `title` varchar(255) DEFAULT NULL
    information_schema: IS_NULLABLE=YES, COLUMN_DEFAULT=NULL
    mysqldump:  `coin` int(10) unsigned NOT NULL DEFAULT 1 COMMENT '...'
    information_schema: COLUMN_TYPE=int(10) unsigned, EXTRA='', COLUMN_COMMENT='...'

第一版就是直接比字符串，121 条全部是这类噪声。归一化后应只剩真实差异。

比对项
------
表名集合、列名与列序、列类型（含 unsigned 与显示宽度）、可空性、默认值、
AUTO_INCREMENT、列注释、主键、索引（含唯一性与列序）、存储引擎、排序规则。

退出码
------
0 = 完全一致；1 = 存在差异（逐项打印）；2 = 无法连接或读取失败。

用法
----
    python3 extra/check_schema.py
    python3 extra/check_schema.py --ignore-comments     # 存量库迁移后复核结构用
    DB_HOST=... DB_USER=... DB_PASS=... DB_NAME=... python3 extra/check_schema.py

`--ignore-comments` 跳过列注释比对：注释是文档、不影响行为，老版 HUSTOJ 库
与 schema.sql 的注释普遍不同，这正是 `extra/migrate.sh` 迁移后建议的复核口径。
"""

from __future__ import annotations

import os
import re
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
SCHEMA = ROOT / "schema" / "schema.sql"
DB_INFO = ROOT / "phpweb" / "include" / "db_info.inc.php"

WS = re.compile(r"\s+")

# 不计入结构比对的表：
#   schema_migrations —— extra/migrate.sh 的版本台账（应用基础设施，不是业务结构，
#   只存在于跑过迁移的存量库，schema.sql 里刻意没有它）。
IGNORED_TABLES = {"schema_migrations"}

# --ignore-comments：跳过列注释比对。注释是文档、不影响行为；老版 HUSTOJ 库
# 的列注释与 schema.sql 普遍不同，迁移后用它复核结构即可（migrate.sh 文档同此口径）。
IGNORE_COMMENTS = "--ignore-comments" in sys.argv


def _canon_type(t: str) -> str:
    """类型归一化：统一小写与空白，保留 unsigned 与显示宽度。

    显示宽度（int(11)）必须保留并参与比较 —— 它虽已弃用，但线上与旧版
    judged-go 的 C 代码按该宽度解析，属于真实语义的一部分。
    """
    return WS.sub(" ", t.strip().lower())


def canon_default(d) -> str:
    """默认值归一化。

    MariaDB 有个反直觉行为：对可空的 TEXT/BLOB 列，
    COLUMN_DEFAULT 返回**字面字符串** 'NULL'，而不是 SQL NULL，
    即使 DDL 里写的是 DEFAULT NULL 或根本没写。
    三者语义相同，统一成 "NULL"。
    """
    if d is None:
        return "NULL"
    d = d.strip()
    return "NULL" if d.upper() == "NULL" else d


def parse_column(defn: str) -> dict:
    """把一条 DDL 列定义解析成结构化元组。

    覆盖 HustOJ 实际用到的子集：
        类型 [unsigned] [NOT NULL|NULL] [DEFAULT 字面量] [AUTO_INCREMENT] [COMMENT '文本']
    """
    s = WS.sub(" ", defn.strip())
    out: dict = {"type": "", "nullable": True, "default": None,
                 "auto_increment": False, "comment": ""}

    # COMMENT 最容易含空格与转义单引号，优先摘出来
    mc = re.search(r"\bCOMMENT\s+'((?:[^'\\]|\\.|'')*)'", s, re.I)
    if mc:
        out["comment"] = mc.group(1).replace("''", "'")
        s = s[: mc.start()] + s[mc.end():]

    if re.search(r"\bAUTO_INCREMENT\b", s, re.I):
        out["auto_increment"] = True
        s = re.sub(r"\bAUTO_INCREMENT\b", "", s, flags=re.I)

    if re.search(r"\bNOT\s+NULL\b", s, re.I):
        out["nullable"] = False
    s = re.sub(r"\bNOT\s+NULL\b", "", s, flags=re.I)

    # 顺序很关键：必须先摘掉 DEFAULT，再剥 NULL。
    # 反过来做的话，"varchar(255) DEFAULT NULL" 会先被剥成
    # "varchar(255) DEFAULT "，DEFAULT 正则要求后面至少一个字符就匹配不上，
    # 结果 "default" 残留在 type 里，同时 default 丢成 None。
    md = re.search(r"\bDEFAULT\s+(\S.*?)\s*(?=$|ON\s+UPDATE|COMMENT)", s, re.I)
    if md:
        out["default"] = md.group(1).strip()
        s = s[: md.start()] + s[md.end():]

    # 显式 NULL 与"省略即默认可空"等价，归一化掉这个差异
    s = re.sub(r"(?<![A-Z])NULL(?![A-Z(])", "", s, flags=re.I)

    s = re.sub(r"\bCOLLATE\b.*$", "", s, flags=re.I).strip()
    s = re.sub(r"\bCHARACTER\s+SET\b.*$", "", s, flags=re.I).strip()
    out["type"] = _canon_type(s)
    return out


def parse_schema_ddl(sql: str) -> dict:
    """从 CREATE TABLE 语句解析出 {表: (引擎, collation, [(列名, 元组)], [索引])}。"""
    tables: dict = {}
    pattern = (r"CREATE TABLE `(\w+)`\s*\((.*?)\n\)\s*"
               r"ENGINE=(\w+)([^;]*);")
    for m in re.finditer(pattern, sql, re.S):
        name, body, engine, tail = m.groups()
        cols: list[tuple[str, dict]] = []
        keys: list[str] = []
        for raw in body.split("\n"):
            line = raw.strip().rstrip(",")
            if not line or line.startswith("--"):
                continue
            if line.startswith(("PRIMARY KEY", "UNIQUE KEY", "KEY ")):
                keys.append(WS.sub(" ", line).replace("`", ""))
                continue
            cm = re.match(r"`(\w+)`\s+(.+)$", line)
            if cm:
                cols.append((cm.group(1), parse_column(cm.group(2))))
        mc = re.search(r"COLLATE=(\w+)", tail)
        tables[name] = (engine.upper(), mc.group(1) if mc else None, cols, sorted(keys))
    return tables


def credentials() -> dict:
    if os.environ.get("DB_NAME"):
        return {k: os.environ[k] for k in ("DB_HOST", "DB_USER", "DB_PASS", "DB_NAME")}
    # 复用 PHP 配置里的凭据，避免在 CI 里再抄一份。
    # 分隔符用 %c(9)：PHP 单引号里的 \t 是字面反斜杠+t，printf 也不会把它
    # 解释成制表符（实测输出 "localhost\thustoj"），split 会因此只得到 1 段。
    path = str(DB_INFO).replace("'", "\\'")
    php = ("require '%s'; "
           'printf("%%s%%c%%s%%c%%s%%c%%s", '
           "$DB_HOST,9,$DB_USER,9,$DB_PASS,9,$DB_NAME);" % path)
    out = subprocess.run(["php", "-r", php], capture_output=True, text=True,
                         cwd=str(ROOT / "phpweb"))
    if out.returncode != 0:
        sys.stderr.write("无法从 db_info.inc.php 读取数据库凭据:\n" + out.stderr + "\n")
        sys.exit(2)
    parts = out.stdout.strip().split("\t")
    if len(parts) != 4:
        sys.stderr.write("凭据格式异常（期望 4 段，实际 %d 段）: %r\n"
                         % (len(parts), out.stdout))
        sys.exit(2)
    return dict(zip(("DB_HOST", "DB_USER", "DB_PASS", "DB_NAME"), parts))


def fetch_live(env: dict) -> dict:
    """从 information_schema 读取线上实际结构。

    走 information_schema 而非 mysqldump：CI 容器里未必有 mysqldump，
    且检查结构不该依赖导出整库。三段结果用哨兵首列区分。
    """
    sql = f"""
    SELECT 'T', TABLE_NAME, IFNULL(ENGINE,''), IFNULL(TABLE_COLLATION,'')
      FROM information_schema.TABLES
     WHERE TABLE_SCHEMA = '{env["DB_NAME"]}' AND TABLE_TYPE = 'BASE TABLE'
     ORDER BY TABLE_NAME;
    SELECT 'C', TABLE_NAME, COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE,
           IFNULL(COLUMN_DEFAULT,'__HUSTOJ_NULL__'), EXTRA, COLUMN_COMMENT, ORDINAL_POSITION
      FROM information_schema.COLUMNS
     WHERE TABLE_SCHEMA = '{env["DB_NAME"]}'
     ORDER BY TABLE_NAME, ORDINAL_POSITION;
    SELECT 'I', TABLE_NAME, INDEX_NAME, SEQ_IN_INDEX, COLUMN_NAME, NON_UNIQUE
      FROM information_schema.STATISTICS
     WHERE TABLE_SCHEMA = '{env["DB_NAME"]}'
     ORDER BY TABLE_NAME, INDEX_NAME, SEQ_IN_INDEX;
    """
    r = subprocess.run(
        ["mysql", "--batch", "--raw", "--skip-column-names",
         f"-h{env['DB_HOST']}", f"-u{env['DB_USER']}", f"-p{env['DB_PASS']}",
         env["DB_NAME"], "-e", sql],
        capture_output=True, text=True,
    )
    if r.returncode != 0:
        sys.stderr.write("无法读取数据库结构:\n" + r.stderr + "\n")
        sys.exit(2)

    engines: dict[str, str] = {}
    collations: dict[str, str] = {}
    cols: dict[str, list] = {}
    idx: dict[str, dict] = {}

    for line in r.stdout.splitlines():
        p = line.split("\t")
        if p[0] == "T":
            engines[p[1]] = p[2].upper()
            collations[p[1]] = p[3]
        elif p[0] == "C":
            _, tbl, col, ctype, nullable, dflt, extra, comment, _pos = p
            cols.setdefault(tbl, []).append((col, {
                "type": _canon_type(ctype),
                "nullable": nullable == "YES",
                "default": None if dflt == "__HUSTOJ_NULL__" else dflt,
                "auto_increment": "auto_increment" in extra.lower(),
                "comment": comment,
            }))
        elif p[0] == "I":
            _, tbl, iname, seq, col, nonuniq = p
            e = idx.setdefault(tbl, {}).setdefault(
                iname, {"cols": [], "unique": nonuniq == "0"})
            e["cols"].append((int(seq), col))

    tables: dict = {}
    for t, eng in engines.items():
        keys: list[str] = []
        for iname, e in idx.get(t, {}).items():
            joined = ",".join(c for _, c in sorted(e["cols"]))
            if iname == "PRIMARY":
                keys.append(f"PRIMARY KEY ({joined})")
            elif e["unique"]:
                keys.append(f"UNIQUE KEY {iname} ({joined})")
            else:
                keys.append(f"KEY {iname} ({joined})")
        tables[t] = (eng, collations[t], cols.get(t, []), sorted(keys))
    return tables


def main() -> int:
    if not SCHEMA.exists():
        sys.stderr.write(f"缺少 {SCHEMA}\n")
        return 2

    schema_sql = SCHEMA.read_text(encoding="utf-8")
    want = parse_schema_ddl(schema_sql)
    live = fetch_live(credentials())
    want = {t: v for t, v in want.items() if t not in IGNORED_TABLES}
    live = {t: v for t, v in live.items() if t not in IGNORED_TABLES}

    problems: list[str] = []
    for t in sorted(set(live) - set(want)):
        problems.append(f"线上多出表（schema.sql 缺失）: {t}")
    for t in sorted(set(want) - set(live)):
        problems.append(f"schema.sql 多出表（线上不存在）: {t}")

    for t in sorted(set(live) & set(want)):
        le, lcol, lcols, lkeys = live[t]
        we, wcol, wcols, wkeys = want[t]
        if le != we:
            problems.append(f"{t}: 引擎 线上={le} schema={we}")
        if lcol != wcol:
            problems.append(f"{t}: collation 线上={lcol} schema={wcol}")

        lnames = [c for c, _ in lcols]
        wnames = [c for c, _ in wcols]
        if lnames != wnames:
            miss = [c for c in wnames if c not in lnames]
            extra = [c for c in lnames if c not in wnames]
            if miss:
                problems.append(f"{t}: schema 有而线上缺的列: {miss}")
            if extra:
                problems.append(f"{t}: 线上有而 schema 缺的列: {extra}")
            if not miss and not extra:
                problems.append(f"{t}: 列顺序不一致\n      线上: {lnames}\n"
                                f"      schema: {wnames}")

        lmap = dict(lcols)
        for col, wdef in wcols:
            if col not in lmap:
                continue
            ldef = lmap[col]
            fields = ["type", "nullable", "auto_increment"]
            if not IGNORE_COMMENTS:
                fields.append("comment")
            for field in fields:
                if ldef[field] != wdef[field]:
                    problems.append(
                        f"{t}.{col}.{field} 不一致\n"
                        f"      线上: {ldef[field]!r}\n      schema: {wdef[field]!r}")
            if canon_default(ldef["default"]) != canon_default(wdef["default"]):
                problems.append(
                    f"{t}.{col}.default 不一致\n"
                    f"      线上: {ldef['default']!r}\n      schema: {wdef['default']!r}")

        if lkeys != wkeys:
            only_l = [k for k in lkeys if k not in wkeys]
            only_w = [k for k in wkeys if k not in lkeys]
            if only_l:
                problems.append(f"{t}: 线上独有的索引: {only_l}")
            if only_w:
                problems.append(f"{t}: schema 独有的索引: {only_w}")

    if problems:
        print("=== schema 一致性校验失败 ===")
        for p in problems:
            print("  ✗ " + p)
        print(f"\n共 {len(problems)} 处差异。")
        print("生产库结构必须与 schema/schema.sql 一致；如线上已变更，")
        print("请重新从 mysqldump 导出该文件，不要手工编辑。")
        return 1

    ncols = sum(len(c[2]) for c in live.values())
    print("=== schema 一致性校验 ===")
    print(f"  ✓ {len(want)} 张表 / {ncols} 列与生产库逐项一致")
    print("    （列名/列序/类型含 unsigned 与显示宽度/可空/默认/AUTO_INCREMENT/"
          "注释/主键/索引/引擎/collation）")
    return 0


if __name__ == "__main__":
    sys.exit(main())
