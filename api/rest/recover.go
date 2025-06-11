package rest

import (
	"bytes"
	"fmt"
	"html/template"
	"io"
	stdlog "log"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/eapache/go-resiliency/breaker"
	"github.com/timemore/foundation/errors"
)

type Recover struct {
	StackAll   bool
	StackSize  int
	PrintStack bool

	svc RecoveryService
}

func initRec() {
	panicTpl = template.Must(template.New("panic_recovery_tpl").Funcs(template.FuncMap{
		"unescape": func(s string) template.HTML {
			return template.HTML(s)
		},
	}).Parse(notificationTplSource))

	cb = breaker.New(3, 2, time.Minute*1)
}

type RecoveryService interface {
	Notify(msg any) (string, error)
}

func NewRecoveryFilter(svc RecoveryService) *Recover {
	initRec()
	return &Recover{
		PrintStack: true,
		StackAll:   false,
		StackSize:  1024,

		svc: svc,
	}
}

var (
	// circuit breaker
	cb        *breaker.Breaker
	panicText = "(panic) %v"
)

var notificationTplSource = "*\\[{{.Timestamp.Format \"02/01/06 15:04:05 MST\"}}\\]* \n\n" +
	"```\n" +
	"{{.Message}}" +
	"```\n"

var panicTpl *template.Template

func (rec *Recover) RecoverOnPanic(panicReason any, httpWriter http.ResponseWriter) {
	defer func() {
		if !rec.recoveryBreak() {
			if rcv := rec.panicRecover(recover()); rcv != nil {
				panicReason = fmt.Sprintf(panicText, rcv)
			}
			rec.publishError(panicReason, nil, true)
		}
	}()
	if panicReason == io.ErrUnexpectedEOF {
		httpWriter.WriteHeader(http.StatusRequestEntityTooLarge)
		httpWriter.Write([]byte(http.StatusText(http.StatusRequestEntityTooLarge)))
		return
	}

	httpWriter.WriteHeader(http.StatusInternalServerError)
	httpWriter.Write([]byte(fmt.Sprintf(panicText, panicReason)))
}

func (rec *Recover) panicRecover(rc any) error {
	if cb != nil {
		r := cb.Run(func() error {
			return rec.recovery(rc)
		})
		return r
	}
	return rec.recovery(rc)
}

func (rec *Recover) publishError(panicReason any, reqBody []byte, withStackTrace bool) {
	tNow := time.Now()
	var buffer bytes.Buffer
	buffer.WriteString(fmt.Sprintf("%v\r\n", panicReason))
	for i := 2; ; i += 1 {
		pc, file, line, ok := runtime.Caller(i + contextCallerSkipFrameCount)
		if !ok {
			break
		}

		parts := strings.Split(runtime.FuncForPC(pc).Name(), ".")
		partsCount := len(parts)
		pkgPath := ""
		funcName := parts[partsCount-1]
		if parts[partsCount-2][0] == '(' {
			funcName = parts[partsCount-2] + "." + funcName
			pkgPath = strings.Join(parts[0:partsCount-2], ".")
		} else {
			pkgPath = strings.Join(parts[0:partsCount-1], ".")
		}
		buffer.WriteString(fmt.Sprintf("%s:%s\r\n", pkgPath, funcName))
		buffer.WriteString(fmt.Sprintf("  %s:%d\r\n", file, line))
	}

	if rec.PrintStack {
		stdlog.Println(buffer.String())
	}

	buff := new(bytes.Buffer)
	panicMessage := struct {
		Timestamp time.Time
		Message   any
	}{
		Timestamp: tNow,
		Message:   unescape(buffer.String()),
	}
	if err := panicTpl.Execute(buff, panicMessage); err != nil {
		panic(err)
	}

	go func(msg string) {
		_, _ = rec.svc.Notify(msg)
	}(buff.String())
}

func (rec *Recover) recoveryBreak() bool {
	if cb == nil {
		return false
	}

	if err := cb.Run(func() error {
		return nil
	}); err == breaker.ErrBreakerOpen {
		return true
	}
	return false
}

func (rec *Recover) recovery(r any) error {
	var err error
	if r != nil {
		switch t := r.(type) {
		case string:
			err = errors.New(t)
		case error:
			err = t
		default:
			err = errors.New("Unknown error")
		}
	}
	return err

}
