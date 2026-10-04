// 批量删除题目（admin/problem_batch_delete.php 的事务化重写）。
//
// PHP 版本逐条 DELETE/UPDATE，无事务，中途失败会留下不一致状态。
// Go 版本：
//   - 单次事务包裹所有 DELETE/UPDATE（contest_problem、problem、privilege、solution）
//   - 文件系统清理在事务外完成（事务提交后），失败记录日志但不回滚 DB
//   - 单次最多 200 条（与 PHP 一致）
//   - 返回删除计数与失败详情

package web

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const maxBatchDelete = 200 // 与 PHP 保持一致

// BatchDeleteRequest 批量删除请求。
type BatchDeleteRequest struct {
	PIDs  []int32 `json:"pid"`   // pid[] 数组
	Hlist string  `json:"hlist"` // 逗号分隔
	CSRF  string  `json:"csrf"`
}

// BatchDeleteResponse 批量删除响应。
type BatchDeleteResponse struct {
	Success bool     `json:"success"`
	Deleted int      `json:"deleted"`
	Failed  []string `json:"failed,omitempty"`
	Message string   `json:"message"`
}

// handleBatchDeleteProblem 处理 POST /admin/problem_batch_delete.php。
func (s *Server) handleBatchDeleteProblem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 管理员校验
	id := s.resolveIdentity(w, r)
	if !id.Admin {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// CSRF 由 wrapCSRF 中间件统一校验：nosurf 对所有非安全方法
	// （POST/PUT/DELETE…）强制检查 Origin/Referer 与双提交 token。
	//
	// 这里不再重复校验 —— 旧的 validateCSRF 查的是 PHP 会话里的
	// csrf_keys 池，与 nosurf 无关；留着会让"没带 csrf 字段也能过"
	// 与"必须带 nosurf token"两套规则并存，排查时互相干扰。

	// 解析参数
	var req BatchDeleteRequest
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}
	req.CSRF = r.FormValue("csrf")
	req.Hlist = r.FormValue("hlist")
	for _, v := range r.Form["pid"] {
		if id, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && id > 0 {
			req.PIDs = append(req.PIDs, int32(id))
		}
	}
	if req.Hlist != "" {
		for _, part := range strings.Split(req.Hlist, ",") {
			if id, err := strconv.Atoi(strings.TrimSpace(part)); err == nil && id > 0 {
				req.PIDs = append(req.PIDs, int32(id))
			}
		}
	}
	// 去重
	seen := make(map[int32]bool)
	unique := make([]int32, 0, len(req.PIDs))
	for _, id := range req.PIDs {
		if !seen[id] {
			seen[id] = true
			unique = append(unique, id)
		}
	}
	req.PIDs = unique

	if len(req.PIDs) == 0 {
		// 无效请求，重定向到列表页（模拟 PHP 行为）
		http.Redirect(w, r, "/admin/problem_list.php", http.StatusSeeOther)
		return
	}
	// 限制批次大小
	if len(req.PIDs) > maxBatchDelete {
		req.PIDs = req.PIDs[:maxBatchDelete]
	}

	// 执行批量删除
	resp := s.batchDeleteProblems(r.Context(), req.PIDs)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if resp.Success {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusInternalServerError)
	}
	// 返回 JSON（PHP 版本返回 HTML，这里为了 API 化返回 JSON，前端可据此刷新）
	// 兼容性：也支持 HTML 重定向模式
	if r.Header.Get("Accept") == "text/html" || r.FormValue("format") == "html" {
		if resp.Success {
			http.Redirect(w, r, "/admin/problem_list.php?deleted="+strconv.Itoa(resp.Deleted), http.StatusSeeOther)
		} else {
			http.Error(w, "删除失败: "+strings.Join(resp.Failed, "; "), http.StatusInternalServerError)
		}
		return
	}
	// JSON 响应
	_ = json.NewEncoder(w).Encode(resp)
}

