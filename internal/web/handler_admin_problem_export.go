// 题目导出（admin/problem_export.php、problem_export_xml.php）与复制入口
// （admin/problem_copy.php）。
//
// 权限：administrator 或 problem_importer（Identity 未建模 importer → 仅 administrator）。
// 说明（有意简化，均已记录）：
//   - 不复刻 PHP 的临时文件 + session 进度 + `?last_file` 二次下载；Go 直接一次性
//     生成并流式返回（结果等价：用户拿到下载）。
//   - 不嵌入图片 base64、不导出 prepend/template/append 与 solution（依赖语言扩展名表）。
//   - 导出内容为 FPS XML：题面字段 + 测试用例 + SPJ 源码。

package web

import (
	"archive/zip"
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sempr/hustoj-go/pkg/dbgen"
)

type exportProblem struct {
	ID           int32
	Title        string
	TimeLimit    float64
	MemoryLimit  int32
	Description  string
	Input        string
	Output       string
	SampleInput  string
	SampleOutput string
	Hint         string
	Source       string
	RemoteOj     string
	RemoteID     string
	Spj          string
}

func (s *Server) handleAdminProblemExport(w http.ResponseWriter, r *http.Request) {
	identity := s.resolveIdentity(w, r)
	if !identity.Admin {
		s.adminLoginRequired(w)
		return
	}
	s.renderPage(w, r, http.StatusOK, "adminProblemExportPage", "题目导出", struct {
		Token *http.Request
	}{Token: r})
}

