# 子任务评分规则文档

## 概述

本包实现了 OJ（Online Judge）系统的两种评分模式：普通模式和 OI 模式。主要处理子任务评分逻辑，支持部分得分。

## 核心概念

### 测试点（Test Point）

每个测试文件称为一个测试点，包含以下属性：
- **Filename**: 测试文件名，可包含分数信息（如 `test[10].in` 表示该测试点 10 分）
- **Score**: 测试点分值
- **Result**: 测试结果（AC、WA、PE 等）
- **SpjMark**: 特殊评测得分率（0-1 之间），用于部分正确的情况

### 子任务（Subtask）

多个测试点组成一个子任务，子任务名称通过文件名主编号识别。

#### 子任务判定规则

**核心原则**：取主编号（第一个下划线前的部分）进行比较，主编号相同的属于同一子任务

**主编号提取**：
- 取文件名中第一个点号（`.`）前的部分（前缀）
- 在前缀中，取第一个下划线（`_`）前的部分作为主编号
- 如果无前缀或主编号，则使用完整前缀

**判定函数**：`SameSubtask(last, cur)` 返回是否属于同一子任务

#### 示例说明

**同一子任务** ✅：
- `1_1.in` 和 `1_2.in` → 主编号都是 `1`，同一子任务
- `1_1.in` 和 `1_1.in` → 主编号都是 `1`，同一子任务
- `1.in` 和 `1_2.in` → 主编号都是 `1`，同一子任务（`1` 没有下划线，`1_2` 的主编号也是 `1`）
- `2_1.in` 和 `2_2.in` → 主编号都是 `2`，同一子任务
- `a_1.in` 和 `a_2.in` → 主编号都是 `a`，同一子任务
- `group1_1.in` 和 `group1_2.in` → 主编号都是 `group1`，同一子任务

**不同子任务** ❌：
- `1_1.in` 和 `2_1.in` → 主编号 `1` ≠ `2`，不同子任务
- `1_1.in` 和 `3_1.in` → 主编号 `1` ≠ `3`，不同子任务
- `a_1.in` 和 `b_1.in` → 主编号 `a` ≠ `b`，不同子任务
- `test1.in` 和 `test2.in` → 主编号 `test1` ≠ `test2`，不同子任务

**文件名格式建议**：
- 推荐使用格式：`主编号[_次编号].in`（如 `1_1.in`, `1_2.in`）
- 主测试点：`主编号.in`（如 `1.in` 可作为子任务 1 的主测试点）
- 复杂格式：`group1_1.in`、`contest1_a.in` 等也支持

#### 子任务评分规则

同一子任务的测试点必须**全部通过**，才能获得该子任务的全部分数：

1. **全部通过**：获得该子任务所有测试点的分数之和
2. **任意测试点未通过**：该子任务得分为 0，即使其他测试点通过

**示例**：

```go
// 子任务 1（测试点 1 和 1_1）
results := []TestResult{
    {Filename: "1.in[10]", Score: 10, Result: OJ_AC, SpjMark: 0},     // 通过
    {Filename: "1_1.in[10]", Score: 10, Result: OJ_WA, SpjMark: 0},  // 未通过
}
// 结果：子任务1得 0 分（因为 1_1 未通过，回退 1 的分数）

// 子任务 2（测试点 2 和 2_1）
results := []TestResult{
    {Filename: "2.in[10]", Score: 10, Result: OJ_AC, SpjMark: 0},     // 通过
    {Filename: "2_1.in[10]", Score: 10, Result: OJ_AC, SpjMark: 0},    // 通过
}
// 结果：子任务2得 20 分（两个测试点都通过）
```

#### OI 模式 vs 传统子任务

OI 模式的子任务与传统意义上的子任务有所不同：

| 特性 | OI 模式（本子任务包） | 传统子任务 |
|------|---------------------|----------|
| 子任务划分 | 按文件名前缀严格匹配 | 按同一数字前缀 |
| `1` 和 `1_1` | 不同子任务 | 同一子任务 |
| 失败回退 | 回退同一子任务通过的分数 | 通常不回退 |
| 灵活性 | 更高，支持多种命名方式 | 固定格式 |

这种设计允许更灵活的测试文件命名，适用于各种类型的竞赛和评测场景。

## 评分模式

### 1. 普通模式（Normal Mode）

**规则**：所有测试点都必须通过（AC 或 PE），整个任务才算通过。

**得分计算**：
- 全部测试点通过：得满分（100 分）
- 任意测试点未通过：得 0 分
- 最终结果取最差的一个测试点结果

**适用场景**：传统 OJ 评测，要求输出必须完全符合预期。

### 2. OI 模式（OI Mode）

**规则**：采用子任务制评分。

**子任务规则**：
1. 每个子任务包含多个测试点
2. 同一子任务内，只有所有测试点都通过，才能获得该子任务的全部分数
3. 如果子任务内有任意测试点未通过，则该子任务得分为 0
4. 支持部分得分（通过 SpjMark 实现）

**特殊评测（SpjMark）**：
- SpjMark ∈ [0, 1]，表示该测试点的得分比例
- 例如：SpjMark = 0.5 表示即使结果是 WA，也能获得该测试点 50% 的分数
- SpjMark 只影响得分，不影响最终结果判定

