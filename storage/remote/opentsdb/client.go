// Copyright 2013 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package opentsdb

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"github.com/prometheus/log"

	clientmodel "github.com/prometheus/client_golang/model"

	"github.com/prometheus/prometheus/utility"
)

const (
	putEndpoint     = "/api/put"
	contentTypeJSON = "application/json"
)

var (
	illegalCharsRE = regexp.MustCompile(`[^a-zA-Z0-9_\-./]`)
)

// Client allows sending batches of Prometheus samples to OpenTSDB.
type Client struct {
	url        string
	httpClient *http.Client
}

// NewClient creates a new Client.
func NewClient(url string, timeout time.Duration) *Client {
	return &Client{
		url:        url,
		httpClient: utility.NewDeadlineClient(timeout),
	}
}

// StoreSamplesRequest is used for building a JSON request for storing samples
// via the OpenTSDB.
type StoreSamplesRequest struct {
	Metric    TagValue            `json:"metric"`
	Timestamp int64               `json:"timestamp"`
	Value     float64             `json:"value"`
	Tags      map[string]TagValue `json:"tags"`
}

// tagsFromMetric translates Prometheus metric into OpenTSDB tags.
func tagsFromMetric(m clientmodel.Metric) map[string]TagValue {
	tags := make(map[string]TagValue, len(m)-1)
	for l, v := range m {
		if l == clientmodel.MetricNameLabel {
			continue
		}
		tags[string(l)] = TagValue(v)
	}
	return tags
}

// Store sends a batch of samples to OpenTSDB via its HTTP API.
func (c *Client) Store(samples clientmodel.Samples) error {
	reqs := make([]StoreSamplesRequest, 0, len(samples))
	for _, s := range samples {
		v := float64(s.Value)
		if math.IsNaN(v) || math.IsInf(v, 0) {
			log.Warnf("cannot send value %f to OpenTSDB, skipping sample %#v", v, s)
			continue
		}
		metric := TagValue(s.Metric[clientmodel.MetricNameLabel])
		reqs = append(reqs, StoreSamplesRequest{
			Metric:    metric,
			Timestamp: s.Timestamp.Unix(),
			Value:     v,
			Tags:      tagsFromMetric(s.Metric),
		})
	}

	u, err := url.Parse(c.url)
	if err != nil {
		return err
	}

	u.Path = putEndpoint

	buf, err := json.Marshal(reqs)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Post(
		u.String(),
		contentTypeJSON,
		bytes.NewBuffer(buf),
	)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// API returns status code 204 for successful writes.
	// http://opentsdb.net/docs/build/html/api_http/put.html
	if resp.StatusCode == http.StatusNoContent {
		return nil
	}

	// API returns status code 400 on error, encoding error details in the
	// response content in JSON.
	buf, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var r map[string]int
	if err := json.Unmarshal(buf, &r); err != nil {
		return err
	}
	return fmt.Errorf("failed to write %d samples to OpenTSDB, %d succeeded", r["failed"], r["success"])
}

// Name identifies the client as an OpenTSDB client.
func (c Client) Name() string {
	return "opentsdb"
}
