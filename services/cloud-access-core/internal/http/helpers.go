package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var errBadRequest = errors.New("bad request")

func parseJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return fmt.Errorf("%w: %v", errBadRequest, err)
	}
	if err := decoder.Decode(new(struct{})); err != io.EOF {
		return errBadRequest
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) error {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Date", time.Now().UTC().Format(http.TimeFormat))
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(payload)
}

func clientIP(r *http.Request) string {
	if hdr := r.Header.Get("X-Forwarded-For"); hdr != "" {
		parts := strings.Split(hdr, ",")
		return strings.TrimSpace(parts[0])
	}
	if hdr := r.Header.Get("X-Real-IP"); hdr != "" {
		return hdr
	}
	return r.RemoteAddr
}