func (s *Server) handleAdminProblemExportXML(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity := s.resolveIdentity(w, r)
	if !identity.Admin {
		s.adminLoginRequired(w)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	var (
		problems []exportProblem
		filename string
	)
	switch {
	case r.FormValue("in") != "":
		var ids []string
		for _, raw := range strings.Split(r.FormValue("in"), ",") {
			if id := parseInt(raw); id > 0 {
				ids = append(ids, strconv.Itoa(id))
				if p, err := s.queries().GetProblemByID(ctx, int32(id)); err == nil {
					problems = append(problems, exportFromProblem(p))
				}
			}
		}
		filename = "-" + strings.Join(ids, ",")
	case r.URL.Query().Get("cid") != "":
		cid := int32(parseInt(r.URL.Query().Get("cid")))
		title, err := s.queries().GetContestTitle(ctx, cid)
		if err == nil {
			filename = "-" + title.String
		}
		cps, err := s.queries().ListContestProblemIDs(ctx, cid)
		if err == nil {
			for _, cp := range cps {
				if p, err := s.queries().GetProblemByID(ctx, cp.ProblemID); err == nil {
					problems = append(problems, exportFromProblem(p))
				}
			}
		}
	default:
		start := int32(parseInt(r.FormValue("start")))
		end := int32(parseInt(r.FormValue("end")))
		rows, err := s.queries().ListProblemsByRange(ctx, dbgen.ListProblemsByRangeParams{StartID: start, EndID: end})
		if err != nil {
			s.log.Error("导出查询失败", "err", err)
			http.Error(w, "导出失败", http.StatusInternalServerError)
			return
		}
		for _, row := range rows {
			problems = append(problems, exportFromRangeRow(row))
		}
		filename = "-" + strconv.Itoa(int(start)) + "-" + strconv.Itoa(int(end))
	}

	xml := s.buildFPSXML(problems, r)

	userID := identity.UserID
	stamp := time.Now().Format("20060102150405")
	if _, zipWanted := r.Form["zip"]; zipWanted {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		f, _ := zw.Create("problem.xml")
		_, _ = f.Write([]byte(xml))
		_ = zw.Close()
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", "attachment; filename=export_"+userID+filename+"_"+stamp+".zip")
		_, _ = w.Write(buf.Bytes())
		return
	}
	w.Header().Set("Content-Type", "text/xml")
	w.Header().Set("Content-Disposition", "attachment; filename=export_"+userID+filename+"_"+stamp+".xml")
	_, _ = w.Write([]byte(xml))
}

func exportFromProblem(p dbgen.Problem) exportProblem {
	return exportProblem{
		ID: p.ProblemID, Title: p.Title, TimeLimit: p.TimeLimit, MemoryLimit: p.MemoryLimit,
		Description: p.Description, Input: p.Input, Output: p.Output,
		SampleInput: p.SampleInput.String, SampleOutput: p.SampleOutput.String,
		Hint: p.Hint, Source: p.Source.String, RemoteOj: p.RemoteOj.String, RemoteID: p.RemoteID.String,
		Spj: p.Spj,
	}
}

func exportFromRangeRow(r dbgen.ListProblemsByRangeRow) exportProblem {
	return exportProblem{
		ID: r.ProblemID, Title: r.Title, TimeLimit: r.TimeLimit, MemoryLimit: r.MemoryLimit,
		Description: r.Description, Input: r.Input, Output: r.Output,
		SampleInput: r.SampleInput.String, SampleOutput: r.SampleOutput.String,
		Hint: r.Hint, Source: r.Source.String, RemoteOj: r.RemoteOj.String, RemoteID: r.RemoteID.String,
		Spj: r.Spj,
	}
}

// fixcdata 复刻 problem_export_xml.php 的 fixcdata：去掉 \x1a，转义 CDATA 结束符。
func fixcdata(s string) string {
	s = strings.ReplaceAll(s, "\x1a", "")
	return strings.ReplaceAll(s, "]]>", "]]]]><![CDATA[>")
}

func cdata(s string) string { return "<![CDATA[" + fixcdata(s) + "]]>" }

func (s *Server) buildFPSXML(problems []exportProblem, r *http.Request) string {
	var b strings.Builder
	line := func(s string) { b.WriteString(s); b.WriteString("\n") }
	line(`<?xml version="1.0" encoding="UTF-8"?>`)
	line("<!DOCTYPE fps PUBLIC")
	line(`"-//freeproblemset//An opensource XML standard for Algorithm Contest Problem Set//EN"`)
	line(`"http://hustoj.com/fps.current.dtd" >`)
	line(`<fps version="1.6" url="https://github.com/zhblue/freeproblemset/">`)
	line(`<generator name="HUSTOJ" url="https://github.com/zhblue/hustoj/" />`)

	host := r.Host
	for _, p := range problems {
		line("<item>")
		line("<title>" + cdata(p.Title) + "</title>")
		line("<url>" + cdata("http://"+host+"/problem.php?id="+strconv.Itoa(int(p.ID))) + "</url>")
		if p.TimeLimit == float64(int(p.TimeLimit)) {
			line(`<time_limit unit="s">` + cdata(strconv.Itoa(int(p.TimeLimit))) + `</time_limit>`)
		} else {
			line(`<time_limit unit="s">` + cdata(strconv.FormatFloat(p.TimeLimit, 'f', -1, 64)) + `</time_limit>`)
		}
		line(`<memory_limit unit="mb">` + cdata(strconv.Itoa(int(p.MemoryLimit))) + `</memory_limit>`)
		line("<description>" + cdata(p.Description) + "</description>")
		line("<input>" + cdata(p.Input) + "</input>")
		line("<output>" + cdata(p.Output) + "</output>")
		line("<sample_input>" + cdata(p.SampleInput) + "</sample_input>")
		line("<sample_output>" + cdata(p.SampleOutput) + "</sample_output>")
		s.writeTestCases(&b, p.ID)
		line("<hint>" + cdata(p.Hint) + "</hint>")
		line("<source>" + cdata(p.Source) + "</source>")
		line("<remote_oj>" + cdata(p.RemoteOj) + "</remote_oj>")
		line("<remote_id>" + cdata(p.RemoteID) + "</remote_id>")
		s.writeSPJ(&b, p)
		line("</item>")
	}
	line("</fps>")
	return b.String()
}

func (s *Server) writeTestCases(b *strings.Builder, pid int32) {
	dir := s.problemDataDir(pid)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".in") || name == "sample.in" {
			continue
		}
		base := strings.TrimSuffix(name, ".in")
		if data, err := os.ReadFile(filepath.Join(dir, name)); err == nil {
			b.WriteString(`<test_input name="` + base + `">` + cdata(string(data)) + "</test_input>\n")
		}
		if data, err := os.ReadFile(filepath.Join(dir, base+".out")); err == nil {
			b.WriteString(`<test_output name="` + base + `">` + cdata(string(data)) + "</test_output>\n")
		}
	}
}

