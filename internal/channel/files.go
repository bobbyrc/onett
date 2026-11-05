package channel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// DownloadTimeout bounds one file download from a chat service.
const DownloadTimeout = 2 * time.Minute

// DownloadClient returns a copy of c without its overall timeout, for large bodies: the caller
// bounds a download with a context instead (DownloadTimeout), so a slow but steady transfer is
// not cut off by the short timeout meant for API calls.
func DownloadClient(c *http.Client) *http.Client {
	dc := *c
	dc.Timeout = 0
	return &dc
}

// Download starts a GET of rawURL and returns its body, capped at limit+1 bytes so the saver can
// tell a file at the limit from one over it. The URL may carry a secret (Telegram's embeds the
// bot token), so no error returned here includes it. No credentials are sent.
func Download(ctx context.Context, c *http.Client, rawURL string, limit int64) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, errors.New("bad download address")
	}
	resp, err := c.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return nil, fmt.Errorf("download failed: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}
	return struct {
		io.Reader
		io.Closer
	}{io.LimitReader(resp.Body, limit+1), resp.Body}, nil
}

// MB renders a byte count in megabytes for a message to the user, rounding up so a file just
// over the limit never reads as equal to it.
func MB(n int64) string {
	const mb = 1 << 20
	return fmt.Sprintf("%d MB", (n+mb-1)/mb)
}

// TooLarge is the reply for a file over the size limit. size is 0 when it is not known.
func TooLarge(size, limit int64) string {
	if size <= 0 {
		return fmt.Sprintf("That file is over the limit of %s.", MB(limit))
	}
	return fmt.Sprintf("That file is %s; the limit is %s.", MB(size), MB(limit))
}
