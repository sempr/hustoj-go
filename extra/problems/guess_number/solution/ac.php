<?php
// ac.php — 交互题「猜数字」参考解（PHP）：每轮二分查找，20 次内必然猜中。

$line = fgets(STDIN);
if ($line === false) {
    exit(0);
}
$t = intval(trim($line));

for ($i = 0; $i < $t; $i++) {
    $lo = 1;
    $hi = 1000000;
    while ($lo <= $hi) {
        $mid = intdiv($lo + $hi, 2);
        echo $mid . "\n";
        flush();  // 刷新输出缓冲
        $resp = fgets(STDIN);
        if ($resp === false) {
            exit(0);
        }
        $r = intval(trim($resp));
        if ($r === 0) {
            break;
        } elseif ($r === -1) {  // 猜小了
            $lo = $mid + 1;
        } else {                // 猜大了
            $hi = $mid - 1;
        }
    }
}
