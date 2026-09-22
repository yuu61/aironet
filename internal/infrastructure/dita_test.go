package infrastructure

import (
	"strings"
	"testing"

	"github.com/yuu61/aironet/internal/domain"
)

// cisco.com の章ページの形を最小限に写した断片。本物の HTML は Cisco の著作物なので
// リポジトリに入れず、構造だけをここに再現する。
const chapterPage = `<html><body>
<div id="chapterToc"><ul>
<li><a href="#wp100">Config Commands: a to i</a></li>
<li><a href="#wp200">config aaa auth</a></li>
<li><a href="#grp1">Grouped topics</a><ul><li><a href="#wp300">Adding a WLAN</a></li></ul></li>
<li><a href="#wp400">Restrictions for X</a></li>
<li><a href="#wp500">show inline</a></li>
</ul></div>
<div id="chapterContent">
<h1 class="title topictitle1" id="ariaid-title1">Config Commands: a to i</h1>
<nav class="related-links"><ul class="ullinks"><li><a href="#wp200">config aaa auth</a></li></ul></nav>
<article class="topic reference nested1" id="wp200">
 <h2 class="title topictitle2 CRC_CmdRefCommand-X" id="ariaid-title2">config  aaa   auth</h2>
 <section class="body refbody">
  <section class="section"><p class="p">To configure, use the <span class="keyword kwd">config aaa auth </span> command.</p></section>
  <section class="section refsyn"><p class="figgroup synblk"><span class="keyword kwd">config aaa auth </span><kbd class="ph sep">[ </kbd><var>type</var><kbd class="ph sep"> | </kbd><span class="keyword kwd">local</span><kbd class="ph sep">]</kbd></p></section>
  <h2 class="sectiontitle">Syntax Description</h2>
  <table class="table syntax"><caption></caption><colgroup><col><col></colgroup><tbody>
   <tr><td class="entry"><p class="p"><span class="keyword kwd">local</span></p></td><td class="entry"><p class="p">Use local | database.</p><p class="p">Second paragraph.</p></td></tr>
  </tbody></table>
  <section class="section command_modes"><h3 class="sectiontitle">Command Modes</h3><p class="lines">Global configuration (config)</p></section>
  <h2 class="sectiontitle">Command History</h2>
  <table class="table command_history"><thead><tr><th class="entry">Release</th><th class="entry">Modification</th></tr></thead>
   <tbody><tr><td class="entry">8.5</td><td class="entry">This command was introduced.</td></tr></tbody></table>
  <section class="example"><h3 class="sectiontitle">Examples</h3>
   <p class="p">Example:</p>
   <pre class="pre codeblock"><code>(Cisco Controller) &gt; <b class="ph userinput">config aaa auth local</b>
line 2 &amp; more</code></pre>
   <table class="olh_note" role="note"><tr><td class="td_faq"><p><b>Note</b></p></td><td class="td_faq"><section class="note__content"><p>Be careful.</p><p>Really.</p></section></td></tr></table>
  </section>
 </section>
</article>
<p class="topictitle1" id="grp1">Grouped topics</p>
<article class="topic task nested1" id="wp300">
 <h2 class="title topictitle2" id="ariaid-title3">Adding a WLAN</h2>
 <section class="body taskbody">
  <table class="stepTable"><tbody>
   <tr class="li step"><td><p><b>Step 1</b></p></td><td><p class="ph cmd">Choose <span class="ph uicontrol">Wireless Settings</span>.</p>
     <ul class="ul choices"><li class="li choice"><p class="p">Option A</p></li><li class="li choice"><p class="p">Option B</p></li></ul></td></tr>
   <tr class="li step"><td><p><b>Step 2</b></p></td><td><p class="ph cmd">Click <span class="ph uicontrol">Apply</span>.</p>
     <img src="/c/dam/en/us/td/i/300001-400000/350001-360000/354001-355000/354147.jpg" alt="Login"></td></tr>
  </tbody></table>
  <p class="p">See <a class="xref" href="manage.html#x">Managing</a> and <a class="xref" href="#wp200">this</a> and <a href="https://example.com/a">ext</a>.</p>
 </section>
</article>
<article class="topic reference nested1" id="wp400">
 <h2 class="title topictitle2" id="ariaid-title4">Restrictions for X</h2>
 <section class="body refbody"><section class="section refsyn"><ul class="ul"><li class="li"><p class="p">Only on AP.</p></li></ul></section></section>
</article>
<article class="topic reference nested1" id="wp500">
 <h2 class="title topictitle2" id="ariaid-title5">show inline</h2>
 <section class="body refbody"><section class="section refsyn"><span class="keyword kwd">show inline</span> {<var>a</var> | <var>b</var>}</section></section>
</article>
</div></body></html>`

const sampleTitle = "Config Commands: a to i"

func convertSample(t *testing.T) Converted {
	t.Helper()
	ch := domain.Chapter{
		File:  "config_commands_a_to_i",
		Title: sampleTitle,
		URL:   "https://www.cisco.com/c/en/us/td/docs/wireless/controller/8-5/cmd-ref/b-cr85/config_commands_a_to_i.html",
	}
	images := map[string]string{
		"https://www.cisco.com/c/dam/en/us/td/i/300001-400000/350001-360000/354001-355000/354147.jpg": "354147.jpg",
	}
	cv, err := ConvertChapter([]byte(chapterPage), ch, images)
	if err != nil {
		t.Fatal(err)
	}
	return cv
}

