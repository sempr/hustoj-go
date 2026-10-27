// ac.rs — 交互题「猜数字」参考解（Rust）：每轮二分查找，20 次内必然猜中。
use std::io::{self, BufRead, Write};

fn main() {
    let stdin = io::stdin();
    let mut lines = stdin.lock().lines();
    let mut stdout = io::stdout();

    let t: i64 = match lines.next() {
        Some(Ok(s)) => s.trim().parse().unwrap_or(0),
        _ => return,
    };

    for _ in 0..t {
        let mut lo: i64 = 1;
        let mut hi: i64 = 1000000;
        while lo <= hi {
            let mid = (lo + hi) / 2;
            let _ = writeln!(stdout, "{}", mid);
            let _ = stdout.flush(); // 必须刷新，否则交互器读不到猜测

            let r: i64 = match lines.next() {
                Some(Ok(s)) => s.trim().parse().unwrap_or(0),
                _ => return,
            };
            if r == 0 {
                break;
            } else if r == -1 {
                lo = mid + 1; // 猜小了 → 往大找
            } else {
                hi = mid - 1; // 猜大了 → 往小找
            }
        }
    }
}
