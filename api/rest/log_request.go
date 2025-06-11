package rest

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/emicklei/go-restful/v3"
	"github.com/nyaruka/phonenumbers"
	"github.com/tomasen/realip"
)

type LogFilter struct {
	clock timer
	srv   LogServiceServer
}

type timer interface {
	Now() time.Time
	Since(time.Time) time.Duration
}

type realClock struct{}

func (rc *realClock) Now() time.Time {
	return time.Now()
}

func (rc *realClock) Since(t time.Time) time.Duration {
	return time.Since(t)
}

// LoggerDefaultFormat is the format logged used by the default Logger instance.
var LoggerDefaultFormat = "{{.Status}} | ({{.IPAddr}}) {{.Hostname}} | {{.Method}} {{.Path}} {{if .Message}}:: {{.Message}}{{end}}"
var logRequestPrintTemplate *template.Template

func NewRequestLoggingFilter(logSrv LogServiceServer) *LogFilter {
	logRequestPrintTemplate = template.Must(template.New("rest-logger").Parse(LoggerDefaultFormat))
	return &LogFilter{
		clock: &realClock{},
		srv:   logSrv,
	}
}

func (lf *LogFilter) Filter(req *restful.Request, resp *restful.Response, chain *restful.FilterChain) {
	var logReq bool
	logRequestStr, found := os.LookupEnv("LOG_REQUEST")
	if found {
		if strings.TrimSpace(logRequestStr) != "" {
			if val, err := strconv.ParseBool(strings.TrimSpace(logRequestStr)); err == nil {
				logReq = val
			}
		}
	}
	if !logReq {
		chain.ProcessFilter(req, resp)
	} else {
		tNow := lf.clock.Now()
		httpRequest := req.Request
		startTime := tNow.UTC()
		inBody, err := io.ReadAll(httpRequest.Body)
		if err != nil {
			if err != io.EOF && err != io.ErrUnexpectedEOF {
				panic(err)
			}
		}
		requestBody := bytes.NewReader(inBody)
		req.Request.Body = io.NopCloser(requestBody)
		c := NewResponseCapture(resp.ResponseWriter)
		resp.ResponseWriter = c
		chain.ProcessFilter(req, resp)

		latency := lf.clock.Since(startTime)
		ipAddr := realip.FromRequest(httpRequest)
		if ipAddr == "" {
			ipAddr = req.Request.RemoteAddr
		}

		var urlStr string
		if req.Request.URL != nil {
			urlStr = req.Request.URL.String()
		}
		var jsonRequest, jsonHeader, jsonResponse propertyMap
		bHeader, _ := json.Marshal(httpRequest.Header)
		if err := json.Unmarshal(bHeader, &jsonHeader); err == nil {
			lf.mapFilter(jsonHeader)
		}

		if ct := req.HeaderParameter("Content-Type"); ct == restful.MIME_JSON {
			if err := json.Unmarshal(inBody, &jsonRequest); err == nil {
				lf.mapFilter(jsonRequest)
			}
		}

		var respMessage string
		if c.Bytes() != nil {
			if ct := c.Header().Get("Content-Type"); ct == "application/json" {
				if err := json.Unmarshal(c.Bytes(), &jsonResponse); err == nil {
					lf.mapFilter(jsonResponse)
					_ = unmarshalSingle(jsonResponse, "message", &respMessage)
				}
			}
		}

		inBodyLog := struct {
			Header   propertyMap `json:"header,omitempty"`
			Request  propertyMap `json:"request,omitempty"`
			Response propertyMap `json:"response,omitempty"`
		}{
			Header:   jsonHeader,
			Request:  jsonRequest,
			Response: jsonResponse,
		}

		bLogBody, _ := json.Marshal(&inBodyLog)
		if c.status < 200 || c.status >= 300 {
			b := &bytes.Buffer{}
			logEntry := struct {
				Status   int
				IPAddr   string
				Hostname string
				Method   string
				Path     string
				Message  string
				Latency  int64
			}{
				Status:   c.status,
				IPAddr:   ipAddr,
				Hostname: req.Request.Host,
				Method:   req.Request.Method,
				Path:     req.Request.RequestURI,
				Latency:  latency.Microseconds(),
				Message:  respMessage,
			}
			_ = logRequestPrintTemplate.Execute(b, logEntry)

			if reqURI := req.Request.RequestURI; len(reqURI) > 1 && reqURI != "/" {
				switch statusCode := c.status; {
				case statusCode >= 400 && statusCode < 500:
					switch statusCode {
					case 401, 403:
						log.Printf("%s (%s)", b.String(), "The client does not have access rights to the content")
					case 404:
						log.Printf("%s (%s)", b.String(), "The server can not find the requested resource")
					case 429:
						log.Printf("%s (%s)", b.String(), "The user has sent too many requests in a given amount of time")
					}
				case statusCode >= 500 && statusCode < 600:
					log.Printf("%s (%s)", b.String(), "The server has encountered a situation")
				}
			}
		}
		_ = lf.srv.LogRequest(
			c.StatusCode(),
			respMessage,
			urlStr,
			req.Request.Method,
			ipAddr,
			req.Request.Referer(),
			req.Request.UserAgent(),
			latency.Milliseconds(),
			bLogBody,
		)
	}

}

