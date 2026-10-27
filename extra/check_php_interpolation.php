<?php
/**
 * 检查 PHP 变量名被相邻中文/全角字符吞掉的 bug。
 *
 * 背景：PHP 标识符允许 \x80-\xff 字节，因此双引号里的 "$var中文"
 * 会被整体当成变量名，$var 插值成空串，且无任何报错。
 * 本仓库已实际踩过三次：
 *   - lang/cnt.php        "$SMTP_USER等參數"  → 报错提示丢失 SMTP 账号
 *   - aiapi/common.php    "$keyword的吸引人的标题" → AI 提示词丢掉用户输入
 *   - aiapi/yuanqi.php    "$role。你熟悉"      → 同上
 * 用法: php extra/check_php_interpolation.php [目录]
 * 退出码 0 = 通过，1 = 发现污染变量名。
 */
$root = $argv[1] ?? dirname(__DIR__);

if (!is_dir($root)) {
    fwrite(STDERR, "目录不存在: $root\n");
    exit(2);
}

$bad = [];
$rii = new RecursiveIteratorIterator(new RecursiveDirectoryIterator($root, FilesystemIterator::SKIP_DOTS));

foreach ($rii as $f) {
    if (!$f->isFile() || strtolower($f->getExtension()) !== 'php') {
        continue;
    }
    $path = $f->getPathname();
    // 用词法器而非正则：注释与单引号串不会插值，不该报错
    foreach (token_get_all(file_get_contents($path)) as $t) {
        if (!is_array($t) || $t[0] !== T_VARIABLE) {
            continue;
        }
        if (preg_match('/[\x80-\xff]/', $t[1])) {
            $bad[] = sprintf('%s:%d  %s', str_replace($root . '/', '', $path), $t[2], $t[1]);
        }
    }
}

if ($bad) {
    echo "✗ 发现 " . count($bad) . " 处变量名被相邻非 ASCII 字符污染：\n";
    foreach ($bad as $b) {
        echo "    $b\n";
    }
    echo "  修复方式：改用花括号插值，如 {\$var}中文\n";
    exit(1);
}

echo "✓ PHP 变量名插值检查通过\n";
exit(0);
