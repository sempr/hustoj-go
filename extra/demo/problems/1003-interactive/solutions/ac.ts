// ac.ts — 交互题「猜数字」参考解（TypeScript/Node）：每轮二分查找。
// 用 fs.readSync 逐字节同步读入，避免 async 事件循环与交互协议不匹配。
declare const require: (m: string) => any;
declare const process: any;
declare const Buffer: any;
const fs = require("fs");

function readLine(): string | null {
  const buf = Buffer.alloc(1);
  let s = "";
  for (;;) {
    let n = 0;
    try {
      n = fs.readSync(0, buf, 0, 1, null);
    } catch (e: any) {
      if (e && e.code === "EAGAIN") continue;
      return s.length > 0 ? s : null;
    }
    if (n === 0) return s.length > 0 ? s : null;
    const c = buf.toString("utf8", 0, 1);
    if (c === "\n") return s;
    if (c !== "\r") s += c;
  }
}

const first = readLine();
if (first === null) process.exit(0);
const t = parseInt(first, 10);

for (let i = 0; i < t; i++) {
  let lo = 1;
  let hi = 1000000;
  while (lo <= hi) {
    const mid = Math.floor((lo + hi) / 2);
    fs.writeSync(1, mid + "\n");
    const resp = readLine();
    if (resp === null) process.exit(0);
    const r = parseInt(resp, 10);
    if (r === 0) break;
    if (r === -1) lo = mid + 1; // 猜小了 → 往大找
    else hi = mid - 1;          // 猜大了 → 往小找
  }
}