**最终结果判定**：
- 只要有任意测试点结果为 WA（或更差），FinalResult = WA
- SpjMark > 0 不能改变最终结果

**适用场景**：OI 竞赛、需要部分得分的场景。

## 数据结构

### TestResult

```go
type TestResult struct {
    Filename string  // 测试文件名
    Score    float64 // 该测试点的分值
    Result   int     // 评测结果（使用 constants.OJ_AC, constants.OJ_WA 等常量）
    SpjMark  float64 // 自定义评分（0-1之间，用于部分正确）
}
```

### SubtaskScore

```go
type SubtaskScore struct {
    GetMark     float64 // 实际得分
    TotalMark   float64 // 总分
    PassRate    float64 // 通过率（0-1）
    FinalResult int     // 最终结果（使用 constants.OJ_AC, constants.OJ_WA 等常量）
}
```

## 核心函数

### Judge(results []TestResult, oiMode bool) SubtaskScore

根据评分模式计算最终得分。

**参数**：
- `results`: 测试点结果列表
- `oiMode`: `true` = OI 模式，`false` = 普通模式

**返回**：
- `SubtaskScore` 结构体，包含得分、总分、通过率和最终结果

### CalculateOIScore(results []TestResult) SubtaskScore

计算 OI 模式下的得分。

**算法流程**：
1. 初始化总得分为 0
2. 遍历所有测试点：
   - 累加总分
   - 更新最终结果（取最差结果）
   - 如果测试通过（AC/PE）：
     - 累加该测试点分数
     - 通过率 +1
   - 如果测试未通过：
     - 支持部分得分（SpjMark × 分值）
     - 通过率增加 SpjMark
     - 如果是同一子任务的后续失败测试，回退之前添加的分数
3. 计算最终通过率
4. 返回得分结构体

### CalculateNormalScore(results []TestResult) SubtaskScore

计算普通模式下的得分。

**算法流程**：
1. 检查是否所有测试点都通过（AC 或 PE）
2. 如果全部通过：得分为 100，最终结果为 AC
3. 如果有测试点未通过：得分为 0，最终结果为第一个未通过的测试点结果

## 文件名解析

### ExtractScoreFromFilename(filename string) float64

从文件名中提取分数信息。

**格式**：`name[score].ext`
- 例如：`test[10].in` → 10
- 例如：`1.in[20]` → 20
- 如果未指定分数，默认返回 10

### GetSubtaskPrefix(filename string) string

获取子任务前缀（文件名中第一个点号前的部分）。

**示例**：
- `1.in` → `1`
- `1_1.in` → `1_1`
- `test.in` → `test`

### SameSubtask(last, cur string) bool

判断两个文件名是否属于同一个子任务。

**规则**：
- 只有前缀完全相同的文件名才属于同一子任务
- `1.in` 和 `1_1.in` → **不同子任务**
- `1_1.in` 和 `1_2.in` → **不同子任务**
- `1.in` 和 `1.in` → **同一子任务**

## 使用示例

### OI 模式示例

```go
results := []TestResult{
    {Filename: "1.in[10]", Score: 10, Result: constants.OJ_AC, SpjMark: 0},
    {Filename: "1_1.in[10]", Score: 10, Result: constants.OJ_AC, SpjMark: 0},
    {Filename: "2.in[10]", Score: 10, Result: constants.OJ_WA, SpjMark: 0.5},
}

score := subtask.Judge(results, true) // OI 模式
// 得分：15（子任务1得20分，子任务2得5分）
// 最终结果：WA
```

### 普通模式示例

```go
results := []TestResult{
    {Filename: "1.in", Score: 10, Result: constants.OJ_AC, SpjMark: 0},
    {Filename: "2.in", Score: 10, Result: constants.OJ_WA, SpjMark: 0},
}

score := subtask.Judge(results, false) // 普通模式
// 得分：0（任意测试点未通过，得0分）
// 最终结果：WA
```

### 全部通过示例

```go
results := []TestResult{
    {Filename: "1.in[10]", Score: 10, Result: constants.OJ_AC, SpjMark: 0},
    {Filename: "1_1.in[10]", Score: 10, Result: constants.OJ_AC, SpjMark: 0},
    {Filename: "2.in[10]", Score: 10, Result: constants.OJ_AC, SpjMark: 0},
    {Filename: "2_1.in[10]", Score: 10, Result: constants.OJ_AC, SpjMark: 0},
}

score := subtask.Judge(results, true) // OI 模式
// 得分：40（所有子任务全部通过）
// 最终结果：AC
```

## 注意事项

1. **SpjMark 的优先级**：SpjMark 只影响得分，不影响最终结果判定
2. **子任务划分**：只有文件名前缀完全相同的测试点才属于同一子任务
3. **PE 处理**：表示格式错误（Presentation Error），在普通模式中不影响通过率
4. **结果常量**：使用 `pkg/constants` 包中定义的常量（如 `constants.OJ_AC`、`constants.OJ_WA` 等）
5. **并发安全**：本包无全局状态，所有函数都可在并发环境中安全调用

## 相关文件

- `subtask.go` - 核心实现代码
- `subtask_test.go` - 单元测试
- `../constants/constants.go` - 评测结果常量定义

## 版本历史

- 2024-01 - 初始版本实现
- 2024-03 - 重构：移除重复常量定义，统一使用 pkg/constants
