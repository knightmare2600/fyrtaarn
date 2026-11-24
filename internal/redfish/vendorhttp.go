package redfish

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// vendorResponse is what a vendor backend needs back from a request:
// status, headers (a job's Location) and the body.
type vendorResponse struct {
	status int
	header http.Header
	body   []byte
}

// doJSON sends body (nil for none) as JSON with method and returns the
// response, an error only for transport failures; callers judge the status.
func doJSON(c *http.Client, method, url, user, pass string, body any) (vendorResponse, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return vendorResponse{}, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, rd)
	if err != nil {
		return vendorResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("OData-Version", "4.0")
	req.Header.Set("Accept", "application/json")
	if user != "" {
		req.SetBasicAuth(user, pass)
	}
	resp, err := c.Do(req)
	if err != nil {
		return vendorResponse{}, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return vendorResponse{status: resp.StatusCode, header: resp.Header, body: b}, nil
}

// postXML posts an XML document and returns the response body; an HTTP
// status of 400 or more is an error.
func postXML(c *http.Client, url string, doc []byte) ([]byte, error) {
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(doc))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "text/xml")
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return b, &httpStatusError{resp.StatusCode}
	}
	return b, nil
}

type httpStatusError struct{ code int }

func (e *httpStatusError) Error() string { return fmt.Sprintf("HTTP %d", e.code) }
