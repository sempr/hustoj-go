// 后台批量建号（admin/user_add.php）与 CSV 导入（admin/user_import.php）。
//
// 权限：PHP 为 administrator 或 user_adder；Identity 不建模 user_adder → 仅 administrator
// （已知限制，同 changepass 的 password_setter）。
//
// 到期天数：PHP 用 $OJ_EXPIRY_DAYS（缺省 365）。Go 侧该值不在 web.toml，取默认 365，
// 与 PHP 缺省一致；若运维在 db_info.inc.php 改了它，需同步（迁移期两端各一份）。

package web

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"golang.org/x/text/encoding/simplifiedchinese"

	"github.com/sempr/hustoj-go/pkg/dbgen"
)

const defaultExpiryDays = 365

type adminUserAddVM struct {
	Messages []string
	UList    string
	Token    *http.Request
}

type adminUserImportVM struct {
	Messages []string
	Token    *http.Request
}

// cleanImportLine 复刻 `preg_replace("/[^\x20-\x7e]/", " ", ...)`：把非可打印 ASCII
// 字节替换成空格（因此中文用户名会被打散）。
func cleanImportLine(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c < 0x20 || c > 0x7e {
			b[i] = ' '
		}
	}
	return string(b)
}

func (s *Server) handleAdminUserAdd(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity := s.resolveIdentity(w, r)
	if !identity.Admin {
		s.adminLoginRequired(w)
		return
	}

	vm := adminUserAddVM{Token: r}
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}
		lines := strings.Split(strings.TrimSpace(r.FormValue("ulist")), "\n")
		var remained string
		processed := len(lines) > 0 && len(lines[0]) > 0
		if processed {
			for _, line := range lines {
				// PHP 用 explode(" ", trim(...))：连续空格会产生空 token → 行格式错误。
				tokens := strings.Split(strings.TrimSpace(cleanImportLine(line)), " ")
				if len(tokens) != 2 {
					vm.Messages = append(vm.Messages, token0(tokens)+" ... Error : Line format error!")
					remained += strings.Join(tokens, " ") + "\n"
					continue
				}
				uid, pw := tokens[0], tokens[1]
				if _, err := s.queries().GetUserByID(ctx, uid); err == nil {
					vm.Messages = append(vm.Messages, uid+" ... Error : User already exist!")
					remained += uid + " " + pw + "\n"
					continue
				} else if !errors.Is(err, sql.ErrNoRows) {
					s.log.Error("查询用户失败", "err", err)
					continue
				}
				hash, err := pwGen(pw, false)
				if err != nil {
					s.log.Error("生成密码哈希失败", "err", err)
					continue
				}
				if err := s.queries().InsertBulkUser(ctx, dbgen.InsertBulkUserParams{
					UserID:     uid,
					Password:   sql.NullString{String: hash, Valid: true},
					ExpiryDate: time.Now().AddDate(0, 0, defaultExpiryDays),
					Nick:       uid,
				}); err != nil {
					s.log.Error("建用户失败", "err", err)
					continue
				}
				_ = s.queries().InsertLoginLog(ctx, dbgen.InsertLoginLogParams{
					UserID:   uid,
					Password: sql.NullString{String: "user added", Valid: true},
					Ip:       sql.NullString{String: s.remoteIP(r), Valid: true},
				})
				vm.Messages = append(vm.Messages, uid+" is added!")
			}
			vm.Messages = append(vm.Messages, "Remained lines have error!")
		}
		vm.UList = strings.TrimSpace(remained)
	}

	s.renderPage(w, r, http.StatusOK, "adminUserAddPage", "添加用户", vm)
}

func token0(tokens []string) string {
	if len(tokens) > 0 {
		return tokens[0]
	}
	return ""
}

// ── admin/user_import.php ────────────────────────────────────────────

func (s *Server) handleAdminUserImport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity := s.resolveIdentity(w, r)
	if !identity.Admin {
		s.adminLoginRequired(w)
		return
	}

	vm := adminUserImportVM{Token: r}
	if r.Method == http.MethodPost {
		file, header, err := r.FormFile("fps")
		if err != nil {
			vm.Messages = append(vm.Messages, "上传失败: "+err.Error())
			s.renderPage(w, r, http.StatusOK, "adminUserImportPage", "导入用户", vm)
			return
		}
		defer file.Close()
		raw, err := io.ReadAll(file)
		if err != nil {
			vm.Messages = append(vm.Messages, "读取上传文件失败: "+err.Error())
			s.renderPage(w, r, http.StatusOK, "adminUserImportPage", "导入用户", vm)
			return
		}

		if strings.EqualFold(extOf(header.Filename), "zip") {
			zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
			if err != nil {
				vm.Messages = append(vm.Messages, "zip 解析失败: "+err.Error())
			} else {
				for _, zf := range zr.File {
					if zf.FileInfo().IsDir() {
						continue
					}
					rc, err := zf.Open()
					if err != nil {
						continue
					}
					data, _ := io.ReadAll(rc)
					rc.Close()
					vm.Messages = append(vm.Messages, s.importUsersCSV(ctx, r, bytes.NewReader(data))...)
				}
			}
		} else {
			vm.Messages = append(vm.Messages, s.importUsersCSV(ctx, r, bytes.NewReader(raw))...)
		}
	}

	s.renderPage(w, r, http.StatusOK, "adminUserImportPage", "导入用户", vm)
}

