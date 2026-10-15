{ ac.pas — 交互题「猜数字」参考解（Pascal）：每轮二分查找，20 次内必然猜中。 }
program Main;
var
  t, lo, hi, mid, r: longint;
begin
  readln(t);
  while t > 0 do
  begin
    lo := 1;
    hi := 1000000;
    while lo <= hi do
    begin
      mid := lo + (hi - lo) div 2;
      writeln(mid);
      flush(output);  { 必须刷新，否则交互器读不到猜测 }
      readln(r);
      if r = 0 then
        break
      else if r = -1 then
        lo := mid + 1  { 猜小了 → 往大找 }
      else
        hi := mid - 1; { 猜大了 → 往小找 }
    end;
    t := t - 1;
  end;
end.
