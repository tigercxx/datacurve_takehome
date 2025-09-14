package http

import (
	"net/http"
	"os"
)

func RequireAPIToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := os.Getenv("API_TOKEN")
		got := r.Header.Get("Authorization")
		if len(got) < 8 || got[:7] != "Bearer " || got[7:] != want {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))

			return
		}
		next.ServeHTTP(w, r)
	})
}
