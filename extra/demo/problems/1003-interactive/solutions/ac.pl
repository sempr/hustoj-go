#!/usr/bin/perl
# ac.pl — 交互题「猜数字」参考解（Perl）：每轮二分查找，20 次内必然猜中。
use strict;
use warnings;

$| = 1;  # 关闭缓冲，逐行自动刷新

my $line = <STDIN>;
exit 0 unless defined $line;
my $t = int($line);

for (my $i = 0; $i < $t; $i++) {
    my $lo = 1;
    my $hi = 1000000;
    while ($lo <= $hi) {
        my $mid = int(($lo + $hi) / 2);
        print "$mid\n";
        my $resp = <STDIN>;
        exit 0 unless defined $resp;
        my $r = int($resp);
        if ($r == 0) {
            last;
        } elsif ($r == -1) {  # 猜小了
            $lo = $mid + 1;
        } else {              # 猜大了
            $hi = $mid - 1;
        }
    }
}
