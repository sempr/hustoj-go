<?php
/**
 * CSRF token 签发核心。
 *
 * 只负责"签发"这一个动作，输出裸 token（不含 HTML），供两类调用方复用：
 *   - csrf.php   ：渲染成 <input name="csrf"> 嵌入表单
 *   - admin-header.php：注入 window.CSRF_TOKEN，供 jQuery 自动附加到 AJAX
 *
 * 之所以拆出来：AJAX 端点（admin/ajax.php）需要把 token 作为 JS 值使用，
 * 而表单端需要 HTML 片段。共用同一份签发逻辑可避免两条路径行为漂移。
 *
 * 依赖：调用方需先引入 db_info.inc.php 与 my_func.inc.php（提供 getToken()）。
 */

/**
 * 判定当前请求是否允许签发 token。
 *
 * 仅当 Referer 缺失或与 Host 同源时签发：
 * 跨站页面若能诱导服务端签发 token，CSRF 就失去意义。
 * Referer 缺失时放行是为兼容"直接访问/刷新"等无 Referer 场景，
 * 真正的防线在校验侧——没有 token 一律拒绝。
 */
function is_same_origin_or_missing()
{
    $referer = isset($_SERVER['HTTP_REFERER']) ? (string) $_SERVER['HTTP_REFERER'] : '';
    if ($referer === '') {
        return true;
    }
    $host = isset($_SERVER['HTTP_HOST']) ? (string) $_SERVER['HTTP_HOST'] : '';
    if ($host === '') {
        return false;
    }
    // 匹配 http(s)://host[:port]/... ；IPv6 字面量整体作为一个 host
    if (preg_match('#^https?://([^:/]+|\[[^\]]+\])(?::\d+)?/#i', $referer, $m)) {
        return strcasecmp($m[1], parse_url('http://' . $host, PHP_URL_HOST) ?: $host) === 0;
    }
    return false;
}

/**
 * 签发一枚 token 并记入会话池，返回裸 token。
 *
 * 会话池保留最近 10 枚：既容忍用户多标签页/多表单并行，
 * 又让长期有效但可被嗅探的旧 token 尽快失效。
 *
 * @return string 允许签发时为 token；跨源请求时为空串
 */
function csrf_mint_token()
{
    // 必须显式声明：$OJ_NAME 是 db_info.inc.php 定义的全局变量，
    // 函数内默认看不到全局。不加这行会写进 "_csrf_keys"，
    // 与校验侧读取的 "<ojname>_csrf_keys" 对不上，表现为"签发成功但一律校验失败"。
    global $OJ_NAME;

    if (!is_same_origin_or_missing()) {
        return '';
    }
    if (!isset($_SESSION[$OJ_NAME . '_' . 'csrf_keys']) || !is_array($_SESSION[$OJ_NAME . '_' . 'csrf_keys'])) {
        $_SESSION[$OJ_NAME . '_' . 'csrf_keys'] = array();
    }
    $token = getToken();
    array_push($_SESSION[$OJ_NAME . '_' . 'csrf_keys'], $token);
    while (count($_SESSION[$OJ_NAME . '_' . 'csrf_keys']) > 10) {
        array_shift($_SESSION[$OJ_NAME . '_' . 'csrf_keys']);
    }
    return $token;
}
