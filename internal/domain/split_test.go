package domain

import (
	"strings"
	"testing"
)

func TestTopicSlug(t *testing.T) {
	cases := map[string]string{
		"config aaa auth":                    "config_aaa_auth",
		"802.11r Fast Transition (CLI)":      "802.11r_fast_transition_cli",
		"Configuring 802.1X (GUI) / (CLI)":   "configuring_802.1x_gui_cli",
		"  Layer 2/3 Security!  ":            "layer_2_3_security",
		"config advanced dca anchor-time":    "config_advanced_dca_anchor-time",
		"日本語だけ":                              "topic",
		"con":                                "_con",
		"nul.value":                          "_nul.value",
		strings.Repeat("word ", 30) + "tail": strings.TrimSuffix(strings.Repeat("word_", 16), "_"),
	}
	for in, want := range cases {
		if got := TopicSlug(in); got != want {
			t.Errorf("TopicSlug(%q) = %q, want %q", in, got, want)
		}
	}
}

// chapterFixture は章 1 本の Markdown を見出しの記録付きで組む。
type chapterFixture struct {
	lines    []string
	headings []int
	sections []Section
	entries  []Entry
}

func (f *chapterFixture) heading(level, depth int, title string, group, entry bool) {
	if len(f.lines) > 0 {
		f.lines = append(f.lines, "")
	}
	f.lines = append(f.lines, strings.Repeat("#", level)+" "+title)
	line := len(f.lines)
	f.headings = append(f.headings, line)
	if depth < 0 {
		return // sectiontitle。索引に載らない
	}
	anchor := TopicSlug(title)
	f.sections = append(f.sections, Section{
		Anchor: anchor, Title: title, Chapter: "ch", File: "ch/README.md", Level: level, Depth: depth, Line: line, Group: group,
	})
	if entry {
		f.entries = append(f.entries, Entry{Command: title, Title: title, Anchor: anchor, File: "ch/README.md", Line: line})
	}
}

func (f *chapterFixture) body(s string) {
	f.lines = append(f.lines, "", s)
}

func (f *chapterFixture) text() ChapterText {
	return ChapterText{
		Markdown: strings.Join(f.lines, "\n") + "\n", Headings: f.headings, Sections: f.sections, Entries: f.entries,
	}
}

func sampleChapter() *chapterFixture {
	f := &chapterFixture{}
	f.heading(1, 0, "Config Commands", false, false)
	f.body("Chapter intro.")
	f.heading(2, 1, "config aaa auth", false, true)
	f.body("**Syntax:** `config aaa auth`")
	f.heading(3, -1, "Syntax Description", false, false)
	f.body("| a | b |")
	f.heading(2, 0, "Grouped", true, false)
	f.heading(3, 1, "config aaa auth", false, true) // 同じ見出しが 2 回
	f.body("Second body.")
	f.heading(3, 1, "Big Topic", false, false)
	f.body(strings.Repeat("x", MaxPartBytes)) // このトピックだけ MaxPartBytes 超
	f.heading(4, 2, "Child One", false, false)
	f.body("Child one body.")
	f.heading(5, 3, "Grandchild", false, false)
	f.body("Deep body.")
	f.heading(4, 2, "Child Two", false, false)
	f.body("Child two body.")
	return f
}

func findPart(t *testing.T, parts []Part, path string) Part {
	t.Helper()
	for _, p := range parts {
		if p.Path == path {
			return p
		}
	}
	t.Fatalf("part %q not found in %v", path, paths(parts))
	return Part{}
}

func paths(parts []Part) []string {
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = p.Path
	}
	return out
}

