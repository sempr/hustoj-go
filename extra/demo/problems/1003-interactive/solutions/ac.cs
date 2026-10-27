// ac.cs — 交互题「猜数字」参考解（C#）：每轮二分查找，20 次内必然猜中。
using System;

class MainClass {
    static void Main() {
        string line = Console.In.ReadLine();
        if (line == null) return;
        int t = int.Parse(line.Trim());

        for (int i = 0; i < t; i++) {
            int lo = 1, hi = 1000000;
            while (lo <= hi) {
                int mid = lo + (hi - lo) / 2;
                Console.Out.Write(mid + "\n");
                Console.Out.Flush();  // 必须刷新，否则交互器读不到猜测
                string resp = Console.In.ReadLine();
                if (resp == null) return;
                int r = int.Parse(resp.Trim());
                if (r == 0) break;
                if (r == -1) lo = mid + 1;  // 猜小了 → 往大找
                else hi = mid - 1;          // 猜大了 → 往小找
            }
        }
    }
}
