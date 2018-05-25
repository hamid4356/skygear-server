// Copyright 2015-present Oursky Ltd.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package router

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/skygeario/skygear-server/pkg/server/logging"
	"github.com/skygeario/skygear-server/pkg/server/skyerr"
	"github.com/skygeario/skygear-server/pkg/server/skyversion"
)

// commonRouter implements the HandlerFunc interface that is common
// to Router and Gateway.
type commonRouter struct {
	payloadFunc      func(req *http.Request) (p *Payload, err error)
	matchHandlerFunc func(p *Payload) (routeConfig, error)
	ResponseTimeout  time.Duration
}

func (r *commonRouter) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	var (
		resp    Response
		payload *Payload
		err     error
	)

	version := strings.TrimPrefix(skyversion.Version(), "v")
	w.Header().Set("Server", fmt.Sprintf("Skygear Server/%s", version))
	resp.writer = w

	// Create request, response struct and match handler
	payload, err = r.payloadFunc(req)
	if err != nil {
		resp.Err = skyerr.NewRequestJSONInvalidErr(err)
		httpStatus := defaultStatusCode(resp.Err)
		w.WriteHeader(httpStatus)
		return
	}
	r.HandlePayload(payload, &resp)
}

func (r *commonRouter) HandlePayload(payload *Payload, resp *Response) {
	var (
		httpStatus = http.StatusOK
		timedOut   bool
	)

	logger := logging.CreateLogger(payload.Context(), "router")

	defer func() {
		if r := recover(); r != nil {
			resp.Err = errorFromRecoveringPanic(r)
			logger.WithField("recovered", r).Errorln("panic occurred while handling request")
		}

		writer := resp.Writer()
		if writer == nil {
			// The response is already written.
			return
		}

		writer.Header().Set("Content-Type", "application/json")

		if timedOut {
			resp.Err = skyerr.NewError(
				skyerr.ResponseTimeout,
				"Service taking too long to respond.",
			)
			logger.Errorln("timed out serving request")
		}

		if resp.Err != nil && httpStatus >= 200 && httpStatus <= 299 {
			httpStatus = defaultStatusCode(resp.Err)
		}

		writer.WriteHeader(httpStatus)
		if err := writeEntity(writer, resp); err != nil {
			panic(err)
		}
	}()

	rc, err := r.matchHandlerFunc(payload)
	if err != nil {
		httpStatus = http.StatusNotFound
		resp.Err = skyerr.NewError(skyerr.UndefinedOperation, err.Error())
		return
	}

	// Call handler
	var cancelFunc context.CancelFunc
	ctx := payload.Context()
	ctx, cancelFunc = context.WithCancel(ctx)
	defer cancelFunc()
	// We use a string for context key here (instead of type) because the same
	// keys have to be shared better the `router` and the `logging` package.
	// This key is supposed to be in `router` package, but declaring this
	// key in the `router` package introduce a circular dependency.
	payload.SetContext(context.WithValue(ctx, "RequestTag", rc.Tag)) // nolint: golint

	go func() {
		httpStatus = r.callHandler(
			rc.Handler,
			rc.Preprocessors,
			payload,
			resp,
		)
		cancelFunc()
	}()

	// This function will return in one of the following conditions:
	select {
	case <-payload.Context().Done():
		// request conext cancelled or response generated
	case <-getTimeoutChan(r.ResponseTimeout):
		// timeout exceeded
		timedOut = true
	}
}

func (r *commonRouter) callHandler(handler Handler, pp []Processor, payload *Payload, resp *Response) (httpStatus int) {
	logger := logging.CreateLogger(payload.Context(), "router")
	httpStatus = http.StatusOK

	defer func() {
		if r := recover(); r != nil {
			logger.WithField("recovered", r).Errorln("panic occurred while handling request")

			resp.Err = errorFromRecoveringPanic(r)
			httpStatus = defaultStatusCode(resp.Err)
		}
	}()

	for _, p := range pp {
		httpStatus = p.Preprocess(payload, resp)
		if resp.Err != nil {
			if httpStatus == http.StatusOK {
				httpStatus = defaultStatusCode(resp.Err)
			}
			return
		}
	}

	handler.Handle(payload, resp)
	return httpStatus
}

func writeEntity(w http.ResponseWriter, i interface{}) error {
	if w == nil {
		return errors.New("writer is nil")
	}
	return json.NewEncoder(w).Encode(i)
}

func getTimeoutChan(timeout time.Duration) <-chan time.Time {
	if timeout.Seconds() > 0 {
		return time.After(timeout)
	}
	return make(chan time.Time)
}

type routeConfig struct {
	Tag           string
	Preprocessors []Processor
	Handler
}
