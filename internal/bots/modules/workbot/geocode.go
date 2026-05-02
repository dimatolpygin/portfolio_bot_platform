package workbot

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type nominatimResponse struct {
	Address struct {
		City    string `json:"city"`
		Town    string `json:"town"`
		Village string `json:"village"`
		County  string `json:"county"`
	} `json:"address"`
}

// ReverseGeocode calls Nominatim and returns the best city name.
// Falls back: city → town → village → county → "Неизвестно".
func ReverseGeocode(ctx context.Context, httpClient *http.Client, lat, lon float64) (string, error) {
	url := fmt.Sprintf(
		"https://nominatim.openstreetmap.org/reverse?format=json&lat=%f&lon=%f&zoom=10&addressdetails=1",
		lat, lon,
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("build geocode request: %w", err)
	}
	req.Header.Set("User-Agent", "bots-platform/workbot geocoder")
	req.Header.Set("Accept-Language", "ru")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("geocode request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return "", fmt.Errorf("read geocode response: %w", err)
	}

	var nr nominatimResponse
	if err := json.Unmarshal(body, &nr); err != nil {
		return "", fmt.Errorf("decode geocode response: %w", err)
	}

	addr := nr.Address
	for _, candidate := range []string{addr.City, addr.Town, addr.Village, addr.County} {
		if candidate != "" {
			return candidate, nil
		}
	}
	return "Неизвестно", nil
}
