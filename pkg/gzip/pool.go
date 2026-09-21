// Пакет gzip предоставляет инструменты промышленного уровня для
// симметричного сжатия ответов сервера и распаковки входящих запросов на лету.
package gzip

import (
	"compress/gzip"
	"sync"
)

// Глобальные пулы объектов для исключения аллокаций памяти в куче под каждый HTTP-запрос.
var (
	// writerPool повторно использует внутренние структуры компрессора gzip.Writer.
	writerPool = sync.Pool{
		New: func() any {
			w, _ := gzip.NewWriterLevel(nil, gzip.DefaultCompression)
			return w
		},
	}

	// readerPool повторно использует структуры декомпрессора gzip.Reader.
	readerPool = sync.Pool{
		New: func() any {
			return &gzip.Reader{}
		},
	}
)