type propertyMap map[string]any

func (p propertyMap) Value() (driver.Value, error) {
	j, err := json.Marshal(p)
	return j, err
}

func (p *propertyMap) Scan(src any) error {
	source, ok := src.([]byte)
	if !ok {
		return errors.New("type assertion .([]byte) failed")
	}

	var i any
	err := json.Unmarshal(source, &i)
	if err != nil {
		return err
	}

	*p, ok = i.(map[string]any)
	if !ok {
		return errors.New("type assertion .(map[string]interface{}) failed")
	}

	return nil
}

type LoggerBody struct {
	Header   propertyMap `json:"header,omitempty"`
	Request  propertyMap `json:"request,omitempty"`
	Response propertyMap `json:"response,omitempty"`
}

type HTTPReqInfo struct {
	Method    string
	Uri       string
	Referer   string
	IPAddr    string
	UserAgent string
	Duration  time.Duration
	Request   *http.Request
}

type ResponseCapture struct {
	http.ResponseWriter
	wroteHeader bool
	status      int
	body        *bytes.Buffer
}

func NewResponseCapture(w http.ResponseWriter) *ResponseCapture {
	return &ResponseCapture{
		ResponseWriter: w,
		wroteHeader:    false,
		body:           new(bytes.Buffer),
	}
}

func (c ResponseCapture) Header() http.Header {
	return c.ResponseWriter.Header()
}

func (c ResponseCapture) Write(data []byte) (int, error) {
	if !c.wroteHeader {
		c.WriteHeader(http.StatusOK)
	}
	c.body.Write(data)
	return c.ResponseWriter.Write(data)
}

func (c *ResponseCapture) WriteHeader(statusCode int) {
	c.status = statusCode
	c.wroteHeader = true
	c.ResponseWriter.WriteHeader(statusCode)
}

func (c ResponseCapture) Bytes() []byte {
	return c.body.Bytes()
}

func (c ResponseCapture) StatusCode() int {
	return c.status
}

const (
	Base64         string = "^(?:[A-Za-z0-9+\\/]{4})*(?:[A-Za-z0-9+\\/]{2}==|[A-Za-z0-9+\\/]{3}=|[A-Za-z0-9+\\/]{4})$"
	PrintableASCII string = "^[\x20-\x7E]+$"
	DataURI        string = "^data:.+\\/(.+);base64$"
	Email          string = "^(((([a-zA-Z]|\\d|[!#\\$%&'\\*\\+\\-\\/=\\?\\^_`{\\|}~]|[\\x{00A0}-\\x{D7FF}\\x{F900}-\\x{FDCF}\\x{FDF0}-\\x{FFEF}])+(\\.([a-zA-Z]|\\d|[!#\\$%&'\\*\\+\\-\\/=\\?\\^_`{\\|}~]|[\\x{00A0}-\\x{D7FF}\\x{F900}-\\x{FDCF}\\x{FDF0}-\\x{FFEF}])+)*)|((\\x22)((((\\x20|\\x09)*(\\x0d\\x0a))?(\\x20|\\x09)+)?(([\\x01-\\x08\\x0b\\x0c\\x0e-\\x1f\\x7f]|\\x21|[\\x23-\\x5b]|[\\x5d-\\x7e]|[\\x{00A0}-\\x{D7FF}\\x{F900}-\\x{FDCF}\\x{FDF0}-\\x{FFEF}])|(\\([\\x01-\\x09\\x0b\\x0c\\x0d-\\x7f]|[\\x{00A0}-\\x{D7FF}\\x{F900}-\\x{FDCF}\\x{FDF0}-\\x{FFEF}]))))*(((\\x20|\\x09)*(\\x0d\\x0a))?(\\x20|\\x09)+)?(\\x22)))@((([a-zA-Z]|\\d|[\\x{00A0}-\\x{D7FF}\\x{F900}-\\x{FDCF}\\x{FDF0}-\\x{FFEF}])|(([a-zA-Z]|\\d|[\\x{00A0}-\\x{D7FF}\\x{F900}-\\x{FDCF}\\x{FDF0}-\\x{FFEF}])([a-zA-Z]|\\d|-|\\.|_|~|[\\x{00A0}-\\x{D7FF}\\x{F900}-\\x{FDCF}\\x{FDF0}-\\x{FFEF}])*([a-zA-Z]|\\d|[\\x{00A0}-\\x{D7FF}\\x{F900}-\\x{FDCF}\\x{FDF0}-\\x{FFEF}])))\\.)+(([a-zA-Z]|[\\x{00A0}-\\x{D7FF}\\x{F900}-\\x{FDCF}\\x{FDF0}-\\x{FFEF}])|(([a-zA-Z]|[\\x{00A0}-\\x{D7FF}\\x{F900}-\\x{FDCF}\\x{FDF0}-\\x{FFEF}])([a-zA-Z]|\\d|-|\\.|_|~|[\\x{00A0}-\\x{D7FF}\\x{F900}-\\x{FDCF}\\x{FDF0}-\\x{FFEF}])*([a-zA-Z]|[\\x{00A0}-\\x{D7FF}\\x{F900}-\\x{FDCF}\\x{FDF0}-\\x{FFEF}])))\\.?$"
	CreditCard     string = "^(?:4[0-9]{12}(?:[0-9]{3})?|5[1-5][0-9]{14}|6(?:011|5[0-9][0-9])[0-9]{12}|3[47][0-9]{13}|3(?:0[0-5]|[68][0-9])[0-9]{11}|(?:2131|1800|35\\d{3})\\d{11})$"
	PhoneNumber    string = "^(?:(?:\\(?(?:00|\\+)([1-4]\\d\\d|[1-9]\\d?)\\)?)?[\\-\\.\\ \\\\/]?)?((?:\\(?\\d{1,}\\)?[\\-\\.\\ \\\\/]?){0,})(?:[\\-\\.\\ \\\\/]?(?:#|ext\\.?|extension|x)[\\-\\.\\ \\\\/]?(\\d+))?$"
)

