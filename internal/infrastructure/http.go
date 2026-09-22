package infrastructure

import (
	"context"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// UserAgent は cisco.com に名乗る UA。
//
// ブラウザを装ってはいけない。cisco.com の前段 (Akamai) は UA と TLS の指紋を
// 突き合わせていて、Go の net/http から Chrome の UA を送ると常に 403 になる
// (同じ URL で交互に試して確認済み)。逆に、この UA に Accept と Accept-Language を
// 添えれば常に 200 が返る。UA だけで Accept を省くと 403 になるので、3 つを揃えて送る。
const UserAgent = "manualbook/0.1 (+https://github.com/yuu61/aironet)"

// Client は cisco.com に礼儀正しく取りに行く HTTP クライアント。
// 連続するリクエストの間に delay を置き、一時的な失敗だけ少し待って取り直す。
type Client struct {
	last  time.Time
	http  *http.Client
	delay time.Duration
}

// NewClient は delay 間隔で取りに行く Client を作る。
func NewClient(delay time.Duration) *Client {
	return &Client{http: &http.Client{Timeout: 2 * time.Minute}, delay: delay}
}

// akamaiRef は 403 ページに刷られる参照番号。ブロックされたときに問い合わせや
// 切り分けに使えるので、エラー文に含める。
var akamaiRef = regexp.MustCompile(`Reference(?:&#32;|\s)+(?:&#35;|#)\s*([0-9a-f.]+)`)

// Get は URL の本文を返す。非 200 はエラー。
func (c *Client) Get(ctx context.Context, url string) ([]byte, error) {
	const attempts = 3
	var lastErr error
	for i := range attempts {
		if i > 0 {
			// 一時的な失敗 (5xx、接続断) は間隔を広げて取り直す。
			if err := sleep(ctx, c.delay*time.Duration(1<<i)); err != nil {
				return nil, err
			}
		}
		body, retry, err := c.getOnce(ctx, url)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retry {
			break
		}
	}
	return nil, lastErr
}

func (c *Client) getOnce(ctx context.Context, url string) (body []byte, retry bool, err error) {
	if waitErr := c.wait(ctx); waitErr != nil {
		return nil, false, waitErr
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,image/*;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, true, err
	}
	if resp.StatusCode == http.StatusOK {
		return body, false, nil
	}
	msg := "HTTP " + resp.Status
	if m := akamaiRef.FindSubmatch(body); m != nil {
		msg += " (Akamai Reference #" + string(m[1]) + ")"
	}
	if resp.StatusCode == http.StatusForbidden {
		msg += "。UA を装わず Accept / Accept-Language を送っているか確かめる"
	}
	retry = resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests
	return nil, retry, errors.New(msg)
}

// wait は直前のリクエストから delay 経つまで待つ。
func (c *Client) wait(ctx context.Context) error {
	if !c.last.IsZero() {
		if rest := c.delay - time.Since(c.last); rest > 0 {
			if err := sleep(ctx, rest); err != nil {
				return err
			}
		}
	}
	c.last = time.Now()
	return nil
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// IsHTML は本文が HTML ページかどうかの目安。画像の URL が HTML (エラーページ) を
// 返したときに画像として保存しないために見る。
func IsHTML(body []byte) bool {
	head := strings.ToLower(string(body[:min(len(body), 512)]))
	return strings.Contains(head, "<html") || strings.Contains(head, "<!doctype html")
}
