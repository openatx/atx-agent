package subcmd

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/alecthomas/kingpin"
	"github.com/codeskyblue/goreq"
)

type HTTPHeaderValue http.Header

func (h *HTTPHeaderValue) Set(value string) error {
	parts := strings.SplitN(value, ":", 2)
	if len(parts) != 2 {
		return fmt.Errorf("expected HEADER:VALUE got '%s'", value)
	}
	(*http.Header)(h).Add(parts[0], parts[1])
	return nil
}

func (h *HTTPHeaderValue) String() string {
	return ""
}

func (h *HTTPHeaderValue) IsCumulative() bool {
	return true
}

func HTTPHeader(s kingpin.Settings) (target *http.Header) {
	target = &http.Header{}
	s.SetValue((*HTTPHeaderValue)(target))
	return
}

type HTTPURLValue url.Values

func (h *HTTPURLValue) Set(value string) error {
	parts := strings.SplitN(value, "=", 2)
	if len(parts) != 2 {
		return fmt.Errorf("expected KEY=VALUE got '%s'", value)
	}
	(*url.Values)(h).Add(parts[0], parts[1])
	return nil
}

func (h *HTTPURLValue) String() string {
	return ""
}

func (h *HTTPURLValue) IsCumulative() bool {
	return true
}

func HTTPValue(s kingpin.Settings) (target *url.Values) {
	target = &url.Values{}
	s.SetValue((*HTTPURLValue)(target))
	return
}

var (
	method   string
	reqUrl   string
	headers  *http.Header
	values   *url.Values
	bodyData string
)

func RegisterCurl(curl *kingpin.CmdClause) {
	curl.Flag("request", "Specify request command to use").Short('X').Default("GET").StringVar(&method)
	curl.Arg("url", "url string").Required().StringVar(&reqUrl)
	curl.Flag("data", "body data").StringVar(&bodyData)
	headers = HTTPHeader(curl.Flag("header", "Add a HTTP header to the request.").Short('H'))
	values = HTTPValue(curl.Flag("form", "Add a HTTP form values").Short('F'))
}

func DoCurl() {
	if !regexp.MustCompile(`^https?://`).MatchString(reqUrl) {
		reqUrl = "http://" + reqUrl
	}
	request := goreq.Request{
		Method: method,
		Uri:    reqUrl,
	}
	request.ShowDebug = true
	for k, values := range *headers {
		for _, v := range values {
			request.AddHeader(k, v)
		}
	}
	if method == "GET" {
		request.QueryString = *values
	} else if method == "POST" {
		if bodyData != "" {
			request.Body = bodyData
		} else {
			request.Body = *values
		}
	} else {
		log.Fatalf("Unsupported method: %s", method)
	}
	res, err := request.Do()
	if err != nil {
		log.Fatal(err)
	}
	log.Println(res.Body.ToString())
}
