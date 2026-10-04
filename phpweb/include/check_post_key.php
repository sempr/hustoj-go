<?php
/**
 * postkey 校验。
 *
 * 由 include/set_post_key.php 在表单中签发，对应校验方共 37 个端点
 * （admin/* 与根级写操作）。校验通过后立即作废（单次消费）。
 *
 * 响应契约（勿改）：失败时输出 "post key fail..." 并 exit(1)。
 */
$__pk_session = $OJ_NAME . '_' . 'postkey';
$__pk_ok = false;

if (isset($_SESSION[$__pk_session]) && is_string($_SESSION[$__pk_session])
    && $__pk_session !== ''
    && isset($_POST['postkey']) && is_string($_POST['postkey'])) {
    // 定长比较，避免按字节提前返回而泄露 token 前缀
    $__pk_ok = hash_equals($_SESSION[$__pk_session], $_POST['postkey']);
}

if (!$__pk_ok) {
    http_response_code(403);
    echo "post key fail...";
    exit(1);
}

unset($_SESSION[$__pk_session]);
