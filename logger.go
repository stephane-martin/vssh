package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Credits: gorilla/handlers

type LogFormatterParams struct {
	Request    *http.Request
	URL        url.URL
	TimeStamp  time.Time
	StatusCode int
	Size       int
}

type loggingHandler struct {
	writer    io.Writer
	handler   http.Handler
	formatter func(writer io.Writer, params LogFormatterParams)
}

func (h loggingHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	t := time.Now()
	logger := &responseLogger{w: w, status: http.StatusOK}
	u := *req.URL

	h.handler.ServeHTTP(logger, req)

	params := LogFormatterParams{
		Request:    req,
		URL:        u,
		TimeStamp:  t,
		StatusCode: logger.Status(),
		Size:       logger.Size(),
	}

	h.formatter(h.writer, params)
}

func LoggingHandler(out io.Writer, h http.Handler) http.Handler {
	return loggingHandler{out, h, writeLog}
}

func writeLog(writer io.Writer, params LogFormatterParams) {
	buf := buildCommonLogLine(params.Request, params.URL, params.TimeStamp, params.StatusCode, params.Size)
	_, _ = io.WriteString(writer, buf)
}

func buildCommonLogLine(req *http.Request, url url.URL, ts time.Time, status int, size int) string {
	username := "-"
	if url.User != nil {
		if name := url.User.Username(); name != "" {
			username = name
		}
	}

	host, _, err := net.SplitHostPort(req.RemoteAddr)

	if err != nil {
		host = req.RemoteAddr
	}

	uri := req.RequestURI

	// Requests using the CONNECT method over HTTP/2.0 must use
	// the authority field (aka r.Host) to identify the target.
	// Refer: https://httpwg.github.io/specs/rfc7540.html#CONNECT
	if req.ProtoMajor == 2 && req.Method == "CONNECT" {
		uri = req.Host
	}
	if uri == "" {
		uri = url.RequestURI()
	}

	var buf strings.Builder
	buf.Grow(3 * (len(host) + len(username) + len(req.Method) + len(uri) + len(req.Proto) + 50) / 2)
	buf.WriteString(fmt.Sprintf("[blue]%s[-]", host))
	buf.WriteString(" - ")
	buf.WriteString(username)
	buf.WriteString(" [")
	buf.WriteString(fmt.Sprintf("[wheat]%s[-]", ts.Format("02/Jan/2006:15:04:05 -0700")))
	buf.WriteString(`] "`)
	buf.WriteString(fmt.Sprintf("[yellowgreen]%s[-]", req.Method))
	buf.WriteByte(' ')
	buf.WriteString("[violet]")
	appendQuoted(&buf, uri)
	buf.WriteString("[-]")
	buf.WriteByte(' ')
	buf.WriteString(req.Proto)
	buf.WriteString(`" `)
	buf.WriteString(fmt.Sprintf("[green]%d[-]", status))
	buf.WriteByte(' ')
	buf.WriteString(strconv.Itoa(size))
	buf.WriteByte('\n')
	return buf.String()
}

const lowerhex = "0123456789abcdef"

func appendQuoted(buf *strings.Builder, s string) {
	var runeTmp [utf8.UTFMax]byte
	for width := 0; len(s) > 0; s = s[width:] {
		r := rune(s[0])
		width = 1
		if r >= utf8.RuneSelf {
			r, width = utf8.DecodeRuneInString(s)
		}
		if width == 1 && r == utf8.RuneError {
			buf.WriteString(`\x`)
			buf.WriteByte(lowerhex[s[0]>>4])
			buf.WriteByte(lowerhex[s[0]&0xF])
			continue
		}
		if r == rune('"') || r == '\\' { // always backslashed
			buf.WriteByte('\\')
			buf.WriteByte(byte(r))
			continue
		}
		if strconv.IsPrint(r) {
			n := utf8.EncodeRune(runeTmp[:], r)
			buf.WriteString(string(runeTmp[:n]))
			continue
		}
		switch r {
		case '\a':
			buf.WriteString(`\a`)
		case '\b':
			buf.WriteString(`\b`)
		case '\f':
			buf.WriteString(`\f`)
		case '\n':
			buf.WriteString(`\n`)
		case '\r':
			buf.WriteString(`\r`)
		case '\t':
			buf.WriteString(`\t`)
		case '\v':
			buf.WriteString(`\v`)
		default:
			switch {
			case r < ' ':
				buf.WriteString(`\x`)
				buf.WriteByte(lowerhex[s[0]>>4])
				buf.WriteByte(lowerhex[s[0]&0xF])
			case r > utf8.MaxRune:
				r = 0xFFFD
				fallthrough
			case r < 0x10000:
				buf.WriteString(`\u`)
				for s := 12; s >= 0; s -= 4 {
					buf.WriteByte(lowerhex[r>>uint(s)&0xF])
				}
			default:
				buf.WriteString(`\U`)
				for s := 28; s >= 0; s -= 4 {
					buf.WriteByte(lowerhex[r>>uint(s)&0xF])
				}
			}
		}
	}
}

type responseLogger struct {
	w      http.ResponseWriter
	status int
	size   int
}

func (l *responseLogger) Header() http.Header {
	return l.w.Header()
}

func (l *responseLogger) Write(b []byte) (int, error) {
	size, err := l.w.Write(b)
	l.size += size
	return size, err
}

func (l *responseLogger) WriteHeader(s int) {
	l.w.WriteHeader(s)
	l.status = s
}

func (l *responseLogger) Status() int {
	return l.status
}

func (l *responseLogger) Size() int {
	return l.size
}

func (l *responseLogger) Flush() {
	f, ok := l.w.(http.Flusher)
	if ok {
		f.Flush()
	}
}

func Logger(level string) (*zap.SugaredLogger, error) {
	zcfg := zap.NewDevelopmentConfig()
	zcfg.DisableCaller = true
	zcfg.DisableStacktrace = true
	zcfg.Sampling = nil
	zcfg.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	//zcfg := zap.NewProductionConfig()
	//zcfg.Encoding = "console"
	//zcfg := zap.NewDevelopmentConfig()
	loglevel := zapcore.DebugLevel
	_ = loglevel.Set(level)
	zcfg.Level.SetLevel(loglevel)
	zcfg.Sampling = nil
	l, err := zcfg.Build()
	if err != nil {
		return nil, fmt.Errorf("unable to initialize zap logger: %s", err)
	}
	return l.Sugar(), nil
}
