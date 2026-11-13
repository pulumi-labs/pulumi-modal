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
	"net"
	"sync/atomic"
	"testing"
	"time"

	pb "github.com/modal-labs/modal-client/go/proto/modal_proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

// flakyModal fails the first n calls of ImageListTags and EnvironmentList with code.
type flakyModal struct {
	pb.UnimplementedModalClientServer
	failures int32
	code     codes.Code
	calls    atomic.Int32
	trailer  string
}

func (f *flakyModal) ImageListTags(context.Context, *pb.ImageListTagsRequest) (*pb.ImageListTagsResponse, error) {
	if f.calls.Add(1) <= f.failures {
		return nil, status.Error(f.code, "injected")
	}
	return pb.ImageListTagsResponse_builder{}.Build(), nil
}

func (f *flakyModal) SecretDelete(ctx context.Context, _ *pb.SecretDeleteRequest) (*emptypb.Empty, error) {
	_ = grpc.SetTrailer(ctx, metadata.Pairs("retry-after", f.trailer))
	if f.calls.Add(1) <= f.failures {
		return nil, status.Error(f.code, "injected")
	}
	return &emptypb.Empty{}, nil
}

func startFlaky(t *testing.T, f *flakyModal) *modalClient {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	srv := grpc.NewServer()
	pb.RegisterModalClientServer(srv, f)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	c, err := newModalClient("ak-test", "as-test", lis.Addr().String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func fastRetry(t *testing.T) *[]time.Duration {
	t.Helper()
	old := retryPolicy.sleep
	var delays []time.Duration
	retryPolicy.sleep = func(ctx context.Context, d time.Duration) error {
		delays = append(delays, d)
		return ctx.Err()
	}
	t.Cleanup(func() { retryPolicy.sleep = old })
	return &delays
}

func TestRetryResourceExhaustedThenSuccess(t *testing.T) {
	delays := fastRetry(t)
	f := &flakyModal{failures: 3, code: codes.ResourceExhausted}
	c := startFlaky(t, f)
	_, err := listImageTags(context.Background(), c, "main", "im-1")
	require.NoError(t, err)
	assert.EqualValues(t, 4, f.calls.Load())
	assert.Len(t, *delays, 3)
	for i, d := range *delays {
		assert.LessOrEqual(t, d, retryPolicy.maxDelay, "delay %d", i)
	}
}

func TestRetryExhaustionSurfacesError(t *testing.T) {
	fastRetry(t)
	f := &flakyModal{failures: 1000, code: codes.ResourceExhausted}
	c := startFlaky(t, f)
	_, err := listImageTags(context.Background(), c, "main", "im-1")
	require.Error(t, err)
	assert.Equal(t, codes.ResourceExhausted, status.Code(err))
	assert.EqualValues(t, retryPolicy.maxAttempts, f.calls.Load())
}

func TestRetryStopsOnContextCancel(t *testing.T) {
	old := retryPolicy.sleep
	t.Cleanup(func() { retryPolicy.sleep = old })
	ctx, cancel := context.WithCancel(context.Background())
	retryPolicy.sleep = func(ctx context.Context, _ time.Duration) error { cancel(); return ctx.Err() }
	f := &flakyModal{failures: 1000, code: codes.ResourceExhausted}
	c := startFlaky(t, f)
	_, err := listImageTags(ctx, c, "main", "im-1")
	require.Error(t, err)
	assert.EqualValues(t, 1, f.calls.Load())
}

func TestRetryUnavailableOnlyForReadOnly(t *testing.T) {
	fastRetry(t)
	f := &flakyModal{failures: 2, code: codes.Unavailable}
	c := startFlaky(t, f)
	_, err := listImageTags(context.Background(), c, "main", "im-1")
	require.NoError(t, err)
	assert.EqualValues(t, 3, f.calls.Load())

	// SecretDelete is a mutation: Unavailable is not retried.
	g := &flakyModal{failures: 2, code: codes.Unavailable}
	c2 := startFlaky(t, g)
	_, err = c2.stub.SecretDelete(context.Background(), pb.SecretDeleteRequest_builder{SecretId: "st-1"}.Build())
	require.Error(t, err)
	assert.EqualValues(t, 1, g.calls.Load())
}

func TestRetryHonorsRetryAfterTrailer(t *testing.T) {
	delays := fastRetry(t)
	f := &flakyModal{failures: 1, code: codes.ResourceExhausted, trailer: "2"}
	c := startFlaky(t, f)
	// ResourceExhausted is retried for every RPC, including mutations.
	_, err := c.stub.SecretDelete(context.Background(), pb.SecretDeleteRequest_builder{SecretId: "st-1"}.Build())
	require.NoError(t, err)
	require.Len(t, *delays, 1)
	assert.Equal(t, 2*time.Second, (*delays)[0])
}

func TestRetryDelayBounds(t *testing.T) {
	for attempt := 1; attempt < 70; attempt++ {
		d := retryDelay(attempt, nil)
		assert.Greater(t, d, time.Duration(0))
		assert.LessOrEqual(t, d, retryPolicy.maxDelay)
	}
}
