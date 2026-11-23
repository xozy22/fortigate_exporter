// Copyright The Prometheus Authors
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

package fortigatehttpclient

import (
	"errors"
	"fmt"
)

// HTTPStatusError is returned when the FortiGate answers an API request with
// a status code other than 200. It allows callers to react to specific codes,
// e.g. to treat "404 Not Found" as "endpoint not available on this model".
type HTTPStatusError struct {
	StatusCode int
	Path       string
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("response code was %d, expected 200 (path: %q)", e.StatusCode, e.Path)
}

// StatusCode returns the HTTP status code carried by err, if err is (or wraps)
// an *HTTPStatusError.
func StatusCode(err error) (int, bool) {
	var se *HTTPStatusError
	if errors.As(err, &se) {
		return se.StatusCode, true
	}
	return 0, false
}
