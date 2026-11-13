// Copyright 2026, Pulumi Corporation.
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

package provider

import (
	"context"
	"math/rand/v2"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// Modal rate-limits some control-plane RPCs (ImageListTags in particular) and
// answers with ResourceExhausted. Image Read and Delete each list tags several
// times, so a refresh of many Images used to fail on the first rejection
// (#1876). Every unary RPC is therefore retried with capped exponential
// backoff and jitter.
//
// Safety: ResourceExhausted is a rejection before the call runs, so every RPC
// can be retried. Unavailable can also arrive after the server acted (a dropped
// connection), so it is retried only for read-only RPCs (Get*, List*, not
// GetOrCreate, which may create).
var retryPolicy = struct {
	maxAttempts int
	baseDelay   time.Duration
	maxDelay    time.Duration
	// sleep waits for d or until ctx is done; tests replace it.
	sleep func(ctx context.Context, d time.Duration) error
}{
	maxAttempts: 6,
	baseDelay:   250 * time.Millisecond,
	maxDelay:    8 * time.Second,
	sleep: func(ctx context.Context, d time.Duration) error {
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			return nil
		}
	},
}

// isReadOnlyMethod reports whether the full gRPC method name is a read-only RPC.
func isReadOnlyMethod(method string) bool {
	name := method[strings.LastIndex(method, "/")+1:]
	if strings.Contains(name, "GetOrCreate") {
		return false
	}
	return strings.Contains(name, "Get") || strings.Contains(name, "List")
}

func retryableCode(method string, c codes.Code) bool {
	switch c {
	case codes.ResourceExhausted:
		return true
	case codes.Unavailable:
		return isReadOnlyMethod(method)
	}
	return false
}

// retryDelay is the wait before retry number attempt (1-based): the server's
// retry-after trailer (seconds) when present, else exponential backoff with
// full jitter on the upper half, capped at maxDelay.
func retryDelay(attempt int, trailer metadata.MD) time.Duration {
	if v := trailer.Get("retry-after"); len(v) > 0 {
		if secs, err := strconv.ParseFloat(v[0], 64); err == nil && secs >= 0 {
			d := time.Duration(secs * float64(time.Second))
			return min(d, retryPolicy.maxDelay)
		}
	}
	d := retryPolicy.baseDelay << (attempt - 1)
	if d <= 0 || d > retryPolicy.maxDelay {
		d = retryPolicy.maxDelay
	}
	return d/2 + time.Duration(rand.Int64N(int64(d/2)+1))
}

// retryInterceptor retries rate-limited and transiently unavailable unary RPCs.
func retryInterceptor() grpc.UnaryClientInterceptor {
	return func(
		ctx context.Context,
		method string,
		req, reply any,
		cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		opts ...grpc.CallOption,
	) error {
		var err error
		for attempt := 1; ; attempt++ {
			var trailer metadata.MD
			err = invoker(ctx, method, req, reply, cc, append(opts[:len(opts):len(opts)], grpc.Trailer(&trailer))...)
			if err == nil || attempt >= retryPolicy.maxAttempts {
				return err
			}
			if !retryableCode(method, status.Code(err)) {
				return err
			}
			if serr := retryPolicy.sleep(ctx, retryDelay(attempt, trailer)); serr != nil {
				return err
			}
		}
	}
}
