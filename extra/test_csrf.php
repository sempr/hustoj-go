<?php
/**
 * CSRF 回归测试：csrf.php 签发 + include/csrf_check.php 校验。
 *
 * 用法: php extra/test_csrf.php
 * 依赖: PHP CLI，会话写入临时目录。不访问数据库。
 *
 * 每条用例在独立子进程中执行（经 extra/_csrf_probe.php），
 * 因为 csrf_check.php 校验失败会直接 exit()，无法在同进程内断言。
 */

$root = dirname(__DIR__);
$probe = $root . '/extra/_csrf_probe.php';
$savePath = sys_get_temp_dir() . '/hustoj-csrf-test';
@mkdir($savePath, 0700, true);

$pass = 0;
$fail = 0;

function ok($name, $cond, $extra = '')
{
    global $pass, $fail;
    if ($cond) {
        $pass++;
        printf("  ✓ %s\n", $name);
    } else {
        $fail++;
        printf("  ✗ %s%s\n", $name, $extra !== '' ? "  → $extra" : '');
    }
}

/** 调用探针；返回 ['token'=>..,'sid'=>..,'pool'=>..] 或 ['err'=>诊断] */
function probe(array $spec)
{
    global $probe;
    $errFile = tempnam(sys_get_temp_dir(), 'csrf-err-');
    $cmd = escapeshellarg(PHP_BINARY) . ' ' . escapeshellarg($probe) . ' '
         . escapeshellarg(json_encode($spec, JSON_UNESCAPED_UNICODE))
         . ' 2>' . escapeshellarg($errFile);
    $raw = (string)shell_exec($cmd);
    $err = (string)@file_get_contents($errFile);
    @unlink($errFile);

    $j = json_decode(trim($raw), true);
    if (!is_array($j)) {
        // validate 失败时 csrf_check.php 直接 echo 文本并 exit，stdout 非 JSON 属预期；
        // 此时用 stdout 作为失败证据。
        return ['err' => trim($raw) !== '' ? trim($raw) : trim($err)];
    }
    return $j;
}

// ── 1. 签发：同源判定 ────────────────────────────────────────────────
echo "=== csrf.php 签发（同源判定）===\n";

$r = probe(['op' => 'issue', 'method' => 'GET', 'referer' => 'http://oj.example.com/submitpage.php']);
ok('同源 Referer 签发 token', ($r['token'] ?? null) !== null, $r['err'] ?? 'token=null');

$r = probe(['op' => 'issue', 'method' => 'GET']);
ok('缺失 Referer 仍签发（兼容非浏览器客户端）', ($r['token'] ?? null) !== null);

$r = probe(['op' => 'issue', 'method' => 'GET', 'referer' => 'http://evil.example.net/x.php']);
ok('跨源 Referer 不签发 token', ($r['token'] ?? null) === null);

$r = probe(['op' => 'issue', 'method' => 'GET', 'referer' => 'http://oj.example.com:8080/x']);
ok('同源不同端口视为同源', ($r['token'] ?? null) !== null);

$r = probe(['op' => 'issue', 'method' => 'GET', 'referer' => 'https://OJ.Example.COM/x']);
ok('同源大小写与协议不敏感', ($r['token'] ?? null) !== null);

$r = probe(['op' => 'issue', 'method' => 'GET', 'referer' => 'http://oj.example.com.evil.net/x']);
ok('后缀相似域名判定为跨源', ($r['token'] ?? null) === null);

$r = probe(['op' => 'issue', 'method' => 'GET', 'referer' => 'http://evil.net/?u=http://oj.example.com']);
ok('Referer 中嵌套本站 URL 不算同源', ($r['token'] ?? null) === null);

$r = probe(['op' => 'issue', 'method' => 'GET', 'referer' => 'not-a-url']);
ok('非法 Referer 视为跨源（不签发）', ($r['token'] ?? null) === null);

// ── 2. 签发：token 池上限 ────────────────────────────────────────────
echo "=== csrf.php token 池 ===\n";

$sid = bin2hex(random_bytes(8));
$pool = null;
for ($i = 0; $i < 15; $i++) {
    $r = probe(['op' => 'issue', 'method' => 'GET', 'sid' => $sid,
                'referer' => 'http://oj.example.com/']);
    $pool = $r['pool'] ?? null;
}
ok('连续签发 15 次后会话内只保留最近 10 个', $pool === 10, '实际 ' . var_export($pool, true));

