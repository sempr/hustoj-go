/* 示例题 1002 特殊评测程序（problem.spj=1，hustoj 风格）。
 *
 * 调用（沙箱内 /code 下）：spj data.in sysdata.out data.usr
 *   argv[1] = 输入文件（含 n）
 *   argv[2] = 标准答案文件（本题不唯一，忽略）
 *   argv[3] = 选手输出
 * 退出码：0 = AC，非 0 = WA；诊断写到 stderr。
 *
 * 编译：gcc -O2 -static spj.c -o spj
 */
#include <stdio.h>
#include <stdlib.h>

int main(int argc, char **argv) {
    if (argc < 4) { fprintf(stderr, "SE: usage spj data.in sysdata.out data.usr\n"); return 2; }
    FILE *fin = fopen(argv[1], "r");
    FILE *fusr = fopen(argv[3], "r");
    if (!fin || !fusr) { fprintf(stderr, "SE: cannot open files\n"); return 2; }

    int n;
    if (fscanf(fin, "%d", &n) != 1 || n < 3 || n > 99 || n % 2 == 0) {
        fprintf(stderr, "SE: bad n in test data\n"); return 2;
    }
    long long N = (long long)n * n;
    unsigned char *seen = calloc((size_t)N + 1, 1);
    long long *row = calloc((size_t)n, sizeof(long long));
    long long *col = calloc((size_t)n, sizeof(long long));
    if (!seen || !row || !col) { fprintf(stderr, "SE: oom\n"); return 2; }

    long long d1 = 0, d2 = 0;
    for (int i = 0; i < n; ++i) {
        for (int j = 0; j < n; ++j) {
            long long v;
            if (fscanf(fusr, "%lld", &v) != 1) { fprintf(stderr, "WA: too few numbers\n"); return 1; }
            if (v < 1 || v > N) { fprintf(stderr, "WA: value %lld out of range\n", v); return 1; }
            if (seen[v]) { fprintf(stderr, "WA: duplicate value %lld\n", v); return 1; }
            seen[v] = 1;
            row[i] += v; col[j] += v;
            if (i == j) d1 += v;
            if (i + j == n - 1) d2 += v;
        }
    }
    char extra[64];
    if (fscanf(fusr, "%63s", extra) == 1) { fprintf(stderr, "WA: extra output\n"); return 1; }

    long long magic = (long long)n * (N + 1) / 2;
    for (int i = 0; i < n; ++i) {
        if (row[i] != magic) { fprintf(stderr, "WA: row %d sum %lld != %lld\n", i + 1, row[i], magic); return 1; }
        if (col[i] != magic) { fprintf(stderr, "WA: col %d sum %lld != %lld\n", i + 1, col[i], magic); return 1; }
    }
    if (d1 != magic || d2 != magic) { fprintf(stderr, "WA: diagonal mismatch\n"); return 1; }

    fprintf(stderr, "OK: %dx%d magic square accepted\n", n, n);
    return 0;
}
