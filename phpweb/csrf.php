<?php
/**
 * 渲染表单用的 csrf hidden input。
 *
 * 签发逻辑见 include/csrf_token.php。
 */
require_once("include/db_info.inc.php");
require_once("include/my_func.inc.php");
require_once("include/csrf_token.php");

$token = csrf_mint_token();
if ($token !== '') {
    ?><input type="hidden" name="csrf" value="<?php echo htmlspecialchars($token, ENT_QUOTES, 'UTF-8') ?>"><?php
}