// ── 3. 校验 ─────────────────────────────────────────────────────────
echo "=== csrf_check.php 校验 ===\n";

// 建立会话并取一个有效 token
$iss = probe(['op' => 'issue', 'method' => 'GET', 'referer' => 'http://oj.example.com/submitpage.php']);
$sid = $iss['sid'] ?? '';
$tok = $iss['token'] ?? '';
$poolBefore = $iss['pool'] ?? 0;
ok('探针可在指定会话内取得 token', $tok !== '' && $sid !== '' && $poolBefore === 1,
   json_encode($iss, JSON_UNESCAPED_UNICODE));

$r = probe(['op' => 'validate', 'method' => 'POST', 'sid' => $sid, 'post' => ['csrf' => $tok]]);
ok('POST 携带有效 token 通过', ($r['passed'] ?? false) === true, $r['err'] ?? '未通过');
ok('通过后 token 已从池中消费', ($r['pool'] ?? -1) === $poolBefore - 1,
   "前 $poolBefore → 后 " . var_export($r['pool'] ?? null, true));

$r = probe(['op' => 'validate', 'method' => 'POST', 'sid' => $sid, 'post' => ['csrf' => $tok]]);
ok('同一 token 二次使用被拒（单次消费）', ($r['passed'] ?? false) !== true);

$r = probe(['op' => 'validate', 'method' => 'POST', 'sid' => $sid, 'post' => []]);
ok('POST 缺少 csrf 字段被拒', ($r['passed'] ?? false) !== true);

$r = probe(['op' => 'validate', 'method' => 'POST', 'sid' => $sid, 'post' => ['csrf' => 'deadbeef']]);
ok('POST 伪造 token 被拒', ($r['passed'] ?? false) !== true);

$r = probe(['op' => 'validate', 'method' => 'POST', 'sid' => $sid, 'post' => ['csrf' => ['x']]]);
ok('POST csrf 为数组被拒（防类型混淆）', ($r['passed'] ?? false) !== true);

$r = probe(['op' => 'validate', 'method' => 'POST', 'sid' => bin2hex(random_bytes(8)),
            'post' => ['csrf' => $tok]]);
ok('会话不存在时拒绝（fail-closed）', ($r['passed'] ?? false) !== true);

$r = probe(['op' => 'validate', 'method' => 'GET', 'sid' => $sid, 'post' => []]);
ok('GET 请求不触发校验（保持只读页面可用）', ($r['passed'] ?? false) === true, $r['err'] ?? '被拒');

// ── 4. postkey（37 个端点共用）───────────────────────────────────────
echo "=== check_post_key.php（postkey 机制）===\n";

// 先惰性签发，取回 token
$r = probe(['op' => 'peek_postkey', 'method' => 'GET']);
$sid = $r['sid'] ?? '';
$tok = $r['token'] ?? '';
ok('postkey 惰性签发为 10 位大写十六进制', preg_match('/^[0-9A-F]{10}$/', (string)$tok) === 1,
   json_encode($r, JSON_UNESCAPED_UNICODE));

$r = probe(['op' => 'postkey', 'method' => 'POST', 'sid' => $sid, 'post' => ['postkey' => $tok]]);
ok('正确 postkey 通过', ($r['passed'] ?? false) === true, $r['err'] ?? '未通过');
ok('通过后 postkey 作废（单次消费）', ($r['consumed'] ?? false) === true,
   json_encode($r, JSON_UNESCAPED_UNICODE));

$r = probe(['op' => 'postkey', 'method' => 'POST', 'sid' => $sid, 'post' => ['postkey' => $tok]]);
ok('同一 postkey 二次使用被拒', ($r['passed'] ?? false) !== true);

$r = probe(['op' => 'postkey', 'method' => 'POST', 'sid' => $sid, 'post' => []]);
ok('缺少 postkey 被拒', ($r['passed'] ?? false) !== true);

$r = probe(['op' => 'postkey', 'method' => 'POST', 'sid' => $sid, 'post' => ['postkey' => ['x']]]);
ok('postkey 为数组被拒（防松散比较）', ($r['passed'] ?? false) !== true);

