<?php
/**
 * CSRF token 校验（AJAX 版，不消费 token）。
 *
 * 与 include/csrf_check.php 的唯一区别：不从会话池中移除 token。
 *
 * 为什么要单列一个变体：csrf_check.php 是单次消费的（更严格），
 * 但 AJAX 页面天然会在一次页面生命周期内发起多次请求
 * （user_list.php / news_list.php / problem_list.php 的表格内联编辑），
 * 消费掉 token 后第二次内联编辑就会被 403，功能直接不可用。
 * 因此 AJAX 端点改用"可重复校验 + 10 枚池上限"的强度，
 * 与浏览器自动附加 header 的传统做法量级相当。
 *
 * 依赖：调用方需先引入 db_info.inc.php（提供 $OJ_NAME 与会话）。
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
    if (array_search($_POST['csrf'], $_SESSION[$__csrf_key], true) === false) {
        csrf_fail();
    }
}

unset($__csrf_key);
