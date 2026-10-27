# AGENTS.md

本文件是协作/自动化代理（含 opencode 等）在本仓库工作时的约定。

## 提交信息（commit message）

- **禁止**在提交信息里添加任何 AI/工具署名尾注，例如
  `Co-authored-by: CommandCodeBot <noreply@commandcode.ai>`，
  以及其它由工具自动生成的 `Co-authored-by` / `Generated-by` /
  `Co-authored-with` 之类行。
- 提交信息只描述改动本身：`ADD:` / `MOD:` / `FIX:` / `REMOVE:` 前缀 + 要点正文。
- 作者与提交者身份由 `git config user.name/user.email` 决定，
  不要手动改写，也不要追加协作者署名。
