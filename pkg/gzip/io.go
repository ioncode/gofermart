package gzip

import (
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
)

// CompressWriter перехватывает запись в http.ResponseWriter для ленивого сжатия.
type CompressWriter struct {
	w           http.ResponseWriter
	zw          *gzip.Writer
	wroteHeader bool
	noContent   bool
}

// NewCompressWriter создает новый экземпляр обертки на базе пулируемого gzip.Writer.
func NewCompressWriter(w http.ResponseWriter) *CompressWriter {
	zw := writerPool.Get().(*gzip.Writer)
	zw.Reset(w)
	return &CompressWriter{w: w, zw: zw}
}

// Header возвращает карту HTTP-заголовков оригинального ответа.
func (c *CompressWriter) Header() http.Header {
	return c.w.Header()
}

// Write лениво выставляет заголовок сжатия и выталкивает чанки данных в сеть.
func (c *CompressWriter) Write(p []byte) (int, error) {
	if c.noContent {
		return c.w.Write(p)
	}
	if !c.wroteHeader {
		c.w.Header().Set("Content-Encoding", "gzip")
		c.wroteHeader = true
	}
	n, err := c.zw.Write(p)
	if err != nil {
		return n, err
	}
	if err := c.zw.Flush(); err != nil {
		return n, fmt.Errorf("gzip flush failed: %w", err)
	}
	if f, ok := c.w.(http.Flusher); ok {
		f.Flush()
	}
	return n, nil
}

// WriteHeader блокирует выставление Content-Encoding для пустых статус-кодов.
func (c *CompressWriter) WriteHeader(statusCode int) {
	if statusCode == http.StatusNoContent || statusCode == http.StatusNotModified {
		c.wroteHeader = true
		c.noContent = true
	}
	c.w.WriteHeader(statusCode)
}

// Close досылает остатки архива только если хендлер реально писал данные в сокет.
func (c *CompressWriter) Close() error {
	if !c.wroteHeader || c.noContent {
		writerPool.Put(c.zw)
		return nil
	}
	err := c.zw.Close()
	writerPool.Put(c.zw)
	return err
}

// CompressReader прозрачно распаковывает входящее сжатое тело запроса клиента.
type CompressReader struct {
	r  io.ReadCloser
	zr *gzip.Reader
}

// NewCompressReader извлекает декомпрессор из пула и связывает его с потоком запроса.
func NewCompressReader(r io.ReadCloser) (*CompressReader, error) {
	zr := readerPool.Get().(*gzip.Reader)
	if err := zr.Reset(r); err != nil {
		readerPool.Put(zr)
		return nil, err
	}
	return &CompressReader{r: r, zr: zr}, nil
}

// Read вычитывает распакованные байты для парсеров бизнес-логики.
func (c *CompressReader) Read(p []byte) (int, error) {
	return c.zr.Read(p)
}

// Close закрывает сетевые дескрипторы и возвращает структуры декомпрессии в пул.
func (c *CompressReader) Close() error {
	if err := c.r.Close(); err != nil {
		return err
	}
	_ = c.zr.Close()
	readerPool.Put(c.zr)
	return nil
}
