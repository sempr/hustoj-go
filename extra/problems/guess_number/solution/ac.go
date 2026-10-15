// ac.go — 交互题「猜数字」参考解（Go）：每轮二分查找，20 次内必然猜中。
package main

import (
	"bufio"
	"fmt"
	"os"
)

func main() {
	in := bufio.NewReader(os.Stdin)
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()

	var t int
	if _, err := fmt.Fscan(in, &t); err != nil {
		return
	}

	for ; t > 0; t-- {
		lo, hi := 1, 1000000
		for lo <= hi {
			mid := (lo + hi) / 2
			fmt.Fprintln(out, mid)
			out.Flush() // 必须刷新，否则交互器读不到猜测

			var r int
			if _, err := fmt.Fscan(in, &r); err != nil {
				return
			}
			if r == 0 {
				break
			}
			if r == -1 {
				lo = mid + 1 // 猜小了 → 往大找
			} else {
				hi = mid - 1 // 猜大了 → 往小找
			}
		}
	}
}