$r = probe(['op' => 'postkey', 'method' => 'POST', 'sid' => $sid, 'post' => ['postkey' => '0000000000']]);
ok('错误 postkey 被拒', ($r['passed'] ?? false) !== true);

// ── 5. AJAX 校验（不消费 token）─────────────────────────────────────────
// admin/ajax.php 等端点单页内会发起多次请求，token 必须可重复使用；
// 但仍不能缺失或错误即放行。
echo "=== csrf_check_ajax.php（AJAX，可重复校验）===\n";

$sid = 'ajax' . bin2hex(random_bytes(6));
$r = probe(['op' => 'issue', 'sid' => $sid]);
$atok = $r['token'] ?? '';
ok('AJAX 端可签发 token', is_string($atok) && $atok !== '', json_encode($r));

$r = probe(['op' => 'validate_ajax', 'sid' => $sid, 'method' => 'POST', 'post' => ['csrf' => $atok]]);
ok('AJAX 携带有效 token 通过', ($r['passed'] ?? false) === true, $r['err'] ?? '未通过');
ok('AJAX 校验后 token 未被消费（可重复使用）', ($r['reusable'] ?? false) === true,
   '单页多次内联编辑依赖此性质');

$r = probe(['op' => 'validate_ajax', 'sid' => $sid, 'method' => 'POST', 'post' => ['csrf' => $atok]]);
ok('AJAX 同一 token 可连续多次通过', ($r['passed'] ?? false) === true);

$r = probe(['op' => 'validate_ajax', 'sid' => $sid, 'method' => 'POST', 'post' => []]);
ok('AJAX 缺 token 被拒', ($r['passed'] ?? false) !== true);

$r = probe(['op' => 'validate_ajax', 'sid' => $sid, 'method' => 'POST', 'post' => ['csrf' => 'deadbeef']]);
ok('AJAX 错误 token 被拒', ($r['passed'] ?? false) !== true);

$r = probe(['op' => 'validate_ajax', 'sid' => $sid, 'method' => 'POST', 'post' => ['csrf' => ['x']]]);
ok('AJAX token 为数组被拒', ($r['passed'] ?? false) !== true);

$r = probe(['op' => 'validate_ajax', 'sid' => 'other' . bin2hex(random_bytes(6)), 'method' => 'POST', 'post' => ['csrf' => $atok]]);
ok('AJAX token 不能跨会话使用', ($r['passed'] ?? false) !== true);

// ── 6. submit.php 提交+轮询序列 ────────────────────────────────────────
// 提交流程是"提交一次 + 轮询结果若干次"，六套皮肤都走
// $.post("submit.php?ajax", form.serialize())，token 是表单里的静态 hidden。
// 若这里换成单次消费校验，第一发消费掉 token、紧接着轮询就 403，
// 用户表现为"提交成功但一直转圈看不到结果"。
echo "=== submit.php 提交+轮询序列 ===\n";

$sid = 'sub' . bin2hex(random_bytes(6));
$r = probe(['op' => 'issue', 'sid' => $sid]);
$stok = $r['token'] ?? '';
ok('提交页可签发 token', is_string($stok) && $stok !== '', json_encode($r));

$r = probe(['op' => 'validate_poll', 'sid' => $sid, 'mode' => 'reusable',
            'method' => 'POST', 'times' => 5, 'post' => ['csrf' => $stok]]);
ok('可重复校验：同一 token 可完成 1 次提交 + 4 次轮询', ($r['times'] ?? 0) === 5,
   '实际通过 ' . ($r['times'] ?? 0) . ' 次');

// 反证：单次消费校验在轮询序列中必然断在第二发。
// 这条断言把"为什么 submit.php 不能用 csrf_check.php"钉死，
// 防止有人日后以"更严格"为由换回去。
$sid2 = 'sub2' . bin2hex(random_bytes(6));
$r2 = probe(['op' => 'issue', 'sid' => $sid2]);
$r3 = probe(['op' => 'validate_poll', 'sid' => $sid2, 'mode' => 'single',
             'method' => 'POST', 'times' => 5, 'post' => ['csrf' => $r2['token'] ?? '']]);
ok('单次消费校验在轮询序列中断（故 submit.php 不可用它）',
   ($r3['passed'] ?? false) !== true,
   '若此条转红，说明单次消费校验的行为变了，需重新评估 submit.php');

