package web

import (
	"context"
	"regexp"
	"testing"
	"time"
)

const testOJName = "吉尔吉斯倔驴大学OJ"

var (
	reGoUser  = regexp.MustCompile(`_user_id\|s:\d+:"([^"]*)"`)
	reGoAdmin = regexp.MustCompile(`_administrator\|b:1`)
	reGoSB    = regexp.MustCompile(`_source_browser\|b:1`)
	reGoPE    = regexp.MustCompile(`_problem_editor\|b:1`)
	reGoCC    = regexp.MustCompile(`_contest_creator\|b:1`)
	reGoPV    = regexp.MustCompile(`_problem_verifiter\|b:1`)
)

func goSessionFromBits(bits string) GoSession {
	gs := GoSession{UserID: "admin", Nick: "PHP", CreatedAt: time.Now().Unix()}
	if m := reGoUser.FindStringSubmatch(bits); len(m) == 2 {
		gs.UserID = m[1]
	}
	gs.Privileges = GoPrivileges{
		Admin:           reGoAdmin.MatchString(bits),
		SourceBrowser:   reGoSB.MatchString(bits),
		ProblemEditor:   reGoPE.MatchString(bits),
		ContestCreator:  reGoCC.MatchString(bits),
		ProblemVerifier: reGoPV.MatchString(bits),
	}
	return gs
}

func seedGoSessionFromBits(t *testing.T, srv *Server, sid, bits string) {
	t.Helper()
	if srv.sessions == nil {
		srv.sessions = newTestSessionStore(t)
	}
	gs := goSessionFromBits(bits)
	if err := srv.sessions.Save(context.Background(), sid, &gs); err != nil {
		t.Fatalf("seed Go session %s: %v", sid, err)
	}
}
