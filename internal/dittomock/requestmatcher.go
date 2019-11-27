package dittomock

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"grpc-ditto/internal/logger"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/bhmj/jsonslice"
)

var (
	ErrNotMatched = errors.New("dittomock: request not matched")
)

type RequestMatherOption func(*RequestMatcher)

func WithMocksPath(mocksPath string) RequestMatherOption {
	return func(rm *RequestMatcher) {
		rm.mocksPath = mocksPath
	}
}

func WithLogger(l logger.Logger) RequestMatherOption {
	return func(rm *RequestMatcher) {
		rm.logger = l
	}
}

func WithMocks(mocks []DittoMock) RequestMatherOption {
	return func(rm *RequestMatcher) {
		rules := map[string][]DittoMock{}
		mergeMocks(mocks, rules)
		rm.rules = rules
	}
}

type RequestMatcher struct {
	rules     map[string][]DittoMock
	logger    logger.Logger
	mocksPath string
}

func (rm *RequestMatcher) Match(method string, json []byte) (*DittoResponse, error) {
	mocks, ok := rm.rules[method]
	if !ok {
		return nil, ErrNotMatched
	}

	for _, mock := range mocks {
		res, err := rm.matches(json, mock.Request)
		if err != nil {
			rm.logger.Warnw("matching error", "err", err)
			continue
		}

		if res {
			return &mock.Response, nil
		}
	}

	return nil, ErrNotMatched
}

func NewRequestMatcher(opts ...RequestMatherOption) (*RequestMatcher, error) {
	matcher := &RequestMatcher{
		rules: map[string][]DittoMock{},
	}

	for _, opt := range opts {
		opt(matcher)
	}

	if matcher.logger == nil {
		matcher.logger = logger.NewLogger()
	}

	if matcher.mocksPath != "" {
		err := filepath.Walk(matcher.mocksPath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if filepath.Ext(path) == ".json" {
				mocks, err := matcher.loadMock(path)
				if err != nil {
					return err
				}
				mergeMocks(mocks, matcher.rules)
			}
			return nil
		})

		if err != nil {
			return nil, err
		}
	}

	return matcher, nil
}

func (rm *RequestMatcher) loadMock(mockJson string) ([]DittoMock, error) {
	mocks := []DittoMock{}
	js, err := ioutil.ReadFile(mockJson)
	if err != nil {
		return mocks, err
	}
	msg := json.RawMessage{}
	err = json.Unmarshal(js, &msg)
	if err != nil {
		return mocks, err
	}
	if msg[0] == '[' {
		err := json.Unmarshal(msg, &mocks)
		if err != nil {
			return mocks, err
		}
	} else {
		var m DittoMock
		err := json.Unmarshal(msg, &m)
		if err != nil {
			return mocks, err
		}

		mocks = append(mocks, m)

	}

	return mocks, nil
}

func (rm *RequestMatcher) matches(json []byte, req *DittoRequest) (bool, error) {
	result := false
	for _, pattern := range req.BodyPatterns {
		if len(pattern.EqualToJson) > 0 {
			val, err := jsonMatcher(json, pattern.EqualToJson)
			if err != nil || !val {
				return false, err
			}

			result = true
		}
		if pattern.MatchesJsonPath != nil {
			val, err := jsonPathMatcher(json, pattern.MatchesJsonPath)
			if err != nil || !val {
				return false, err
			}

			result = true
		}

	}

	return result, nil
}

func jsonPathMatcher(jsonSrc []byte, pattern *JSONPathWrapper) (bool, error) {
	val, err := jsonslice.Get(jsonSrc, pattern.Expression)
	if err != nil {
		return false, fmt.Errorf("jsonslice matching: %w, expr: %s", err, pattern.Expression)
	}

	if len(val) == 0 {
		return false, nil
	}

	if pattern.Partial {
		return len(val) > 0, nil
	}

	if pattern.Equals != "" {
		strVal := ""
		err = json.Unmarshal(val, &strVal)
		if err != nil {
			return false, err
		}
		return strings.EqualFold(strVal, pattern.Equals), nil
	}

	if pattern.Contains != "" {
		return strings.Contains(string(val), pattern.Contains), nil
	}

	return false, nil
}

func jsonMatcher(jsonVal []byte, expetedJson json.RawMessage) (bool, error) {
	src, err := canonicalJSON(jsonVal)
	if err != nil {
		return false, err
	}

	expected, err := canonicalJSON(expetedJson)
	if err != nil {
		return false, err
	}

	return bytes.Equal(src, expected), nil
}

func mergeMocks(mocks []DittoMock, group map[string][]DittoMock) {
	for _, m := range mocks {
		methodMocks, ok := group[m.Request.Method]
		if !ok {
			methodMocks = []DittoMock{}
		}
		methodMocks = append(methodMocks, m)
		group[m.Request.Method] = methodMocks
	}
}

func canonicalJSON(src []byte) ([]byte, error) {
	var val interface{}
	err := json.Unmarshal(src, &val)
	if err != nil {
		return nil, err
	}

	canonicalJson, err := json.Marshal(val)
	if err != nil {
		return nil, err
	}

	return canonicalJson, nil
}