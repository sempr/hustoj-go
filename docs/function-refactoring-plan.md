# HUSTOJ-Go 代码重构计划

**版本**: 1.0
**日期**: 2024-01-20
**目标**: 优化长函数，提升代码可维护性和可读性

---

## 1. 项目概述

### 1.1 项目背景
HUSTOJ-Go 是一个在线判题系统，用于自动评判编程作业和竞赛题目。系统由多个模块组成，包括判题客户端(JudgeClient)、守护进程(Daemon)、沙箱(Sandbox)等核心组件。

### 1.2 当前问题
通过代码分析发现，项目中存在多个超过50行的长函数，部分函数达到100+行。这些长函数具有以下特点：
- **职责不清**: 一个函数承担多个业务职责
- **嵌套过深**: 多重if/else嵌套，逻辑难以跟踪
- **可读性差**: 代码行数过多，难以理解业务流程
- **测试困难**: 难以编写针对性的单元测试
- **维护成本高**: 修改一处可能影响多处业务逻辑

### 1.3 重构必要性
长函数在项目的重构负债，导致：
- 新人上手困难，代码理解成本高
- Bug修复和定位效率低
- 代码复用性差，相同逻辑可能重复编写
- 难以进行单元测试，测试覆盖率低
- 代码评审效率低，难以发现潜在问题

---

## 2. 重构目标

### 2.1 总体目标
通过重构长函数，将复杂业务逻辑拆分为独立、可复用的小函数，提升代码的可读性、可维护性和可测试性。

### 2.2 具体目标
1. **降低函数复杂度**: 所有函数控制在50行以内，理想为20-30行
2. **明确职责划分**: 每个函数只负责一个具体的业务职责
3. **提升可读性**: 函数命名清晰，逻辑流程直观易懂
4. **提高可测试性**: 每个子函数都可以独立测试
5. **减少代码重复**: 提取公共逻辑，避免重复代码
6. **不影响功能**: 重构前后功能完全一致，不引入新Bug
7. **性能不下降**: 重构后性能不降低（或提升）

### 2.3 可量化指标
| 指标 | 重构前 | 重构后目标 |
|------|--------|------------|
| 平均函数行数 | >80行 | <30行 |
| 最长函数行数 | 117行 | <50行 |
| 函数平均复杂度(圈复杂度) | >15 | <5 |
| 单元测试覆盖率 | - | >70% |
| Bug密度 | - | 降低50% |

---

## 3. 重构范围

### 3.1 受影响的文件
- `/internal/client/run.go` (368行)
- `/internal/client/executor.go` (249行)
- `/internal/client/file_compare.go` (259行)
- `/internal/sandbox/sandbox.go` (163行)

### 3.2 重构的函数列表

#### 3.2.1 最高优先级 (P0) - ✅ 已完成
| 文件路径 | 函数名 | 原始行数 | 重构状态 | 重构方式 |
|----------|--------|----------|----------|----------|
| `internal/sandbox/sandbox.go` | `runParent()` | 117行 | ✅ 已完成 | 提取为 SandboxController 结构体，拆分初始化、执行、监控、清理等职责 |
| `internal/client/run.go` | `Run()` | 87行 | ✅ 已完成 | 提取prepareJudgeContext、setupEnvironment、handleCompilation、handleExecution等子函数 |
| `internal/client/run.go` | `runTestCases()` | 114行 | ✅ 已完成 | 拆分为prepareTestContext、executeAllTestCases、executeSingleTestCase、processTestResults等子函数 |

#### 3.2.2 高优先级 (P1)
| 文件路径 | 函数名 | 当前行数 | 主要问题 | 重构策略 |
|----------|--------|----------|----------|----------|
| `internal/client/executor.go` | `runAndCompare()` | 111行 | 逻辑链路过长,错误处理嵌套 | 提取构建命令、执行、结果处理等子函数 |
| `internal/client/run.go` | `Run()` | 87行 | 职责过多,业务流程不清晰 | 提取初始化、编译、测试、清理等阶段 |
| `internal/client/executor.go` | `handleSpecialJudge()` | 79行 | SPJ逻辑冗长,可读性差 | 提取SPJ命令构建、执行、结果处理 |