// batchDeleteProblems 在单一事务中批量删除题目。
//
// 步骤：
//  1. 开启事务
//  2. 逐题检查存在性，构建待删 ID 列表
//  3. 批量删除 contest_problem、privilege
//  4. 批量 UPDATE solution SET problem_id=0, result=13
//  5. 批量 DELETE FROM problem
//  6. 提交事务
//  7. 事务外清理文件系统（OJ_DATA/<id>/）
//  8. 最后 ALTER TABLE problem AUTO_INCREMENT = next_id
func (s *Server) batchDeleteProblems(ctx context.Context, pids []int32) BatchDeleteResponse {
	// 1. 检查存在性
	var existing []int32
	for _, pid := range pids {
		row := s.db.QueryRowContext(ctx, "SELECT problem_id FROM problem WHERE problem_id = ?", pid)
		var exists int32
		if err := row.Scan(&exists); err == nil {
			existing = append(existing, pid)
		}
	}
	if len(existing) == 0 {
		return BatchDeleteResponse{
			Success: false,
			Message: "未找到任何有效题目",
		}
	}

	// 2. 事务内删除
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		s.log.Error("开启事务失败", "err", err)
		return BatchDeleteResponse{Success: false, Message: "事务开启失败"}
	}
	defer func() {
		if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
			s.log.Error("事务回滚失败", "err", err)
		}
	}()

	// 2.1 删除 contest_problem
	if _, err := tx.ExecContext(ctx,
		"DELETE FROM contest_problem WHERE problem_id IN ("+placeholders(len(existing))+")",
		toArgs(existing)...); err != nil {
		s.log.Error("删除 contest_problem 失败", "err", err)
		return BatchDeleteResponse{Success: false, Message: "删除关联失败"}
	}

	// 2.2 删除 privilege (p<id>)
	if _, err := tx.ExecContext(ctx,
		"DELETE FROM privilege WHERE rightstr IN ("+rightstrPlaceholders(len(existing))+")",
		rightstrArgs(existing)...); err != nil {
		s.log.Error("删除 privilege 失败", "err", err)
		return BatchDeleteResponse{Success: false, Message: "删除权限失败"}
	}

	// 2.3 更新 solution
	if _, err := tx.ExecContext(ctx,
		"UPDATE solution SET problem_id = 0, result = 13 WHERE problem_id IN ("+placeholders(len(existing))+")",
		toArgs(existing)...); err != nil {
		s.log.Error("更新 solution 失败", "err", err)
		return BatchDeleteResponse{Success: false, Message: "更新提交记录失败"}
	}

	// 2.4 删除 problem
	if _, err := tx.ExecContext(ctx,
		"DELETE FROM problem WHERE problem_id IN ("+placeholders(len(existing))+")",
		toArgs(existing)...); err != nil {
		s.log.Error("删除 problem 失败", "err", err)
		return BatchDeleteResponse{Success: false, Message: "删除题目失败"}
	}

	if err := tx.Commit(); err != nil {
		s.log.Error("事务提交失败", "err", err)
		return BatchDeleteResponse{Success: false, Message: "事务提交失败"}
	}

	// 3. 文件系统清理（事务外，失败不回滚 DB）
	failedFS := make([]string, 0)
	ojData := s.cfg.OJHome + "/data"
	for _, pid := range existing {
		dir := filepath.Join(ojData, strconv.Itoa(int(pid)))
		// 安全检查：确保路径在 OJ_DATA 下，且 ID 为纯数字
		if !strings.HasPrefix(dir, ojData) || !isNumeric(strconv.Itoa(int(pid))) {
			s.log.Warn("跳过非法数据目录", "dir", dir)
			continue
		}
		if err := os.RemoveAll(dir); err != nil {
			s.log.Error("删除数据目录失败", "dir", dir, "err", err)
			failedFS = append(failedFS, strconv.Itoa(int(pid)))
		}
	}

	// 4. 重置 AUTO_INCREMENT（DDL 隐式提交，单独执行）
	var maxID int32
	row := s.db.QueryRowContext(ctx, "SELECT COALESCE(MAX(problem_id), 0) FROM problem")
	if err := row.Scan(&maxID); err == nil {
		nextID := maxID + 1
		if nextID < 1000 {
			nextID = 1000
		}
		_, _ = s.db.ExecContext(ctx, "ALTER TABLE problem AUTO_INCREMENT = ?", nextID)
	}

	deleted := len(existing)
	return BatchDeleteResponse{
		Success: true,
		Deleted: deleted,
		Failed:  failedFS,
		Message: fmt.Sprintf("成功删除 %d 道题目", deleted),
	}
}

// placeholders 生成 ?,?,... 占位符字符串。
func placeholders(n int) string {
	if n <= 0 {
		return "?"
	}
	return strings.Repeat("?,", n)[:2*n-1]
}

// toArgs 将 []int32 转为 []interface{}。
func toArgs(ids []int32) []interface{} {
	args := make([]interface{}, len(ids))
	for i, v := range ids {
		args[i] = v
	}
	return args
}

// rightstrPlaceholders 生成 p1,p2,... 占位符。
func rightstrPlaceholders(n int) string {
	if n <= 0 {
		return "?"
	}
	parts := make([]string, n)
	for i := range parts {
		parts[i] = "?"
	}
	return strings.Join(parts, ",")
}

// rightstrArgs 生成 p<id> 参数。
func rightstrArgs(ids []int32) []interface{} {
	args := make([]interface{}, len(ids))
	for i, v := range ids {
		args[i] = "p" + strconv.Itoa(int(v))
	}
	return args
}

func isNumeric(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(s) > 0
}

// registerAdminRoutes 挂载管理后台路由。
func (s *Server) registerAdminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /admin/problem_batch_delete.php", s.handleBatchDeleteProblem)
}