func TestSplitLayout(t *testing.T) {
	sp, err := Split(Chapter{File: "ch"}, sampleChapter().text())
	if err != nil {
		t.Fatal(err)
	}
	want := "ch/README.md,ch/config_aaa_auth.md,ch/config_aaa_auth_2.md,ch/big_topic.md,ch/child_one.md,ch/child_two.md"
	if got := strings.Join(paths(sp.Parts), ","); got != want {
		t.Errorf("parts = %s", got)
	}
	index := findPart(t, sp.Parts, "ch/README.md").Markdown
	wantIndex := "# Config Commands\n\nChapter intro.\n\n- [config aaa auth](config_aaa_auth.md)\n\n## Grouped\n\n" +
		"- [config aaa auth](config_aaa_auth_2.md)\n- [Big Topic](big_topic.md)\n"
	if index != wantIndex {
		t.Errorf("README:\n%s", index)
	}
	cmd := findPart(t, sp.Parts, "ch/config_aaa_auth.md").Markdown
	if cmd != "# config aaa auth\n\n**Syntax:** `config aaa auth`\n\n## Syntax Description\n\n| a | b |\n" {
		t.Errorf("command file:\n%s", cmd)
	}
	big := findPart(t, sp.Parts, "ch/big_topic.md").Markdown
	if !strings.HasPrefix(big, "# Big Topic\n\nxxx") || !strings.HasSuffix(big, "\n\n- [Child One](child_one.md)\n- [Child Two](child_two.md)\n") {
		t.Errorf("big topic:\n%.40s…%s", big, big[len(big)-80:])
	}
	// 孫が切り出されるのは親 (Child One) がファイルかつ閾値超のとき。Child One は小さいので孫はその中。
	child := findPart(t, sp.Parts, "ch/child_one.md").Markdown
	if child != "# Child One\n\nChild one body.\n\n## Grandchild\n\nDeep body.\n" {
		t.Errorf("child one:\n%s", child)
	}
}

func TestSplitRecursive(t *testing.T) {
	// 大きなトピックの子も大きければ、その子 (孫) まで切り出す。
	f := &chapterFixture{}
	f.heading(1, 0, "Chapter", false, false)
	f.heading(2, 1, "Feature", false, false)
	f.body(strings.Repeat("a", MaxPartBytes))
	f.heading(3, 2, "Task", false, false)
	f.body(strings.Repeat("b", MaxPartBytes))
	f.heading(4, 3, "Step Detail", false, false)
	f.body("Detail.")
	f.heading(3, 2, "Other", false, false)
	f.body("Other.")
	sp, err := Split(Chapter{File: "ch"}, f.text())
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(paths(sp.Parts), ","); got != "ch/README.md,ch/feature.md,ch/task.md,ch/step_detail.md,ch/other.md" {
		t.Errorf("parts = %s", got)
	}
	task := findPart(t, sp.Parts, "ch/task.md").Markdown
	if !strings.HasSuffix(task, "\n\n- [Step Detail](step_detail.md)\n") {
		t.Errorf("task:\n%s", task[len(task)-80:])
	}
	if d := findPart(t, sp.Parts, "ch/step_detail.md").Markdown; d != "# Step Detail\n\nDetail.\n" {
		t.Errorf("step detail:\n%s", d)
	}
}

// checkSection は索引の見出しが分割後のファイルの見出し行を指すことを見る。
func checkSection(t *testing.T, parts []Part, s Section, file string, line, level int) {
	t.Helper()
	if s.File != file || s.Line != line || s.Level != level {
		t.Errorf("%q → %s:%d (level %d), want %s:%d (level %d)", s.Title, s.File, s.Line, s.Level, file, line, level)
	}
	if s.ID() != s.Chapter+"#"+s.Anchor {
		t.Errorf("ID = %q", s.ID())
	}
	lines := strings.Split(findPart(t, parts, s.File).Markdown, "\n")
	if s.Line > len(lines) || lines[s.Line-1] != strings.Repeat("#", s.Level)+" "+s.Title {
		t.Errorf("%q line %d of %s is %q", s.Title, s.Line, s.File, lines[min(s.Line, len(lines))-1])
	}
}

