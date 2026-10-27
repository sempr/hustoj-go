package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sempr/hustoj-go/internal/web/config"
)

const p2SID = "p2usertest1234567890abcd"

func doP2(t *testing.T, target string, h func(*Server, http.ResponseWriter, *http.Request), session string) *httptest.ResponseRecorder {
	t.Helper()
	dir := t.TempDir()
	if session != "" {
		if err := os.WriteFile(filepath.Join(dir, "sess_"+p2SID), []byte(session), 0600); err != nil {
			t.Fatal(err)
		}
	}
	srv := newTestServer(t, &fakeQuerier{}, func(c *config.Config) {
		c.OJName = testOJName
		c.SessionBridge = true
		c.SessionDir = dir
	})
	srv.sessionBridge = NewSessionBridge(dir, testOJName)
	if session != "" {
		srv.sessions = newTestSessionStore(t)
	}
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if session != "" {
		req.AddCookie(&http.Cookie{Name: phpSessionCookieName, Value: p2SID})
	}
	rec := httptest.NewRecorder()
	h(srv, rec, req)
	return rec
}

func TestMailRequiresLogin(t *testing.T) {
	rec := doP2(t, "/mail.php", (*Server).handleMail, "")
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "请先登录") {
		t.Errorf("body should contain login prompt")
	}
}

func TestModifyRequiresLogin(t *testing.T) {
	rec := doP2(t, "/modify.php", (*Server).handleModify, "")
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "请先登录") {
		t.Errorf("body should contain login prompt")
	}
}

func TestLostPasswordGet(t *testing.T) {
	rec := doP2(t, "/lostpassword.php", (*Server).handleLostPassword, "")
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "密码重置") {
		t.Errorf("body should contain reset form")
	}
}

func TestLostPassword2Get(t *testing.T) {
	rec := doP2(t, "/lostpassword2.php", (*Server).handleLostPassword2, "")
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}
