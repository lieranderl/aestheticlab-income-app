package web_test

import (
	"strings"
	"testing"
)

// 2026-11-15 design review: every view switcher (month, history, category
// kind, trend period, movement type) is the same full-width tab box.
func TestViewSwitchersShareOneTabStyle(t *testing.T) {
	s := newSite(t)
	admin := s.login("anna", annaPass)
	for _, path := range []string{"/admin/month", "/admin/history", "/admin/categories", "/admin/trends", "/admin/movements/new"} {
		body := s.get(path, admin).Body.String()
		if !strings.Contains(body, `class="tabs tabs-box w-full`) {
			t.Errorf("%s: no full-width tab box", path)
		}
		if strings.Contains(body, "join-item btn") {
			t.Errorf("%s: still uses joined buttons as a view switcher", path)
		}
	}
}