var (
	rxBase64      = regexp.MustCompile(Base64)
	rxDataURI     = regexp.MustCompile(DataURI)
	rxEmail       = regexp.MustCompile(Email)
	rxCreditCard  = regexp.MustCompile(CreditCard)
	rxPhoneNumber = regexp.MustCompile(PhoneNumber)
)

var SafeFields = []string{"authorization", "api-key", "api", "apikey", "merchant-key", "enterprise-token", "token", "user-token"}

func (lf *LogFilter) mapFilter(aMap map[string]any) {
	for key, val := range aMap {
		if inArray(strings.ToLower(key), SafeFields) {
			delete(aMap, key)
		}
		switch concreteVal := val.(type) {
		case map[string]any:
			lf.mapFilter(val.(map[string]any))

		case []any:
			lf.parseArray(val.([]any))

		default:
			// todo: sanitize data
			// https://gist.github.com/xigang/98504205d86803baedc5
			aMap[key] = concreteVal
			if str, ok := concreteVal.(string); ok {
				strCount := len(str)
				dataURI := strings.Split(str, ",")
				switch {
				case rxDataURI.MatchString(dataURI[0]):
					if rxBase64.MatchString(dataURI[1]) {
						aMap[key] = dataURI[0] + ",****"
					}
				case rxEmail.MatchString(str):
					emailAddr := strings.SplitN(str, "@", 2)
					addr := emailAddr[0]
					for _, c := range []string{"a", "i", "e", "u", "o", "0"} {
						addr = strings.ReplaceAll(strings.ToLower(addr), c, "*")
					}
					aMap[key] = addr + "@" + emailAddr[1]
				case rxCreditCard.MatchString(str):
					aMap[key] = str[:3] + strings.Repeat("*", len(str[3:]))
				case rxPhoneNumber.MatchString(str):
					phoneNumberStr := strings.TrimPrefix(str, "+")
					reLocalPhoneNumber := regexp.MustCompile("^(.*?)0(.*)$")
					repStr := "${1}62$2"
					phoneNumberStr = reLocalPhoneNumber.ReplaceAllString(phoneNumberStr, repStr)
					if _, err := phonenumbers.Parse(phoneNumberStr, ""); err == nil {
						aMap[key] = strings.Repeat("*", len(str[:strCount-4])) + str[strCount-4:]
					}
				case inArray(strings.ToLower(key), SafeFields):
					aMap[key] = "***"
				}
			}

		}
	}
}

func inArray(str string, haystacks []string) bool {
	for _, h := range haystacks {
		if h == str {
			return true
		}
	}
	return false
}

func (lf *LogFilter) parseArray(anArray []any) {
	for _, val := range anArray {
		if data, ok := val.(map[string]any); ok {
			lf.mapFilter(data)
		}
		if data, ok := val.([]any); ok {
			lf.parseArray(data)
		}
	}
}