// importUsersCSV 解析一份 CSV 并逐行 upsert 用户。返回每行的结果消息。
func (s *Server) importUsersCSV(ctx context.Context, r *http.Request, reader io.Reader) []string {
	data, err := io.ReadAll(reader)
	if err != nil {
		return []string{"读取失败: " + err.Error()}
	}
	// 跳过 UTF-8 BOM。
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})

	cr := csv.NewReader(bytes.NewReader(data))
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = true

	var messages []string
	checked, gbk := false, false
	for {
		row, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			messages = append(messages, "CSV 解析错误: "+err.Error())
			break
		}
		if !checked {
			if len(row) > 0 && row[0] == "学号" {
				checked = true
				continue
			}
			if len(row) > 0 && gbkDecode(row[0]) == "学号" {
				checked, gbk = true, true
				continue
			}
			messages = append(messages, "请用下载的模板填写，保存为UTF-8编码。")
			break
		}
		messages = append(messages, s.importOneUser(ctx, r, row, gbk)...)
	}
	return messages
}

func (s *Server) importOneUser(ctx context.Context, r *http.Request, row []string, gbk bool) []string {
	if len(row) < 3 {
		return nil
	}
	userID := strings.TrimSpace(row[0])
	nick := row[1]
	password := strings.TrimSpace(row[2])
	school, email, group := "", "", ""
	expiry := time.Now().AddDate(0, 0, defaultExpiryDays)
	if len(row) > 3 {
		school = row[3]
	}
	if len(row) > 4 {
		email = row[4]
	}
	if len(row) > 5 {
		group = row[5]
	}
	expiryStr := ""
	if len(row) > 6 {
		expiryStr = row[6]
	}
	if gbk {
		nick = gbkDecode(nick)
		school = gbkDecode(school)
		group = gbkDecode(group)
		expiryStr = gbkDecode(expiryStr)
	}
	if expiryStr != "" {
		if !isDateString(expiryStr) {
			if n, err := parseIntStrict(expiryStr); err == nil {
				expiry = time.Now().AddDate(0, 0, n)
			}
		} else if t, err := time.Parse("2006-01-02", expiryStr); err == nil {
			expiry = t
		}
	}
	// 昵称超长时动态加宽 nick 列（PHP 同款 ALTER）。
	if n := len([]rune(nick)); n > 20 {
		if _, err := s.db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE `users` MODIFY COLUMN `nick` varchar(%d) NULL DEFAULT ''", n)); err != nil {
			s.log.Error("加宽 nick 列失败", "err", err)
		}
	}

	hash, err := pwGen(password, false)
	if err != nil {
		return []string{userID + " : 密码哈希失败: " + err.Error()}
	}
	err = s.queries().UpsertImportUser(ctx, dbgen.UpsertImportUserParams{
		UserID:     userID,
		Email:      sql.NullString{String: email, Valid: email != ""},
		Ip:         s.remoteIP(r),
		Password:   sql.NullString{String: hash, Valid: true},
		Nick:       nick,
		School:     school,
		GroupName:  group,
		ExpiryDate: expiry,
	})
	if err != nil {
		s.log.Error("导入用户失败", "err", err, "user_id", userID)
		return []string{userID + " : 导入失败"}
	}
	return []string{userID + " : " + expiry.Format("2006-01-02") + " : 1"}
}

var dateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

func isDateString(s string) bool { return dateRe.MatchString(s) }

func parseIntStrict(s string) (int, error) {
	var n int
	_, err := fmt.Sscanf(strings.TrimSpace(s), "%d", &n)
	return n, err
}

func gbkDecode(s string) string {
	if out, err := simplifiedchinese.GBK.NewDecoder().String(s); err == nil {
		return out
	}
	return s
}

func extOf(name string) string {
	if i := strings.LastIndex(name, "."); i >= 0 {
		return name[i+1:]
	}
	return ""
}

func (s *Server) registerAdminUserImportRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/user_add.php", s.handleAdminUserAdd)
	mux.HandleFunc("POST /admin/user_add.php", s.handleAdminUserAdd)
	mux.HandleFunc("GET /admin/user_import.php", s.handleAdminUserImport)
	mux.HandleFunc("POST /admin/user_import.php", s.handleAdminUserImport)
}
