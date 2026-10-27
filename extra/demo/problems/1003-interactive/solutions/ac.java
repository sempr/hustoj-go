// ac.java — 交互题「猜数字」参考解（Java）：每轮二分查找，20 次内必然猜中。
import java.io.*;

public class Main {
    public static void main(String[] args) throws IOException {
        BufferedReader in = new BufferedReader(new InputStreamReader(System.in));
        PrintWriter out = new PrintWriter(
                new BufferedWriter(new OutputStreamWriter(System.out)));

        String line = in.readLine();
        if (line == null) return;
        int t = Integer.parseInt(line.trim());

        for (int i = 0; i < t; i++) {
            int lo = 1, hi = 1000000;
            while (lo <= hi) {
                int mid = lo + (hi - lo) / 2;
                out.print(mid);
                out.print('\n');
                out.flush();  // 必须刷新，否则交互器读不到猜测
                String resp = in.readLine();
                if (resp == null) return;
                int r = Integer.parseInt(resp.trim());
                if (r == 0) break;
                if (r == -1) lo = mid + 1;  // 猜小了 → 往大找
                else hi = mid - 1;          // 猜大了 → 往小找
            }
        }
        out.flush();
    }
}
