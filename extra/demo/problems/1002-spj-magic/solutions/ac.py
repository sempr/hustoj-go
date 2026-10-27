# 示例题 1002 参考解（Python）：奇数阶幻方的 Siamese 构造法。
n = int(input())
a = [[0] * n for _ in range(n)]
i, j = 0, n // 2
for v in range(1, n * n + 1):
    a[i][j] = v
    ni, nj = (i - 1) % n, (j + 1) % n
    if a[ni][nj]:
        ni, nj = (i + 1) % n, j
    i, j = ni, nj
print('\n'.join(' '.join(map(str, row)) for row in a))
