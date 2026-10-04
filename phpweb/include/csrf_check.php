<?php
/**
 * CSRF token 校验（单次消费）。
 *
 * 由 include/csrf_check.php 的调用方在处理 POST 前引入。
 * 约定：POST 请求必须携带有效 csrf 字段，否则 403；校验通过后该 token 立即作废。
 *
 * 需要可重复校验的 AJAX 端点请改用 include/csrf_check_ajax.php。
 *
 * 注意：本文件依赖 $OJ_NAME（session 键前缀）。调用方需先引入 db_info.inc.php。
 */
require_once(__DIR__ . '/csrf_check_bootstrap.php');

csrf_require_session();

$__csrf_key = (isset($OJ_NAME) ? $OJ_NAME : '') . '_' . 'csrf_keys';

if (isset($_SERVER['REQUEST_METHOD']) && strtoupper($_SERVER['REQUEST_METHOD']) === 'POST') {
    // token 必须是标量字符串：拒绝 csrf[]= 之类的数组参数
    if (!isset($_POST['csrf']) || !is_string($_POST['csrf']) || $_POST['csrf'] === '') {
        csrf_fail();
    }
    if (!isset($_SESSION[$__csrf_key]) || !is_array($_SESSION[$__csrf_key])) {
        csrf_fail();
    }

    // 严格比较，避免松散比较下的类型混淆
    $index = array_search($_POST['csrf'], $_SESSION[$__csrf_key], true);
    if ($index === false) {
        csrf_fail();
    }

    // 单次消费：校验通过的 token 立即从会话中移除
    array_splice($_SESSION[$__csrf_key], $index, 1);
}

unset($__csrf_key);