#### 3.2.3 中优先级 (P2)
| 文件路径 | 函数名 | 当前行数 | 主要问题 | 重构策略 |
|----------|--------|----------|----------|----------|
| `internal/client/file_compare.go` | `compareFilesByStream()` | 68行 | 流处理复杂,逻辑重复 | 抽取过滤函数,重构循环逻辑 |

### 3.3 不涉及的范围
- 不改变现有业务逻辑和算法
- 不修改数据存储结构
- 不影响对外接口
- 不引入新的技术栈或框架

---

## 4. 重构策略

### 4.1 核心重构方法

#### 方法1: 抽取方法 (Extract Method)
**适用场景**: 所有长函数
**做法**:
1. 识别函数中的独立业务逻辑块
2. 提炼公共逻辑片段
3. 为新函数命名（函数名即为注释）
4. 保持单一职责原则

**示例**:
```go
// Before: 函数内部包含多个职责
func Run() error {
    // 获取Solution
    solution, err := jc.db.GetSolution(jc.solutionID)
    if err != nil {
        return fmt.Errorf("failed: %w", err)
    }

    // 获取Problem
    problem, err := jc.db.GetProblem(solution.ProblemID)
    if err != nil {
        return fmt.Errorf("failed: %w", err)
    }

    // 获取语言配置
    ...
}

// After: 抽取为子函数
func Run() error {
    setupResult, err := jc.prepareJudge() // 抽取的函数
    if err != nil {
        return err
    }

    // 后续流程...
}

func (jc *JudgeClient) prepareJudge() (*JudgeSetup, error) {
    // 获取Solution
    // 获取Problem
    // 获取语言配置
    return setupResult, nil
}
```

#### 方法2: 引入参数对象 (Introduce Parameter Object)
**适用场景**: `runTestCases()`、`runAndCompare()` 等参数过多的函数
**做法**:
1. 识别经常一起传递的参数组
2. 创建结构体封装这些参数
3. 简化函数签名
4. 便于扩展和维护

**示例**:
```go
// Before: 参数列表过长
func runTestCases(solution *Solution, problem *Problem, rootfs string,
                  langConfig *LangConfig, spjProgram int) error

// After: 使用参数对象
type TestCaseContext struct {
    Solution   *Solution
    Problem    *Problem
    Rootfs     string
    LangConfig *LangConfig
    SpjProgram int
}

func runTestCases(ctx *TestCaseContext) error
```

#### 方法3: 状态模式 (State Pattern)
**适用场景**: `runParent()` 中的复杂状态管理
**做法**:
1. 识别不同状态（初始化、运行中、超时、完成等）
2. 创建状态接口
3. 每个状态实现为独立结构体
4. 使用状态机管理执行流程

#### 方法4: 策略模式 (Strategy Pattern)
**适用场景**: `handleSpecialJudge()` 中不同类型的SPJ处理
**做法**:
1. 定义SPJ处理接口
2. 实现具体SPJ处理器（SPJ、TPJ、UPJ）
3. 工厂模式创建处理器
4. 运行时动态选择策略

### 4.2 具体重构步骤 (以 runParent 为例)

#### 步骤1: 分析现有代码
- 识别代码中的独立职责：
  - 初始化设置
  - Tracer启动和监控
  - CPU时间检查
  - 信号处理
  - 资源清理
  - 结果构建

#### 步骤2: 抽取初始化逻辑
```go
func runParent(cfg *models.SandboxArgs) (*models.SandboxOutput, error) {
    // 步骤1: 抽取初始化配置
    initResult, err := jc.initializeSandbox(cfg)
    if err != nil {
        return nil, err
    }

    // 步骤2: 执行沙箱监控
    traceResult, err := jc.runTraceLoop(initResult)

    // 步骤3: 构建输出
    output := jc.buildOutput(traceResult, initResult)
    return output, err
}
```

