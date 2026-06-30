package plugin

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"nekocode/bot/tools"
	"nekocode/common"
)

func FetchURL(url string) ([]byte, error) {
	ctx, cancel := common.ShortContext()
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := tools.NewToolHTTPClient(10 * time.Second).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 64*1024))
}
