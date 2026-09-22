package domain

import (
	"fmt"
	"strings"
)

// ValidateAnchors は章内目次 (chapterToc / Contents) に載る全アンカーが、変換結果の
// 見出しに現れることを確かめる。
//
// 目次は cisco.com が本文と同じ DITA から生成しているので、章の全トピックの完全な
// 一覧になる。1 つでも欠けていれば変換器がトピックを読み飛ばしている。
func ValidateAnchors(chapter string, expected []string, sections []Section) error {
	have := make(map[string]bool, len(sections))
	for _, s := range sections {
		have[s.Anchor] = true
	}
	var missing []string
	for _, a := range expected {
		if !have[a] {
			missing = append(missing, a)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	if len(missing) > 5 {
		missing = append(missing[:5], fmt.Sprintf("他 %d 件", len(missing)-5))
	}
	return fmt.Errorf("%s: 章内目次にあるトピックが変換結果に無い: %s", chapter, strings.Join(missing, ", "))
}