#### 步骤3: 抽取并发控制逻辑
```go
func (jc *SandboxController) runTraceLoop(init *SandboxInit) *TraceResult {
    // 启动tracer
    tracerReady := make(chan bool)
    tracerDone := make(chan TraceResult, 1)

    go jc.runTracer(init, tracerReady, tracerDone)

    // 启动CPU检查器
    checkerDone := jc.startCPUMonitor(init)

    // 等待完成或超时
    select {
    case <-checkerDone:
        jc.handleTimeout()
    case result := <-tracerDone:
        return result
    }
}
```

#### 步骤4: 抽取资源清理
```go
func (jc *SandboxController) cleanupResources(paths []string) {
    defer func() {
        cleanupCgroup(cgroupPath)
        cleanupFiles(tempFiles)
    }()
}
```

### 4.3 其他函数重构策略

#### `Run()` 函数
**问题**: 整个判题流程耦合在一起，包括初始化、编译、测试、清理
**策略**:
1. 抽取 `prepareJudgeContext()` - 准备所有判题所需数据
2. 抽取 `executeCompilation()` - 执行编译逻辑
3. 抽取 `handleCompilationError()` - 处理编译错误
4. 抽取 `executeJudge()` - 执行判题流程
5. 保持 `Run()` 函数为高层编排

#### `runTestCases()` 函数
**问题**: 测试用例执行、结果收集、分数计算、SPJ处理混杂在一起
**策略**:
1. 抽取 `prepareTestContext()` - 准备测试环境配置
2. 抽取 `executeSingleTestCase()` - 执行单个测试用例
3. 抽取 `collectTestResults()` - 收集所有测试结果
4. 抽取 `calculateFinalScore()` - 计算最终得分
5. 抽取 `updateDatabaseWithResults()` - 更新数据库

#### `runAndCompare()` 函数
**问题**: 命令构建、文件复制、执行、结果比较都在一个函数
**策略**:
1. 抽取 `setupRunEnvironment()` - 设置运行环境
2. 抽取 `buildSandboxCommand()` - 构建沙箱命令
3. 抽取 `executeSandbox()` - 执行沙箱命令
4. 抽取 `compareOutputFiles()` - 比较输出文件
5. 在父函数中组装这些步骤

---

## 5. 实施步骤

### 5.1 准备阶段 (1-2天)

#### 任务1.1: 代码分析
- [ ] 使用静态分析工具(golangci-lint)扫描代码质量
- [ ] 生成功能调用图，了解函数间依赖
- [ ] 标记所有需要重构的函数
- [ ] 评估测试覆盖情况

#### 任务1.2: 建立测试基础
- [ ] 为所有需要重构的函数编写单元测试（测试当前行为）
- [ ] 运行现有集成测试，确保覆盖率
- [ ] 建立性能基准测试
- [ ] 准备测试数据集

#### 任务1.3: 建立安全机制
- [ ] 创建功能分支 `refactor/long-functions`
- [ ] 配置CI/CD流水线，确保每次提交都运行测试
- [ ] 准备代码审查清单
- [ ] 建立代码质量门禁

### 5.2 重构阶段 (7-10天)

#### 阶段2.1: P0级别重构 (3-4天) - ✅ 已完成
**目标**: 重构 `runParent()` 和 `runTestCases()` - 2025-03-20

##### ✅ `runParent()` 完整重构
- ✅ 抽取 `SandboxController` 结构体，封装所有状态
- ✅ 抽取 `init()` 方法：初始化 cgroup限制、内存限制等配置
- ✅ 抽取 `execute()` 方法：主执行流程
- ✅ 抽取 `waitForCompletion()` 方法：并发流程协调
- ✅ 抽取 `cleanupResources()` 方法：资源清理
- ✅ 抽取 `buildOutput()` 方法：构建输出结果
- ✅ 抽取 `processErrorResult()` 方法：错误结果处理
- ✅ 删除旧版 `runParent` 冗余代码
- ✅ 代码审查通过，编译验证通过

