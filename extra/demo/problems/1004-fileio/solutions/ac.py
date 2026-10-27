# 示例题 1004 参考解（Python）：文件 IO（读 sum.in 写 sum.out）。
with open("sum.in", "r") as f:
    values = f.read().split()
a, b = int(values[0]), int(values[1])
with open("sum.out", "w") as f:
    f.write(f"{a + b}\n")
