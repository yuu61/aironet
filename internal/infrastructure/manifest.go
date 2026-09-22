package infrastructure

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/yuu61/aironet/internal/domain"
)

// ReadManifest はマニフェストを読んで検証する。
func ReadManifest(path string) (domain.Manifest, error) {
	var m domain.Manifest
	b, err := os.ReadFile(path)
	if err != nil {
		return m, fmt.Errorf("マニフェストを読めません: %w", err)
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return m, fmt.Errorf("マニフェストの解析に失敗: %w", err)
	}
	if err := m.Validate(); err != nil {
		return m, fmt.Errorf("%s: %w", path, err)
	}
	return m, nil
}