##### ✅ `Run()` 方法重构（client）
- ✅ 抽取 `JudgeContext` 结构体，封装判题上下文
- ✅ 抽取 `prepareJudgeContext()` 方法：初始化判题信息
- ✅ 抽取 `setupEnvironment()` 方法：准备执行环境
- ✅ 抽取 `handleCompilation()` 方法：编译处理流程
- ✅ 抽取 `handleExecution()` 方法：执行判题流程
- ✅ 代码审查通过，编译验证通过

##### ✅ `runTestCases()` 完整重构
- ✅ 抽取 `TestContext` 结构体，封装测试上下文
- ✅ 抽取 `prepareTestContext()` 方法：准备测试环境
- ✅ 抽取 `executeAllTestCases()` 方法：批量执行测试用例
- ✅ 抽取 `executeSingleTestCase()` 方法：单个测试用例执行
- ✅ 抽取 `processTestResults()` 方法：结果处理和分数计算
- ✅ 代码审查通过，编译验证通过

#### 阶段2.2: P1级别重构 (3-4天)
**目标**: 重构 `Run()`、`runAndCompare()`、`handleSpecialJudge()`

Day 5-6: 重构 `Run()`
- [ ] 抽取prepareJudgeContext()
- [ ] 抽取executeCompilation()
- [ ] 抽取executeJudge()
- [ ] 运行所有测试
- [ ] 提交PR，代码审查

Day 7-8: 重构 `runAndCompare()` 和 `handleSpecialJudge()`
- [ ] 抽取命令构建逻辑
- [ ] 抽取文件准备逻辑
- [ ] 抽取SPJ策略逻辑
- [ ] 运行所有测试
- [ ] 提交PR，代码审查

#### 阶段2.3: P2级别重构 (1-2天)
**目标**: 重构 `compareFilesByStream()`

Day 9: 重构流处理函数
- [ ] 抽取流初始化逻辑
- [ ] 抽取过滤器应用逻辑
- [ ] 抽取缓冲区比较逻辑
- [ ] 运行测试
- [ ] 提交PR，代码审查

### 5.3 验证阶段 (2-3天)

#### 任务3.1: 功能验证
- [ ] 运行所有单元测试，确保100%通过
- [ ] 运行集成测试，确保业务流程正常
- [ ] 运行端到端测试，验证完整判题流程
- [ ] 使用真实数据测试，验证结果一致性

#### 任务3.2: 性能验证
- [ ] 运行性能基准测试
- [ ] 对比重构前后的性能指标
- [ ] 确保性能没有下降（差异<5%）
- [ ] 识别并优化性能瓶颈（如果有）

#### 任务3.3: 代码质量验证
- [ ] 运行静态代码分析（golangci-lint）
- [ ] 统计函数平均行数（目标<30行）
- [ ] 统计圈复杂度（目标<5）
- [ ] 统计代码重复率（目标<3%）
- [ ] 生成代码质量报告

#### 任务3.4: 文档更新
- [ ] 更新函数注释
- [ ] 更新模块README
- [ ] 更新架构文档（如果有）
- [ ] 编写重构总结报告

### 5.4 发布阶段 (1天)

- [ ] 合并所有重构分支到主分支
- [ ] 标记版本（tag）
- [ ] 更新CHANGELOG
- [ ] 团队知识分享会议

---

## 6. 风险评估与应对

### 6.1 技术风险

| 风险 | 影响 | 概率 | 应对措施 |
|------|------|------|----------|
| 重构引入新Bug | 高 | 中 | 建立完整的测试覆盖，小步提交，代码审查 |
| 性能下降 | 中 | 低 | 建立性能基准，每次重构后比对性能 |
| 并发逻辑错误 | 高 | 中 | 重点测试并发场景，使用竞态检测器 `-race` |
| 文件句柄泄漏 | 中 | 低 | 使用静态分析工具检查，添加defer确保清理 |

### 6.2 项目风险

| 风险 | 影响 | 概率 | 应对措施 |
|------|------|------|----------|
| 时间超期 | 中 | 中 | 分阶段重构，优先处理核心函数 |
| 团队不熟悉代码 | 中 | 中 | 进行代码走读，编写文档，结对编程 |
| 测试覆盖不足 | 高 | 中 | 优先补充测试用例，确保核心逻辑覆盖 |

