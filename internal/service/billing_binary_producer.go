package service

import (
	"errors"
	"fmt"
	"github.com/go-openapi/runtime"
	"io"
)

// BinaryProducer creates a new producer that passes through raw binary data
func binaryProducer() runtime.Producer {
	return runtime.ProducerFunc(func(writer io.Writer, data interface{}) error {
		if writer == nil {
			return errors.New("BinaryProducer requires a writer")
		}

		if data == nil {
			return errors.New("no data given to produce binary output from")
		}

		// Handle []byte directly
		if b, ok := data.([]byte); ok {
			_, err := writer.Write(b)
			return err
		}

		// Handle string by converting to bytes
		if s, ok := data.(string); ok {
			_, err := writer.Write([]byte(s))
			return err
		}

		// Handle io.Reader by copying to writer
		if r, ok := data.(io.Reader); ok {
			_, err := io.Copy(writer, r)
			return err
		}

		return fmt.Errorf("%T is not a supported type for BinaryProducer (requires []byte or io.Reader)", data)
	})
}
