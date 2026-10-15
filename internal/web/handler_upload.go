// 编辑器上传端点（原 PHP kindeditor/php/upload_json.php）。
//
// 纯 Go 环境不再依赖 PHP-FPM：Go 自己落盘到 <OJHome>/src/web/upload/
// （nginx 各域名的静态根），以 JSON 返回 /upload/... 地址，供 Markdown
// 编辑器在光标处插入图片/文件链接。
// 扩展名白名单沿用 PHP：只允许图片/媒体/文件三类，从不产出 .php，避免上传即执行。
package web

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// uploadExtensions 对应 PHP 的 $ext_arr：按扩展名决定存进哪个子目录。
var uploadExtensions = map[string][]string{
	"image": {"gif", "jpg", "jpeg", "png", "bmp"},
	"flash": {"swf", "flv"},
	"media": {"swf", "flv", "mp3", "wav", "wma", "wmv", "mid", "avi", "mpg", "asf", "rm", "rmvb", "mp4"},
	"file":  {"pdf", "doc", "docx", "xls", "xlsx", "ppt", "txt", "zip", "rar", "gz", "bz2"},
}

// uploadMaxBytes 对应 PHP 的 400MB 上限（nginx client_max_body_size 500m）。
const uploadMaxBytes = 400 << 20

// uploadDir 返回上传文件的落盘目录；未配置时按 OJHome 推导。
//
// 该目录就是 nginx 的静态根，因此上传结果对 oj / oj-go / oj-php 都直接可读，
// 无需 Go 再提供文件服务。测试把 OJHome 指到临时目录即可隔离。
func (s *Server) uploadDir() string {
	if s.cfg.UploadDir != "" {
		return s.cfg.UploadDir
	}
	return filepath.Join(s.cfg.OJHome, "src", "web", "upload")
}

// uploadResponse 是上传接口的返回体。
//
// error=0 表示成功（与 PHP 的 array('error'=>0,'url'=>...,...) 对齐）；
// 失败时给 error=1 并附 message，便于在编辑器控制台看到原因。
type uploadResponse struct {
	Error   int    `json:"error"`
	URL     string `json:"url,omitempty"`
	Save    string `json:"save,omitempty"`
	Message string `json:"message,omitempty"`
}

func writeUploadJSON(w http.ResponseWriter, status int, resp uploadResponse) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(resp)
}

// uploadCanAccess 对应 PHP 的角色判定：administrator / contest_creator /
// problem_editor。
func uploadCanAccess(id Identity) bool {
	return id.Admin || id.ContestCreator || id.ProblemEditor
}

func (s *Server) handleUploadJSON(w http.ResponseWriter, r *http.Request) {
	identity := s.resolveIdentity(w, r)
	if !uploadCanAccess(identity) {
		writeUploadJSON(w, http.StatusForbidden, uploadResponse{Error: 1, Message: "Login First!"})
		return
	}
	if r.Method != http.MethodPost {
		writeUploadJSON(w, http.StatusMethodNotAllowed, uploadResponse{Error: 1, Message: "POST only"})
		return
	}

	if err := r.ParseMultipartForm(uploadMaxBytes); err != nil {
		writeUploadJSON(w, http.StatusBadRequest, uploadResponse{Error: 1, Message: "无法解析上传数据"})
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeUploadJSON(w, http.StatusBadRequest, uploadResponse{Error: 1, Message: "请选择图片。"})
		return
	}
	defer file.Close()

	if header.Size > uploadMaxBytes {
		writeUploadJSON(w, http.StatusBadRequest, uploadResponse{Error: 1, Message: "上传文件大小超过限制。"})
		return
	}

	// 目录按扩展名归类，扩展名必须命中白名单（杜绝 .php 之类）。
	name := filepath.Base(header.Filename)
	dot := strings.LastIndex(name, ".")
	if dot < 0 {
		writeUploadJSON(w, http.StatusBadRequest, uploadResponse{Error: 1, Message: "文件名不正确。"})
		return
	}
	ext := strings.ToLower(strings.TrimSpace(name[dot+1:]))
	dirName := ""
	for k, allowed := range uploadExtensions {
		for _, a := range allowed {
			if a == ext {
				dirName = k
				break
			}
		}
		if dirName != "" {
			break
		}
	}
	if dirName == "" {
		writeUploadJSON(w, http.StatusBadRequest, uploadResponse{Error: 1, Message: "上传文件扩展名是不允许的扩展名。"})
		return
	}

	ymd := time.Now().Format("20060102")
	dir := filepath.Join(s.uploadDir(), dirName, ymd)
	if err := os.MkdirAll(dir, 0744); err != nil {
		writeUploadJSON(w, http.StatusInternalServerError, uploadResponse{Error: 1, Message: "创建目录失败。"})
		return
	}

	// 与 PHP 一致：保留原文件名，扩展名统一小写（已过白名单）。
	saveName := name[:dot] + "." + ext
	dst := filepath.Join(dir, saveName)

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		writeUploadJSON(w, http.StatusInternalServerError, uploadResponse{Error: 1, Message: "上传文件失败。"})
		return
	}
	if _, err := io.Copy(out, file); err != nil {
		out.Close()
		writeUploadJSON(w, http.StatusInternalServerError, uploadResponse{Error: 1, Message: "上传文件失败。"})
		return
	}
	out.Close()

	writeUploadJSON(w, http.StatusOK, uploadResponse{
		Error: 0,
		URL:   "/upload/" + dirName + "/" + ymd + "/" + saveName,
		Save:  saveName,
	})
}

// handleMarkdownPreview 是编辑器实时预览接口：把提交的 Markdown 用与题面
// 完全相同的 goldmark 转成 HTML 片段返回，保证所见即所得。只读、无副作用，
// 在 csrf.go 里豁免 CSRF；接口本身要求编辑器角色身份。
func (s *Server) handleMarkdownPreview(w http.ResponseWriter, r *http.Request) {
	identity := s.resolveIdentity(w, r)
	if !uploadCanAccess(identity) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	text := r.FormValue("text")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(markdownString(text)))
}

// registerUploadRoutes 注册编辑器相关接口（/assets/ 由 Go 处理，无需 nginx 规则）。
func (s *Server) registerUploadRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /assets/editor/upload.php", s.handleUploadJSON)
	mux.HandleFunc("GET /assets/editor/upload.php", s.handleUploadJSON)
	mux.HandleFunc("POST /admin/markdown_preview.php", s.handleMarkdownPreview)
}
