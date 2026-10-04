<?php
/**
 * csrf_check.php 与 csrf_check_ajax.php 的公共前置：
 * 会话就绪检查 + 统一的失败响应。
 *
 * 注意：本文件必须在 db_info.inc.php 之后引入（依赖 $OJ_NAME）。
 */

/**
 * 统一的 CSRF 校验失败响应：403 + 固定文本，随后终止请求。
 *
 * 响应体不含任何回显信息，避免把会话 token 试探结果泄露给攻击者。
 */
function csrf_fail()
{
    if (!headers_sent()) {
        http_response_code(403);
        header('Content-Type: text/plain; charset=utf-8');
    }
    echo 'Invalid csrf token';
    exit;
}

/**
 * 确保会话可用。
 *
 * 会话起不来属于真实故障（目录权限、session.save_path 不可写等），
 * 此时不能返回 403 —— 那会把"站点坏了"伪装成"CSRF 攻击"，误导排查。
 */
function csrf_require_session()
{
    if (session_status() === PHP_SESSION_NONE) {
        // 不再用 @ 抑制：会话无法启动应当显式失败。
        if (!@session_start()) {
            if (!headers_sent()) {
                http_response_code(500);
                header('Content-Type: text/plain; charset=utf-8');
            }
            echo 'Session unavailable';
            exit;
        }
    }
}