$r = probe(['op' => 'validate_poll', 'sid' => $sid, 'method' => 'POST', 'times' => 2, 'post' => ['csrf' => 'bogus']]);
ok('错误 token 在轮询序列中被拒', ($r['passed'] ?? false) !== true);

$subSrc = file_get_contents("$root/phpweb/submit.php");
ok('submit.php 使用可重复校验（csrf_check_ajax）',
   strpos($subSrc, 'csrf_check_ajax.php') !== false);
ok('submit.php 未回退到单次消费校验',
   strpos($subSrc, '/include/csrf_check.php') === false);

// ── 7. admin/ajax.php 写操作必须带 token ───────────────────────────────
// 该文件原先没有任何 CSRF 防护，却能改 coin_bonus / expiry_date / group_name。
echo "=== admin/ajax.php 写操作 CSRF 门禁 ===\n";

$ajaxSrc = file_get_contents("$root/phpweb/admin/ajax.php");
ok('ajax.php 引入 AJAX 版 CSRF 校验',
   strpos($ajaxSrc, 'csrf_check_ajax.php') !== false);
ok('ajax.php 仍保留角色门禁',
   strpos($ajaxSrc, "_administrator'") !== false);
ok('ajax.php 只读查询已显式放行（problem_get_title）',
   strpos($ajaxSrc, '"problem_get_title"') !== false);
ok('ajax.php 只读查询已显式放行（get_user_list_of_contest）',
   strpos($ajaxSrc, '"get_user_list_of_contest"') !== false);

// 客户端必须真的把 token 送出去，否则加了门禁=后台功能全废。
$hdr = file_get_contents("$root/phpweb/admin/admin-header.php");
ok('admin-header 向 JS 暴露 window.CSRF_TOKEN',
   strpos($hdr, 'window.CSRF_TOKEN') !== false);
ok('admin-header 用 ajaxPrefilter 自动附加 token',
   strpos($hdr, 'ajaxPrefilter') !== false);
ok('prefilter 排除了 aiapi 端点',
   strpos($hdr, 'aiapi') !== false);
ok('不再使用会生成重复 id 的 form#csrf 注入方式',
   strpos($hdr, "<div id='csrf'") === false &&
   strpos($hdr, 'id="csrf"') === false);

// ── 5. 模板覆盖：每个 POST 表单都必须带 csrf token ────────────────────
// 只测机制不足以防回归：新增模板或漏插 require("csrf.php") 时，
// 单测仍然全绿，但 $OJ_CSRF=true 会让该表单直接无法提交。
// 这里按文件解析 <form> 数量与 csrf 签发次数，逐一核对。
echo "=== 模板表单 CSRF 覆盖 ===\n";

$tplDir = $root . '/phpweb/template';
$skins  = ['bs3', 'bshark', 'mdui', 'sidebar', 'sweet', 'syzoj'];
$pages  = ['loginpage.php', 'registerpage.php', 'submitpage.php'];

$uncovered = [];
foreach ($skins as $skin) {
    foreach ($pages as $page) {
        $f = "$tplDir/$skin/$page";
        if (!is_file($f)) {
            continue;
        }
        $src = file_get_contents($f);
        $forms = preg_match_all('/<form\b/i', $src);
        $tokens = preg_match_all('/(?:require|include)\s*\(\s*["\']csrf\.php["\']\s*\)/i', $src);
        if ($forms === 0) {
            continue;
        }
        if ($forms === $tokens) {
            ok(sprintf('%s/%s（%d 个表单）', $skin, $page, $forms), true);
        } else {
            // 必须用花括号：PHP 标识符允许 \x80-\xff，
            // 故 "$page（表单" 会被整体当成变量名，诊断信息会静默丢失。
            $uncovered[] = "{$skin}/{$page}（表单 {$forms}，token {$tokens}）";
            ok(sprintf('%s/%s（%d 个表单）', $skin, $page, $forms), false,
               '表单数与 csrf 签发数不一致');
        }
    }
}

if ($uncovered) {
    echo "  未覆盖清单: " . implode(', ', $uncovered) . "\n";
}

// ── 清理 ────────────────────────────────────────────────────────────
foreach (glob($savePath . '/sess_*') ?: [] as $f) {
    @unlink($f);
}

echo "\n";
printf("通过 %d，失败 %d\n", $pass, $fail);
exit($fail ? 1 : 0);
