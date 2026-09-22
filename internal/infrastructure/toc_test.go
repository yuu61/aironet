package infrastructure

import (
	"strings"
	"testing"
)

const flatTOC = `<html><head><title>Cisco Wireless Controller Command Reference, Release 8.5 - Cisco</title></head><body>
<ul id="bookToc">
<li><a href="/c/en/us/td/docs/wireless/controller/8-5/cmd-ref/b-cr85/preface.html">Preface</a></li>
<li><a href="/c/en/us/td/docs/wireless/controller/8-5/cmd-ref/b-cr85/clear_commands_a_to_l.html">Clear Commands: a to l</a></li>
<li><a href="/c/en/us/td/docs/wireless/controller/8-5/cmd-ref/b-cr85/b-cr85_CLT_chapter.html">Index</a></li>
</ul></body></html>`

const nestedTOC = `<html><body>
<ul id="bookToc">
<li><a href="/c/en/us/td/docs/wireless/controller/8-10/cmd-ref/b-cr810/preface.html">Preface</a></li>
<li><button><span></span>Config Commands</button>
<ul>
<li><a href="/c/en/us/td/docs/wireless/controller/8-10/cmd-ref/b-cr810/config_commands_802_11.html">Config Commands: 802.11</a></li>
<li><a href="/c/en/us/td/docs/wireless/controller/8-10/cmd-ref/b-cr810/config_commands_a_to_i.html">Config Commands: a to i</a></li>
</ul>
</li>
<li><a href="/c/en/us/td/docs/wireless/controller/8-10/cmd-ref/b-cr810/b-cr810_CLT_chapter.html">Index</a></li>
</ul></body></html>`

func TestParseBookTOCFlat(t *testing.T) {
	chs, err := ParseBookTOC([]byte(flatTOC), "https://www.cisco.com/c/en/us/td/docs/wireless/controller/8-5/cmd-ref/b-cr85.html")
	if err != nil {
		t.Fatal(err)
	}
	if len(chs) != 2 {
		t.Fatalf("chapters = %d (Index は除く), want 2: %+v", len(chs), chs)
	}
	if chs[1].File != "clear_commands_a_to_l" || chs[1].Part != "" {
		t.Errorf("chs[1] = %+v", chs[1])
	}
	if !strings.HasPrefix(chs[0].URL, "https://www.cisco.com/") {
		t.Errorf("URL が絶対になっていない: %s", chs[0].URL)
	}
}

func TestParseBookTOCNested(t *testing.T) {
	chs, err := ParseBookTOC([]byte(nestedTOC), "https://www.cisco.com/c/en/us/td/docs/wireless/controller/8-10/cmd-ref/b-cr810.html")
	if err != nil {
		t.Fatal(err)
	}
	if len(chs) != 3 {
		t.Fatalf("chapters = %d, want 3: %+v", len(chs), chs)
	}
	if chs[0].Part != "" || chs[1].Part != "Config Commands" || chs[2].Part != "Config Commands" {
		t.Errorf("parts = %q %q %q", chs[0].Part, chs[1].Part, chs[2].Part)
	}
	if chs[2].Title != sampleTitle {
		t.Errorf("title = %q", chs[2].Title)
	}
}

func TestParseBookTOCMissing(t *testing.T) {
	if _, err := ParseBookTOC([]byte("<html><body><p>x</p></body></html>"), "https://www.cisco.com/x.html"); err == nil {
		t.Error("目次が無いのにエラーにならない")
	}
}

func TestPageTitle(t *testing.T) {
	if got := PageTitle([]byte(flatTOC)); got != "Cisco Wireless Controller Command Reference, Release 8.5" {
		t.Errorf("title = %q", got)
	}
}
