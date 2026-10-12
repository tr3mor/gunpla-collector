package fx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchUSDToEUR(t *testing.T) {
	serve := func(status int, body string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			w.Write([]byte(body))
		}))
	}
	tests := []struct {
		name    string
		status  int
		body    string
		want    float64
		wantErr bool
	}{
		{"ok", 200, `{"base":"USD","rates":{"EUR":0.89087}}`, 0.89087, false},
		{"server error", 500, `oops`, 0, true},
		{"missing rate", 200, `{"rates":{}}`, 0, true},
		{"implausible", 200, `{"rates":{"EUR":89}}`, 0, true},
		{"not json", 200, `<html>`, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := serve(tt.status, tt.body)
			defer srv.Close()
			got, err := fetchUSDToEUR(context.Background(), srv.Client(), srv.URL)
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Errorf("got %v, %v; want %v, err=%v", got, err, tt.want, tt.wantErr)
			}
		})
	}
}