func TestConvertChapterAnchors(t *testing.T) {
	cv := convertSample(t)
	if cv.Title != sampleTitle {
		t.Errorf("title = %q", cv.Title)
	}
	if got := cv.Anchors; strings.Join(got, ",") != "wp100,wp200,grp1,wp300,wp400,wp500" {
		t.Errorf("anchors = %v", got)
	}
	if err := domain.ValidateAnchors("t", cv.Anchors, cv.Sections); err != nil {
		t.Errorf("章内目次の検証に落ちる: %v", err)
	}
	if cv.Sections[0].Line != 1 || cv.Sections[0].Anchor != "wp100" {
		t.Errorf("章タイトル = %+v", cv.Sections[0])
	}
}

func TestConvertChapterEntry(t *testing.T) {
	cv := convertSample(t)
	// 箇条書きの refsyn (Restrictions for X) はコマンドではない。インラインだけの構文 (show inline) はコマンド。
	if len(cv.Entries) != 2 || cv.Entries[1].Command != "show inline" {
		t.Fatalf("entries = %+v", cv.Entries)
	}
	e := cv.Entries[0]
	if e.Command != "config aaa auth" || e.Anchor != "wp200" || !strings.HasSuffix(e.Source, "#wp200") {
		t.Errorf("entry = %+v", e)
	}
	lines := strings.Split(cv.Markdown, "\n")
	if e.Line < 1 || e.Line > len(lines) || lines[e.Line-1] != "## config aaa auth" {
		t.Errorf("entry line %d points to %q", e.Line, lines[min(max(e.Line-1, 0), len(lines)-1)])
	}
}

func TestConvertChapterSectionLines(t *testing.T) {
	cv := convertSample(t)
	lines := strings.Split(cv.Markdown, "\n")
	for _, s := range cv.Sections {
		if s.Line < 1 || s.Line > len(lines) || !strings.HasPrefix(lines[s.Line-1], "#") {
			t.Errorf("section %q line %d does not point to a heading", s.Title, s.Line)
		}
	}
}

func TestConvertChapterMarkdown(t *testing.T) {
	md := convertSample(t).Markdown
	want := []string{
		"# Config Commands: a to i\n",
		"## config aaa auth\n",
		"use the **config aaa auth** command.",
		"**Syntax:** `config aaa auth [ type | local]`",
		"### Syntax Description\n",
		"|  |  |\n| --- | --- |\n| **local** | Use local \\| database.<br>Second paragraph. |",
		"### Command Modes\n\n```\nGlobal configuration (config)\n```",
		"| Release | Modification |\n| --- | --- |\n| 8.5 | This command was introduced. |",
		"```\n(Cisco Controller) > config aaa auth local\nline 2 & more\n```",
		"> **Note:** Be careful.\n>\n> Really.",
		"## Grouped topics\n",
		"1. Choose **Wireless Settings**.\n\n   - Option A\n   - Option B\n2. Click **Apply**.\n\n   ![Login](images/354147.jpg)",
		"See [Managing](manage.md) and this and [ext](https://example.com/a).",
		"## Restrictions for X\n\n- Only on AP.",
		"## show inline\n\n**Syntax:** `show inline {a | b}`",
	}
	for _, w := range want {
		if !strings.Contains(md, w) {
			t.Errorf("Markdown に無い:\n%s\n--- got ---\n%s", w, md)
		}
	}
	if strings.Contains(md, "related-links") || strings.Contains(md, "Step 1") {
		t.Errorf("minitoc か Step ラベルが残っている:\n%s", md)
	}
}

func TestCollectImages(t *testing.T) {
	page := `<html><body><div id="chapterContent">
<img src="https://www.cisco.com/content/dam/en/us/td/i/templates/note.gif">
<img src="/c/dam/en/us/td/i/1/a.jpg">
<img src="/c/dam/en/us/td/i/1/a.jpg">
<img src="/c/dam/en/us/td/i/1/b.png">
</div></body></html>`
	got, err := CollectImages([]byte(page), "https://www.cisco.com/c/x/y.html")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "https://www.cisco.com/c/dam/en/us/td/i/1/a.jpg,https://www.cisco.com/c/dam/en/us/td/i/1/b.png" {
		t.Errorf("images = %v", got)
	}
}

func TestConvertEOT(t *testing.T) {
	page := `<html><body><div id="eot-doc-wrapper">
<header><h1 class="topictitle1">Release Notes for X</h1></header>
<main><article id="rn"><p class="topictitle1" id=""></p>
<article class="topic nested0" id="intro">
<nav><h2 class="topictitle2">Contents</h2><ul class="simple"><li class="olchildlink"><a href="#intro">Introduction</a></li><li class="olchildlink"><a href="#new">New</a></li></ul></nav>
<article class="topic nested1" id="new"><h2 class="topictitle2">New</h2><section class="body"><p>Body.</p></section></article>
</article></article></main></div></body></html>`
	cv, err := ConvertChapter([]byte(page), domain.Chapter{File: "b_ME_RN_810", URL: "https://www.cisco.com/c/x/b_ME_RN_810.html"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(cv.Anchors, ",") != "intro,new" {
		t.Errorf("anchors = %v", cv.Anchors)
	}
	if err := domain.ValidateAnchors("rn", cv.Anchors, cv.Sections); err != nil {
		t.Errorf("%v\n%s", err, cv.Markdown)
	}
	if !strings.HasPrefix(cv.Markdown, "# Release Notes for X\n") || strings.Contains(cv.Markdown, "Contents") {
		t.Errorf("markdown:\n%s", cv.Markdown)
	}
}
