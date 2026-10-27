# 内存峰值探针

`vmhwm.c` 打印进程自己的 `VmHWM` / `VmRSS`（`/proc/self/status`），
用来核对判题器报出的「内存用量」。

判题器的内存口径是 **cgroup 的匿名内存峰值**（沙箱里所有进程的堆栈之和，
多进程程序算总和；不含沙箱 tmpfs 上的输出）。所以：

- 正常情况下，本程序打印的 `VmHWM` 与判题器报的内存应同量级；
- 差很多（例如固定多出 20MB）说明口径出了问题 —— 历史上就踩过
  `getrusage(RUSAGE_CHILDREN).ru_maxrss` 被 Go 的 `fork+exec` 污染的坑。

用法：

```bash
# 编译
gcc -static -O2 -o vmhwm vmhwm.c

# 作为普通提交跑一遍（stdout 不会进 runtimeinfo，看不到输出）
# 作为测试运行（custominput）跑，stdout 会写进 runtimeinfo
#   INSERT INTO custominput(solution_id, input_text) VALUES (<sid>, '');
```

手工在沙箱里跑：

```bash
hustoj run --rootfs <rootfs> --cwd /code --cmd "/code/Main" \
  --stdout /code/data.usr --stderr /code/data.err --time 1000 --memory 131072 --sid 1
```
