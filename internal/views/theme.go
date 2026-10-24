package views

import "context"

// Theme is a phone's colour choice. ThemeSystem (empty) follows the phone's
// light/dark setting; the others force a daisyUI theme from frontend/app.css.
type Theme string

const (
	ThemeSystem Theme = ""
	ThemeLight  Theme = "light"
	ThemeDark   Theme = "dark"
)

// ParseTheme accepts only the known choices; "system" means ThemeSystem.
func ParseTheme(s string) (Theme, bool) {
	switch Theme(s) {
	case ThemeLight, ThemeDark:
		return Theme(s), true
	case "system":
		return ThemeSystem, true
	}
	return ThemeSystem, false
}

type themeKey struct{}

// WithTheme makes Layout render pages in t.
func WithTheme(ctx context.Context, t Theme) context.Context {
	return context.WithValue(ctx, themeKey{}, t)
}

func themeFrom(ctx context.Context) Theme {
	t, _ := ctx.Value(themeKey{}).(Theme)
	return t
}

// daisyTheme is the data-theme value for t; empty lets prefers-color-scheme pick.
func daisyTheme(t Theme) string {
	switch t {
	case ThemeLight:
		return "aesthetic"
	case ThemeDark:
		return "aesthetic-dark"
	}
	return ""
}

func colorScheme(t Theme) string {
	if t == ThemeSystem {
		return "light dark"
	}
	return string(t)
}
