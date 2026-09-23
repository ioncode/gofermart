package accrual

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

type CheckResult struct {
	Body       []byte
	StatusCode int
	Backoff    time.Duration
}

type Client struct {
	httpClient *http.Client
	baseURL    string
}

func NewClient(baseURL string, timeout time.Duration) *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = 100
	transport.MaxIdleConnsPerHost = 100
	transport.IdleConnTimeout = 90 * time.Second

	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout:   timeout,
			Transport: transport,
		},
	}
}

func (c *Client) CheckOrder(ctx context.Context, orderNumber string) (CheckResult, error) {
	url := fmt.Sprintf("%s/api/orders/%s", c.baseURL, orderNumber)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return CheckResult{}, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return CheckResult{}, err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	// Обработка реактивного лимита от сервера
	if resp.StatusCode == http.StatusTooManyRequests {
		seconds, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
		if seconds == 0 {
			seconds = 5 // Дефолт, если сервер не прислал заголовок
		}
		return CheckResult{
			StatusCode: resp.StatusCode,
			Backoff:    time.Duration(seconds) * time.Second,
		}, nil
	}

	if resp.StatusCode == http.StatusNoContent || resp.StatusCode != http.StatusOK {
		return CheckResult{StatusCode: resp.StatusCode}, nil
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	return CheckResult{Body: bodyBytes, StatusCode: resp.StatusCode}, err
}
