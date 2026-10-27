# ac.py — 交互题「猜数字」参考解（Python）：每轮二分查找，20 次内必然猜中。
import sys


def main():
    inp = sys.stdin
    out = sys.stdout

    line = inp.readline()
    if not line:
        return
    t = int(line)

    for _ in range(t):
        lo, hi = 1, 1000000
        while lo <= hi:
            mid = (lo + hi) // 2
            out.write(str(mid) + "\n")
            out.flush()  # 必须刷新，否则交互器读不到猜测
            resp = inp.readline()
            if not resp:
                return
            r = int(resp)
            if r == 0:
                break
            if r == -1:  # 猜小了 → 往大找
                lo = mid + 1
            else:        # 猜大了 → 往小找
                hi = mid - 1


main()