// writeSPJ 复刻 spj=1/2/3 的导出：源码文件优先 .c，其次 .cc。
func (s *Server) writeSPJ(b *strings.Builder, p exportProblem) {
	dir := s.problemDataDir(p.ID)
	readFirst := func(names ...string) (string, bool) {
		for _, n := range names {
			if data, err := os.ReadFile(filepath.Join(dir, n)); err == nil {
				return string(data), true
			}
		}
		return "", false
	}
	switch p.Spj {
	case "1":
		if src, ok := readFirst("spj.c"); ok {
			b.WriteString("<spj language=\"C\">" + cdata(src) + "</spj>\n")
		} else if src, ok := readFirst("spj.cc"); ok {
			b.WriteString("<spj language=\"C++\">" + cdata(src) + "</spj>\n")
		}
		if src, ok := readFirst("tpj.c"); ok {
			b.WriteString("<tpj language=\"C\">" + cdata(src) + "</tpj>\n")
		} else if src, ok := readFirst("tpj.cc"); ok {
			b.WriteString("<tpj language=\"C++\">" + cdata(src) + "</tpj>\n")
		}
	case "2":
		b.WriteString("<spj language=\"Text\">text judge</spj>\n")
	case "3":
		if src, ok := readFirst("interactor.cc"); ok {
			b.WriteString("<interactor language=\"C++\">" + cdata(src) + "</interactor>\n")
		}
	}
}

// ── admin/problem_copy.php ───────────────────────────────────────────

func (s *Server) handleAdminProblemCopy(w http.ResponseWriter, r *http.Request) {
	identity := s.resolveIdentity(w, r)
	if !identity.Admin {
		s.adminLoginRequired(w)
		return
	}
	type src struct {
		Action, Placeholder string
	}
	s.renderPage(w, r, http.StatusOK, "adminProblemCopyPage", "复制题目", struct {
		Sources []src
		Token   *http.Request
	}{
		Sources: []src{
			{Action: "problem_add_page_uoj.php", Placeholder: "http://uoj.ac/problem/1"},
			{Action: "problem_add_page_hustoj.php", Placeholder: "http://hustoj.com/oj/problem.php?id=1000"},
			{Action: "problem_add_page_luogu.php", Placeholder: "https://www.luogu.com.cn/problem/P1000"},
			{Action: "problem_add_page_loj.php", Placeholder: "https://loj.ac/problem/"},
			{Action: "problem_add_page_waterloo.php", Placeholder: ""},
			{Action: "problem_add_page_pku.php", Placeholder: "http://poj.org/problem?id=1000"},
			{Action: "problem_add_page_hdu.php", Placeholder: "https://acm.hdu.edu.cn/showproblem.php?pid=1172"},
			{Action: "problem_add_page_zju.php", Placeholder: ""},
		},
		Token: r,
	})
}

func (s *Server) registerAdminProblemExportRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/problem_export.php", s.handleAdminProblemExport)
	mux.HandleFunc("POST /admin/problem_export_xml.php", s.handleAdminProblemExportXML)
	mux.HandleFunc("GET /admin/problem_export_xml.php", s.handleAdminProblemExportXML)
	mux.HandleFunc("GET /admin/problem_copy.php", s.handleAdminProblemCopy)
}
