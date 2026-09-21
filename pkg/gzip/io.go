package gzip

import (
	"compress/gzip"
	"io"
	"net/http"
)

// CompressWriter перехватывает запись в http.ResponseWriter и выполняет ленивое сжатие байт в сеть.
type CompressWriter struct {
	w           http.ResponseWriter
	zw          *gzip.Writer
	wroteHeader bool // Флаг защиты от двойного выставления заголовков сжатия
}

// NewCompressWriter извлекает «прогретый» компрессор из пула памяти и связывает его с HTTP-ответом.
func NewCompressWriter(w http.ResponseWriter) *CompressWriter {
	zw := writerPool.Get().(*gzip.Writer)
	zw.Reset(w) // Сбрасываем внутреннее состояние компрессора на текущий response-поток

	return &CompressWriter{
		w:  w,
		zw: zw,
	}
}

// Header возвращает карту HTTP-заголовков оригинального ответа.
func (c *CompressWriter) Header() http.Header {
	return c.w.Header()
}

// Write лениво выставляет заголовок Content-Encoding и сжимает байты напрямую в сокет.
func (c *CompressWriter) Write(p []byte) (int, error) {
	if !c.wroteHeader {
		c.w.Header().Set("Content-Encoding", "gzip")
		c.wroteHeader = true
	}
	return c.zw.Write(p)
}

// WriteHeader блокирует выставление Content-Encoding для пустых ответов без тела (204 No Content, 304 Not Modified).
func (c *CompressWriter) WriteHeader(statusCode int) {
	if statusCode == http.StatusNoContent || statusCode == http.StatusNotModified {
		c.wroteHeader = true
	}
	c.w.WriteHeader(statusCode)
}

// Close досылает остатки сжатых байт (flush) в TCP-сокет и безопасно возвращает компрессор в пул.
func (c *CompressWriter) Close() error {
	err := c.zw.Close()
	writerPool.Put(c.zw) // Освобождаем внутренние буферы компрессора обратно в sync.Pool
	return err
}

// CompressReader прозрачно декомпрессирует тело входящего запроса от клиента.
type CompressReader struct {
	r  io.ReadCloser
	zr *gzip.Reader
}

// NewCompressReader извлекает декомпрессор из пула и инициализирует его сжатым потоком HTTP-запроса.
func NewCompressReader(r io.ReadCloser) (*CompressReader, error) {
	zr := readerPool.Get().(*gzip.Reader)
	if err := zr.Reset(r); err != nil {
		readerPool.Put(zr) // При сбое (битый поток) мгновенно освобождаем структуру в пул
		return nil, err
	}
	return &CompressReader{r: r, zr: zr}, nil
}

// Read вычитывает декомпрессированные байты для передачи вышележащим парсерам (например, goccy/go-json).
func (c *CompressReader) Read(p []byte) (int, error) {
	return c.zr.Read(p)
}

// Close закрывает сетевой поток, внутренний декомпрессор и очищает память в пуле.
func (c *CompressReader) Close() error {
	if err := c.r.Close(); err != nil {
		return err
	}
	_ = c.zr.Close()
	readerPool.Put(c.zr) // Освобождаем декомпрессор в sync.Pool
	return nil
}
