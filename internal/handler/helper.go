package handler

import (
	"errors"
	"io"
	"net/http"
	"sync"

	json "github.com/goccy/go-json"
)

const MaxBodySize = 4096

// Инициализируем пул байтовых буферов fixed-размера (4 КБ).
// Это уберет аллокации из io.ReadAll и защитит GC от нагрузки.
var bodyBufferPool = sync.Pool{
	New: func() any {
		b := make([]byte, MaxBodySize)
		return &b
	},
}

func ReadJSONOptimized(w http.ResponseWriter, r *http.Request, dst any) bool {
	ct := r.Header.Get("Content-Type")
	if len(ct) < 16 || ct[:16] != "application/json" {
		http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
		return false
	}

	bufPtr := bodyBufferPool.Get().(*[]byte)
	buf := *bufPtr
	defer bodyBufferPool.Put(bufPtr)
	defer r.Body.Close()

	n, err := io.ReadFull(r.Body, buf)

	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		if errors.Is(err, io.EOF) {
			http.Error(w, "Request body is empty", http.StatusBadRequest)
			return false
		}
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return false
	}

	if err == nil {
		oneByte := make([]byte, 1)
		_, extraErr := r.Body.Read(oneByte)
		if extraErr == nil || !errors.Is(extraErr, io.EOF) {
			http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
			return false
		}
	}

	payload := buf[:n]

	if err := json.Unmarshal(payload, dst); err != nil {
		http.Error(w, "Invalid JSON format", http.StatusBadRequest)
		return false
	}

	return true
}