func TestSplitIndexRemap(t *testing.T) {
	sp, err := Split(Chapter{File: "ch"}, sampleChapter().text())
	if err != nil {
		t.Fatal(err)
	}
	byTitle := map[string][]Section{}
	for _, s := range sp.Sections {
		byTitle[s.Title] = append(byTitle[s.Title], s)
	}
	checkSection(t, sp.Parts, byTitle["Config Commands"][0], "ch/README.md", 1, 1)
	checkSection(t, sp.Parts, byTitle["config aaa auth"][0], "ch/config_aaa_auth.md", 1, 1)
	checkSection(t, sp.Parts, byTitle["Grouped"][0], "ch/README.md", 7, 2)
	checkSection(t, sp.Parts, byTitle["config aaa auth"][1], "ch/config_aaa_auth_2.md", 1, 1)
	checkSection(t, sp.Parts, byTitle["Child One"][0], "ch/child_one.md", 1, 1)
	checkSection(t, sp.Parts, byTitle["Grandchild"][0], "ch/child_one.md", 5, 2)
	if len(sp.Entries) != 2 || sp.Entries[1].File != "ch/config_aaa_auth_2.md" || sp.Entries[1].Line != 1 {
		t.Errorf("entries = %+v", sp.Entries)
	}
}

func TestSplitIndexDemotesSecondTitle(t *testing.T) {
	// 1 ページ資料: 資料名 (h1) の後に nested0 の h1 が並ぶ。README では ## にする。
	f := &chapterFixture{}
	f.heading(1, 0, "Release Notes", false, false)
	f.heading(1, 0, "Introduction", false, false)
	f.body("Intro.")
	f.heading(2, 1, "New Features", false, false)
	f.body("New.")
	sp, err := Split(Chapter{File: "rn"}, f.text())
	if err != nil {
		t.Fatal(err)
	}
	index := findPart(t, sp.Parts, "rn/README.md").Markdown
	if index != "# Release Notes\n\n## Introduction\n\nIntro.\n\n- [New Features](new_features.md)\n" {
		t.Errorf("README:\n%s", index)
	}
}

func TestSplitRejectsMisplacedHeading(t *testing.T) {
	f := sampleChapter()
	f.headings = append(f.headings, 3) // 本文の行を見出しとして記録してしまった
	if _, err := Split(Chapter{File: "ch"}, f.text()); err == nil || !strings.Contains(err.Error(), "3 行") {
		t.Errorf("err = %v", err)
	}
}

func TestSplitRejectsUnrecordedHeading(t *testing.T) {
	f := sampleChapter()
	f.headings = f.headings[1:] // 章タイトルの記録を落とす
	if _, err := Split(Chapter{File: "ch"}, f.text()); err == nil || !strings.Contains(err.Error(), "Config Commands") {
		t.Errorf("err = %v", err)
	}
}

func TestResolveLinks(t *testing.T) {
	sections := []Section{
		{Chapter: "a", Anchor: "top", File: "a/README.md"},
		{Chapter: "a", Anchor: "x", File: "a/x.md"},
		{Chapter: "a", Anchor: "y", File: "a/y.md"},
		{Chapter: "b", Anchor: "z", File: "b/z.md"},
	}
	parts := []Part{
		{Path: "a/README.md", Markdown: "i"},
		{Path: "a/x.md", Markdown: "see " + LinkRef("a", "y", "https://h/a.html#y", "Y") +
			", " + LinkRef("a", "x", "https://h/a.html#x", "self") +
			", " + LinkRef("b", "z", "https://h/b.html#z", "Z") +
			", " + LinkRef("a", "nope", "https://h/a.html#nope", "lost") +
			", " + LinkRef("a", "", "https://h/a.html", "chapter") +
			", " + LinkRef("c", "q", "https://h/c.html#q", "other book") + "."},
		{Path: "a/y.md", Markdown: "y"},
		{Path: "b/z.md", Markdown: "z"},
	}
	got := ResolveLinks(parts, sections)[1].Markdown
	want := "see [Y](y.md), self, [Z](../b/z.md), [lost](README.md), [chapter](README.md), [other book](https://h/c.html#q)."
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}