### 6.3 应对策略

1. **测试驱动**: 先写测试，确保当前行为被完整捕获，再重构
2. **小步提交**: 每次只重构一小部分，提交并测试
3. **并行分支**: 维护重构分支，不影响主分支开发
4. **代码审查**: 所有重构必须经过至少2人审查
5. **回滚预案**: 保留重构前的代码备份，必要时可回滚

---

## 7. 验收标准

### 7.1 功能性标准
- [ ] 所有现有测试用例通过（单元测试+集成测试）
- [ ] 判题结果与重构前完全一致
- [ ] 所有功能正常工作（编译、运行、判题）
- [ ] 多语言判题功能正常（C/C++/Java/Python等）

### 7.2 性能标准
- [ ] 平均判题时间不增加（差异<5%）
- [ ] CPU使用率不增加
- [ ] 内存使用不增加
- [ ] 没有性能回退

### 7.3 代码质量标准
- [ ] 所有函数行数<50行，80%<30行
- [ ] 所有函数圈复杂度<10，80%<5
- [ ] 函数平均行数<30行
- [ ] golangci-lint检查通过（无error）
- [ ] 新增单元测试覆盖率达到70%
- [ ] 代码重复率<3%

### 7.4 文档标准
- [ ] 每个公函数都有清晰的文档注释
- [ ] 复杂业务逻辑有说明
- [ ] 架构文档更新（如有）
- [ ] 重构总结报告提交

---

## 8. 资源需求

### 8.1 人力资源
- **项目负责人**: 1人（负责整体规划和验收）
- **核心开发者**: 2-3人（负责具体重构实施）
- **代码审查员**: 2-3人（负责代码质量审查）
- **测试工程师**: 1人（负责测试用例编写和验证）

### 8.2 时间资源
- **总工期**: 10-15个工作日
- **准备阶段**: 1-2天
- **重构阶段**: 7-10天
- **验证阶段**: 2-3天

### 8.3 工具和环境
- **代码质量工具**: golangci-lint, go vet
- **测试工具**: go test, gobench
- **版本控制**: Git, GitHub/GitLab
- **CI/CD**: GitHub Actions / GitLab CI
- **基准测试**: 真实判题数据集

### 8.4 测试数据
- [ ] 各类语言代码样例（C/C++/Java/Python/Go）
- [ ] 不同类型的判题数据（普通判题、SPJ、TPJ、UPJ）
- [ ] 边界测试数据（超时、内存超限、编译错误等）
- [ ] 性能基准测试数据（大量测试用例）

---

## 9. 重构计划签字

### 9.1 批准人
| 角色 | 姓名 | 日期 | 签名 |
|------|------|------|------|
| 技术负责人 | | | |
| 项目经理 | | | |
| QA负责人 | | | |

### 9.2 实施团队
| 角色 | 姓名 | 职责 |
|------|------|------|
| 主负责人 | | 总体规划、协调、验收 |
| 主要开发者 | | 核心函数重构 |
| 审查员 | | 代码质量审查 |
| 测试工程师 | | 测试用例编写和验证 |

---

## 附录

### 附录A: 相关文档
- [项目README](./README.md)
- [架构设计文档](./docs/architecture.md)
- [代码规范](./docs/coding-standards.md)

### 附录B: 工具使用指南
- [golangci-lint配置](./.golangci.yml)
- [测试编写指南](./docs/testing-guide.md)
- [性能基准测试](./docs/benchmark.md)

### 附录C: 重构前后对比模板
```go
// 重构前
func LongFunction() {
    // 20+ lines...
    // 30+ lines...
    // 40+ lines...
}

// 重构后
func LongFunction() {
    result1, err := subFunction1()
    if err != nil { return err }

    result2, err := subFunction2(result1)
    if err != nil { return err }

    return processResult(result2)
}

func subFunction1() (*Result, error) {
    // 20 lines
}

func subFunction2(r *Result) (*Result, error) {
    // 20 lines
}
```
