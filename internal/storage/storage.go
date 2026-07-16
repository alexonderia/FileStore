package storage

import (
	"context"
	"io"
)

type Object struct {
	Body        io.ReadCloser
	Size        int64
	ContentType string
}

type ObjectStore interface {
	Put(context.Context, string, io.Reader, string) error
	Get(context.Context, string) (Object, error)
	Delete(context.Context, string) error
	List(context.Context, string) ([]string, error)
}
