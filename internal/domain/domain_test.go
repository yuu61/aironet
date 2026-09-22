package domain

import (
	"strings"
	"testing"
)

func validManifest() Manifest {
	return Manifest{
		Trains: []Train{{ID: "8-5"}, {ID: "8-10"}},
		Docs: []Doc{
			{Train: "8-5", Book: "cr", Kind: KindBook, Title: "CR 8.5", URL: "https://www.cisco.com/x/b-cr85.html"},
			{Train: "8-10", Book: "me-rn", Kind: KindPage, Title: "ME RN", URL: "https://www.cisco.com/x/b_ME_RN_810.html"},
		},
	}
}

func TestManifestValidate(t *testing.T) {
	if err := validManifest().Validate(); err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}
	cases := []struct {
		name string
		mut  func(*Manifest)
		want string
	}{
		{"unknown train", func(m *Manifest) { m.Docs[0].Train = "9-0" }, "trains にありません"},
		{"empty kind", func(m *Manifest) { m.Docs[0].Kind = "" }, "kind が無い"},
		{"bad kind", func(m *Manifest) { m.Docs[0].Kind = "pdf" }, "知らない"},
		{"http url", func(m *Manifest) { m.Docs[0].URL = "http://www.cisco.com/x.html" }, "https"},
		{"dup key", func(m *Manifest) { m.Docs[1] = m.Docs[0] }, "重複"},
		{"slash in book", func(m *Manifest) { m.Docs[0].Book = "a/b" }, "ディレクトリ名"},
		{"empty title", func(m *Manifest) { m.Docs[0].Title = "" }, "title が空"},
	}
	for _, c := range cases {
		m := validManifest()
		c.mut(&m)
		err := m.Validate()
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want containing %q", c.name, err, c.want)
		}
	}
}

func TestManifestSelect(t *testing.T) {
	m := validManifest()
	if got := m.Select(""); len(got) != 2 {
		t.Errorf("Select(all) = %d docs", len(got))
	}
	if got := m.Select("8-10/me-rn"); len(got) != 1 || got[0].Book != "me-rn" {
		t.Errorf("Select(8-10/me-rn) = %+v", got)
	}
	if got := m.Select("8-10/cr"); len(got) != 0 {
		t.Errorf("Select(missing) = %+v", got)
	}
}

func TestChapterFile(t *testing.T) {
	cases := map[string]string{
		"https://www.cisco.com/c/en/us/td/docs/wireless/controller/8-5/cmd-ref/b-cr85/config_commands_a_to_i.html": "config_commands_a_to_i",
		"https://www.cisco.com/x/b_ME_RN_810.html?bookSearch=true":                                                 "b_ME_RN_810",
		"https://www.cisco.com/x/page.html#wp123":                                                                  "page",
	}
	for in, want := range cases {
		if got := ChapterFile(in); got != want {
			t.Errorf("ChapterFile(%s) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeCommand(t *testing.T) {
	if got := NormalizeCommand("  config   advanced\n\t 802.11 channel  "); got != "config advanced 802.11 channel" {
		t.Errorf("got %q", got)
	}
	if got := TSVCell("a\tb\nc"); got != "a b c" {
		t.Errorf("TSVCell = %q", got)
	}
}

func TestDefaultManualsDir(t *testing.T) {
	env := func(k string) string {
		if k == ManualsEnv {
			return "/tmp/manuals"
		}
		return ""
	}
	if got := DefaultManualsDir(env, "/home/u"); got != "/tmp/manuals" {
		t.Errorf("env: got %q", got)
	}
	got := DefaultManualsDir(func(string) string { return "" }, "/home/u")
	if !strings.HasSuffix(got, "manuals") || !strings.Contains(got, ".aironet") {
		t.Errorf("home: got %q", got)
	}
}

func TestSectionID(t *testing.T) {
	// ファイルは分割で変わるが、ID は章とアンカーで決まる。
	s := Section{Chapter: "manage", File: "manage/managing_wlans.md", Anchor: "ID307"}
	if got := s.ID(); got != "manage#ID307" {
		t.Errorf("ID = %q", got)
	}
}

func TestValidateAnchors(t *testing.T) {
	secs := []Section{{Anchor: "a"}, {Anchor: "b"}}
	if err := ValidateAnchors("ch", []string{"a", "b"}, secs); err != nil {
		t.Errorf("complete: %v", err)
	}
	err := ValidateAnchors("ch", []string{"a", "c"}, secs)
	if err == nil || !strings.Contains(err.Error(), "c") {
		t.Errorf("missing: %v", err)
	}
}
