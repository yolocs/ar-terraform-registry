package store

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"golang.org/x/oauth2/google"
)

type Downloader struct {
	client *http.Client
}

func NewDownloader(ctx context.Context) (*Downloader, error) {
	// Create an HTTP client with the credentials
	client, err := google.DefaultClient(ctx, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return nil, fmt.Errorf("failed to create authenticated client: %w", err)
	}

	return &Downloader{
		client: client,
	}, nil
}

func (d *Downloader) Download(ctx context.Context, fullFileName string) (io.ReadCloser, error) {
	url := fmt.Sprintf("https://artifactregistry.googleapis.com/download/v1/%s", fullFileName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create download request: %w", err)
	}

	// Execute request with authenticated client
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute download request: %w", err)
	}

	// Check response status
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected download status code: %d", resp.StatusCode)
	}

	return resp.Body, nil
}
