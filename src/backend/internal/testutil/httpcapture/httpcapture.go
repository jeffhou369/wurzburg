// Package httpcapture 提供測試用的 ResponseWriter 包裝：照常把回應寫給原本的 ResponseWriter，
// 同時留下狀態碼與本文，供契約檢查與 Secret 掃描使用。本套件只供測試匯入。
package httpcapture

import (
	"bytes"
	"net/http"
)

// Recorder 包住 http.ResponseWriter，記錄送出的狀態碼與本文。
type Recorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
	body        bytes.Buffer
}

// NewRecorder 建立 Recorder；處理器沒有呼叫 WriteHeader 時狀態碼為 200。
func NewRecorder(w http.ResponseWriter) *Recorder {
	return &Recorder{ResponseWriter: w, status: http.StatusOK}
}

// Status 回傳送出的狀態碼。
func (r *Recorder) Status() int { return r.status }

// Body 回傳送出的本文。
func (r *Recorder) Body() []byte { return r.body.Bytes() }

func (r *Recorder) WriteHeader(code int) {
	if !r.wroteHeader {
		r.status = code
		r.wroteHeader = true
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *Recorder) Write(p []byte) (int, error) {
	r.wroteHeader = true
	r.body.Write(p)
	return r.ResponseWriter.Write(p)
}

// Flush 讓串流回應（例如匯出）照常運作。
func (r *Recorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap 供 http.ResponseController 取得底層的 ResponseWriter。
func (r *Recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }
