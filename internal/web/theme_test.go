package web_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func themeCookies(res *httptest.ResponseRecorder) []*http.Cookie {
	var out []*http.Cookie
	for _, c := range res.Result().Cookies() {

		if c.Name == "kassa_theme" {
			out = append(out, c)
		}
	}
	return out
}

func TestEachPhoneChoosesLightDarkOrSystemTheme(t *testing.T) {
	s := newSite(t)
	session := s.login("olga", olgaPass)

	page := s.get("/profile", session)
	wantPage(t, "profile by default", page, http.StatusOK, "Оформление", `name="theme" value="light"`, `name="theme" value="dark"`, `name="theme" value="system"`, `value="system" aria-pressed="true"`)
	if strings.Contains(page.Body.String(), "data-theme=") {
		t.Fatal("without a choice the page must follow the phone's setting")
	}

	for _, tc := range []struct{ choice, attr, scheme string }{
		{"dark", `data-theme="aesthetic-dark"`, `content="dark"`},
		{"light", `data-theme="aesthetic"`, `content="light"`},
	} {
		res := s.post("/profile/theme", url.Values{"theme": {tc.choice}}, session)
		wantRedirect(t, tc.choice, res, "/profile")
		c := themeCookies(res)
		if len(c) != 1 || c[0].Value != tc.choice || c[0].MaxAge <= 0 || !c[0].Secure || c[0].Path != "/" {
			t.Fatalf("%s: theme cookie %+v", tc.choice, c)
		}
		chosen := append(append([]*http.Cookie{}, session...), c...)
		wantPage(t, tc.choice+" profile", s.get("/profile", chosen), http.StatusOK, tc.attr, `name="color-scheme" `+tc.scheme, `value="`+tc.choice+`" aria-pressed="true"`)
		wantPage(t, tc.choice+" login", s.get("/login", c), http.StatusOK, tc.attr)
	}

	res := s.post("/profile/theme", url.Values{"theme": {"system"}}, session)
	wantRedirect(t, "system", res, "/profile")
	if c := themeCookies(res); len(c) != 1 || c[0].MaxAge >= 0 {
		t.Fatalf("system must forget the choice: %+v", c)
	}

	wantPage(t, "unknown theme", s.post("/profile/theme", url.Values{"theme": {"pink"}}, session), http.StatusBadRequest)
	if body := s.get("/profile", append(session, &http.Cookie{Name: "kassa_theme", Value: `x" onload="`, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})).Body.String(); strings.Contains(body, "data-theme=") {
		t.Fatal("an unknown cookie value must be ignored")
	}
	wantRedirect(t, "signed out", s.post("/profile/theme", url.Values{"theme": {"dark"}}, nil), "/login")
}
