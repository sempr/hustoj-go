#!/usr/bin/env bash
# ac.sh — 交互题「猜数字」参考解（Bash）：每轮二分查找，20 次内必然猜中。
read -r t
i=0
while [ "$i" -lt "$t" ]; do
  lo=1
  hi=1000000
  while [ "$lo" -le "$hi" ]; do
    mid=$(( (lo + hi) / 2 ))
    echo "$mid"
    read -r r
    if [ "$r" -eq 0 ]; then
      break
    elif [ "$r" -eq -1 ]; then
      lo=$(( mid + 1 ))
    else
      hi=$(( mid - 1 ))
    fi
  done
  i=$(( i + 1 ))
done
