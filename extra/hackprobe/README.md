# 沙箱逃逸探针（hackprobe）

给判题沙箱做隔离性自检用的探针程序。每个探针只做**探测与自述**，不带破坏性
载荷；唯一的写操作是往 `/tmp/hustoj_escape_probe` 写一个标记文件，用来确认
是不是真的逃到了宿主机。

探针以**测试运行**（`problem_id=0` + `custominput`）提交：测试运行的 stdout
会写进 `runtimeinfo`，在 `showsource.php` 的「各用例结果」里就能看到报告；
普通提交的 stdout 不入库。

```bash
sudo ./hackprobe/seed_probes.sh --ojhome /srv/hustoj --user demo
# 只跑其中几个：--only race_alloc,race_spawn
# 只看会做什么、不改库：--dry-run
```

## 探针一览

| 探针 | 探什么 | 安全状态下的期望 |
| --- | --- | --- |
| `cap_probe` | 身份与能力 | `Uid: 65534`（nobody）、`CapEff: 0`、`NoNewPrivs: 1` |
| `pidns_probe` | PID 命名空间 | 可见进程数只有 2~3；`/proc/1/cmdline` 是沙箱里的 tini；`NSpid` 只有一层 |
| `procroot_probe` | 宿主机文件系统可达性 | `statfs("/")` 是 overlayfs；`/proc/1/root/...` 写不出去；宿主机上不出现 `hustoj_escape_probe` |
| `device_probe` | 设备节点与敏感文件 | `/dev/mem`、`/proc/kcore`、`/etc/shadow` 等一个都打不开 |
| `forkbomb_probe` | 进程数上限 | 被 cgroup 的 `pids.max` 卡在几十个 |
| `userns_probe` | 用户命名空间与危险 syscall | `unshare(CLONE_NEWUSER)`、`mount`、`ptrace`、`setns` 全部失败 |
| `rlimit_probe` | 生效的资源限制 | `RLIMIT_FSIZE` = 64MB（判题器加的输出上限）；`RLIMIT_AS` 一般是 unlimited |
| `race_alloc_probe` | **有概率**：cgroup 生效前的内存竞态 | 分配量卡在上限附近 |
| `race_spawn_probe` | **有概率**：cgroup 生效前的进程数竞态 | 进程数卡在 64 附近 |

## 两个「有概率」的探针在测什么

判题器是**先启动进程、再建 cgroup、最后把 pid 挪进去**的：

```
internal/run/run.go
  cmd.Start()                 ← 进程已经跑起来了
  ...
  shared.NewCgroup(...)       ← 这时才建 cgroup 并写 cgroup.procs
```

在这个窗口里，进程的内存与进程数都不受 `memory.max` / `pids.max` 约束。
`race_alloc_probe` / `race_spawn_probe` 从第一条指令就开始抢这个窗口，报告
实际抢到多少。

- 抢到了 → 内存/进程数限制可以被绕过一瞬间（打表、占满判题机都做得成），
  修法是把 cgroup 建好、把 pid 挪进去之后再放程序跑；
- 没抢到 → 窗口存在但这次没赢，也仍然值得收窄。

注意：这不影响**判定结果**——最终得分仍按实测峰值给 ML/TL；这里测的是
「当场杀掉」这条兜底有没有生效。

## 跑完怎么读

探针走的是**测试运行**（`problem_id=0`），而状态页只列 `problem_id > 0` 的提交，
所以它们**不会出现在 status.php**。直接按提交号打开源码页看报告：

```
showsource.php?id=<提交号>   →  「各用例结果」里就是探针的输出
```

宿主机上再确认一次没有逃逸：`ls -l /tmp/hustoj_escape_probe`（不存在才是安全的）。

## 一次实测结果（2026-10-22，10.80.1.11）

隔离本身是有效的：

| 项 | 实测 | 结论 |
| --- | --- | --- |
| `statfs("/")` | `0x794c7630`（overlayfs） | pivot_root 生效，根目录是沙箱的镜像 |
| `/proc/1/root/tmp/...` 写入 | Permission denied | 到不了宿主机 |
| 宿主机 `/tmp/hustoj_escape_probe` | 不存在 | 没有逃逸 |
| 可见进程数 | 2（tini + 自己） | PID 命名空间生效 |
| `Uid` / `CapEff` / `NoNewPrivs` | `65534` / `0` / `1` | 已降权、无能力、不可提权 |
| `pids.max` | 第 63 次 fork 失败（EAGAIN） | 进程数上限生效 |
| `RLIMIT_FSIZE` | 67108864（64MB） | 输出上限生效 |
| 内存上限 | 探针申请到 320MB 即被 OOM killer 干掉 | cgroup 的 memory.max 从第一条指令就生效 |

**没有"启动瞬间不受限"的窗口**：tini 在 fork 用户程序之前会先 `raise(SIGSTOP)`
把自己停住，判题器建好 cgroup、把 pid 挪进去之后才 `PtraceCont` 放行 ——
用户程序出生时就已经在 cgroup 里了。

### 自检顺带挖出来的两个真 bug（已修）

1. **测试运行的内存上限单位错了**：`RunConfig.MemoryLimit` 是 MB，测试运行却按
   KB 填（256MB 写成 262144），沙箱再 `<<10` 换算一次 → cgroup 的 memory.max
   变成 256TB，**内存限制等于没有**。探针一口气申请 2.4GB 才被宿主机的 OOM
   killer 收走。修法：单位统一到 MB，换算收口到一个 `sandboxMemoryKB()`，
   并补了单测（`internal/client/client_test.go`）。
2. **判题器的进程计数有竞态**：clone 事件与子进程的退出事件是异步投递的，
   短命子进程的退出事件可能先于它父进程的 clone 事件被处理，计数会瞬间掉到 1
   → 判题器以为只剩残留进程，把**还在跑的用户程序**整个命名空间 SIGTERM 掉
   （表现为程序被误杀、判 RE，且 stdout 全丢）。修法：不再用"活着的进程数掉到 1"
   触发清理，改成按 tini（命名空间 init）退出这个确定事件收尾 —— 内核本来就会
   在 init 退出时把命名空间里剩下的进程一起杀掉。

### 仍建议加固的地方

1. **没有 seccomp**（`Seccomp: 0`）。mount/unshare/ptrace 现在只靠 uid 挡，
   一旦出现提权就会被放大。建议加 syscall 白名单。
2. **非特权用户命名空间可用**：`unshare(CLONE_NEWUSER)` 成功，进程在新的
   用户命名空间里拿到满权限（只限该命名空间，尚未发现逃逸路径），
   同样交给 seccomp 挡。
3. **`/proc/kallsyms` 可读** → 内核符号地址泄露，KASLR 形同虚设。
   建议挂 procfs 时限制，或用 seccomp 挡 `openat`。
4. **`/dev/console` 可打开**（语言镜像里带的字符设备）→ 程序能往宿主机控制台
   写东西。镜像里去掉这个节点，或用 cgroup 的 `devices` 限制。

另外，探针能看到 tini 的命令行（含宿主机 rootfs 路径 `/srv/hustoj/run0/rootfs`），
属于轻微信息泄露。

