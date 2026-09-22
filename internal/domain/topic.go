package domain

import (
	"regexp"
	"strings"
)

// maxSlugLen はトピックのファイル名 (拡張子なし) の上限。Windows の MAX_PATH に収める。
const maxSlugLen = 80

// windowsReserved は Windows でファイル名にできない名前 (最初の . より前で判定される)。
var windowsReserved = regexp.MustCompile(`^(con|prn|aux|nul|com[1-9]|lpt[1-9])($|\.)`)

// TopicSlug は見出しからトピックのファイル名 (拡張子なし) を作る。
// 英数字と . - は小文字のまま残し、他の文字の並びは _ 1 つに畳む。
// "config aaa auth" → "config_aaa_auth"、"802.11r Fast Transition (CLI)" → "802.11r_fast_transition_cli"。
// ls で見つけられるように見出しの語をそのまま残す。同じ章での重複は Split が _2, _3 … で分ける。
func TopicSlug(title string) string {
	s := strings.Trim(slugChars(title), "._-")
	if len(s) > maxSlugLen {
		s = s[:maxSlugLen]
		if i := strings.LastIndex(s, "_"); i > maxSlugLen/2 {
			s = s[:i]
		}
		s = strings.Trim(s, "._-")
	}
	if s == "" {
		return "topic"
	}
	if windowsReserved.MatchString(s) {
		return "_" + s
	}
	return s
}

// slugChars は小文字化し、英数字と . - 以外の並びを _ に畳む。
func slugChars(title string) string {
	var b strings.Builder
	pending := false
	for _, r := range strings.ToLower(title) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '-':
			if pending && b.Len() > 0 {
				b.WriteByte('_')
			}
			pending = false
			b.WriteRune(r)
		default:
			pending = true
		}
	}
	return b.String()
}
