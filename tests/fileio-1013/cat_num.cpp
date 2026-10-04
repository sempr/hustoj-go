#include <cstdio>
int main() {
    freopen("number.in", "r", stdin);
    freopen("number.out", "w", stdout);
    char buf[4096];
    while (fgets(buf, sizeof buf, stdin)) fputs(buf, stdout);
    return 0;
}
