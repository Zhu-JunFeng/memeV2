package logger

import (
	"os"
	"time"

	"github.com/rs/zerolog"
)

func New() zerolog.Logger {
	writer := zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}
	return zerolog.New(writer).With().Timestamp().Logger()
}
