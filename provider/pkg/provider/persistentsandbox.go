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
	"fmt"
	"slices"
	"sort"
	"strings"

	pb "github.com/modal-labs/modal-client/go/proto/modal_proto"
	pgp "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
)

// ---------------------------------------------------------------------------
// PersistentSandbox resource
// ---------------------------------------------------------------------------

// Each session of a persistent Sandbox is limited like a regular one; Modal
// pauses a session that hits a limit and resumes it on next use. The Go SDK
// (v0.11.2, sandbox_impl_persistent.go) sends these values in the definition
// and refuses a caller-supplied timeout, so the provider sends the same.
const (
	persistentSessionTimeoutSecs     = 60 * 60
	persistentSessionIdleTimeoutSecs = 5 * 60
)

// PersistentSandbox provisions a long-lived Modal Sandbox that survives across
// sessions: Modal pauses it when idle and resumes it on use, and it exists
// until it is terminated.
type PersistentSandbox struct{}

// PersistentSandboxArgs are the user-supplied inputs. Modal cannot change a
// Sandbox definition after creation, so every input replaces the resource.
type PersistentSandboxArgs struct {
	// TokenID authenticates with the Modal API. Falls back to the
	// provider-level tokenId when not set.
	TokenID *string `pulumi:"tokenId,optional" provider:"secret"`
	// TokenSecret authenticates with the Modal API. Falls back to the
	// provider-level tokenSecret when not set.
	TokenSecret *string `pulumi:"tokenSecret,optional" provider:"secret"`

	// AppId is the ID of the Modal App that owns the Sandbox.
	AppId string `pulumi:"appId"`
	// ImageId is the ID of the Image the Sandbox runs.
	ImageId string `pulumi:"imageId"`
	// Command is the entrypoint command and arguments.
	Command []string `pulumi:"command,optional"`
	// Workdir is the working directory inside the Sandbox.
	Workdir *string `pulumi:"workdir,optional"`
	// Cpu is the CPU request in cores.
	Cpu *float64 `pulumi:"cpu,optional"`
	// MemoryMiB is the memory request in MiB.
	MemoryMiB *int `pulumi:"memoryMiB,optional"`
	// Regions restricts where the Sandbox may be scheduled.
	Regions []string `pulumi:"regions,optional"`
	// BlockNetwork disables all network access.
	BlockNetwork *bool `pulumi:"blockNetwork,optional"`
	// SecretIds are the IDs of Modal Secrets injected as environment variables.
	SecretIds []string `pulumi:"secretIds,optional"`
	// Volumes maps a mount path inside the Sandbox to a Volume ID.
	Volumes map[string]string `pulumi:"volumes,optional"`
}

// PersistentSandboxState holds the inputs and computed outputs.
type PersistentSandboxState struct {
	PersistentSandboxArgs

	// PersistentSandboxId is the Modal-assigned ID of the persistent Sandbox.
	PersistentSandboxId string `pulumi:"persistentSandboxId"`
	// SandboxId is the ID of the session running now. It changes whenever
	// Modal pauses and resumes the Sandbox, and is empty while it is paused.
	SandboxId string `pulumi:"sandboxId"`
}

func (r *PersistentSandbox) Annotate(a infer.Annotator) {
	a.Describe(&r, "A persistent Modal Sandbox: a long-lived container that Modal pauses when idle and resumes on use, "+
		"and that exists until it is terminated. Uses Modal's experimental persistent-sandbox API (the Go SDK's "+
		"`ExperimentalPersistent` option), which Modal may change. A running Sandbox is billed by Modal, so destroy it "+
		"when you no longer need it. Modal cannot change a Sandbox definition after creation and does not return it, so "+
		"every input replaces the resource, and an imported Sandbox keeps the live definition until you replace it. "+
		"Named Sandboxes, GPUs, environment variables, tags and caller-set timeouts are not supported by Modal for "+
		"persistent Sandboxes; put environment variables in a Secret and pass it in secretIds. "+
		"See https://modal.com/docs/guide/sandbox.")
}

func (a *PersistentSandboxArgs) Annotate(ann infer.Annotator) {
	ann.Describe(&a.TokenID, "Modal API token ID. Overrides the provider-level tokenId.")
	ann.Describe(&a.TokenSecret, "Modal API token secret. Overrides the provider-level tokenSecret.")
	ann.Describe(&a.AppId, "The ID of the Modal App that owns the Sandbox (the appId output of an App). Changing this forces a new resource.")
	ann.Describe(&a.ImageId, "The ID of the Image the Sandbox runs (the imageId output of an Image). Changing this forces a new resource.")
	ann.Describe(&a.Command, "The entrypoint command and arguments, for example [\"sleep\", \"infinity\"]. Changing this forces a new resource.")
	ann.Describe(&a.Workdir, "The working directory inside the Sandbox. Changing this forces a new resource.")
	ann.Describe(&a.Cpu, "The CPU request in cores. Changing this forces a new resource.")
	ann.Describe(&a.MemoryMiB, "The memory request in MiB. Changing this forces a new resource.")
	ann.Describe(&a.Regions, "Regions the Sandbox may be scheduled in. Changing this forces a new resource.")
	ann.Describe(&a.BlockNetwork, "Disable all network access. Changing this forces a new resource.")
	ann.Describe(&a.SecretIds, "IDs of Modal Secrets injected as environment variables (the secretId output of a Secret). Changing this forces a new resource.")
	ann.Describe(&a.Volumes, "Maps a mount path inside the Sandbox to a Volume ID (the volumeId output of a Volume). Changing this forces a new resource.")
}

func (s *PersistentSandboxState) Annotate(ann infer.Annotator) {
	ann.Describe(&s.PersistentSandboxId, "The Modal-assigned ID of the persistent Sandbox. Stable for its whole life, and the ID to import.")
	ann.Describe(&s.SandboxId, "The ID of the session running now. It changes each time Modal pauses and resumes the Sandbox, so a refresh can show a new value; it is empty while the Sandbox is paused.")
}

var _ infer.CustomResource[PersistentSandboxArgs, PersistentSandboxState] = (*PersistentSandbox)(nil)
var _ infer.CustomDiff[PersistentSandboxArgs, PersistentSandboxState] = (*PersistentSandbox)(nil)
var _ infer.CustomRead[PersistentSandboxArgs, PersistentSandboxState] = (*PersistentSandbox)(nil)
var _ infer.CustomDelete[PersistentSandboxState] = (*PersistentSandbox)(nil)

// sortedCopy returns a sorted copy so list inputs compare as sets.
func sortedCopy(in []string) []string {
	out := slices.Clone(in)
	sort.Strings(out)
	return out
}

func sameStringSet(a, b []string) bool {
	return slices.Equal(sortedCopy(a), sortedCopy(b))
}

func sameStringMap(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

func derefFloat(f *float64) float64 {
	if f == nil {
		return 0
	}
	return *f
}

func derefInt(i *int) int {
	if i == nil {
		return 0
	}
	return *i
}

func derefBool(b *bool) bool {
	return b != nil && *b
}

var _ infer.CustomCheck[PersistentSandboxArgs] = (*PersistentSandbox)(nil)

// validatePersistentArgs reports invalid definition fields as (property, reason)
// pairs. It runs in Check so a bad value fails at preview: a change forces a
// replace, and the live Sandbox is deleted before the replacement is created,
// so an apply-time failure would destroy a working Sandbox.
func validatePersistentArgs(args PersistentSandboxArgs) []pgp.CheckFailure {
	var f []pgp.CheckFailure
	if args.Cpu != nil && *args.Cpu <= 0 {
		f = append(f, pgp.CheckFailure{Property: "cpu", Reason: fmt.Sprintf("cpu (%v) must be a positive number", *args.Cpu)})
	}
	if args.MemoryMiB != nil && *args.MemoryMiB <= 0 {
		f = append(f, pgp.CheckFailure{Property: "memoryMiB", Reason: fmt.Sprintf("memoryMiB (%d) must be a positive number", *args.MemoryMiB)})
	}
	if args.Workdir != nil && !strings.HasPrefix(*args.Workdir, "/") {
		f = append(f, pgp.CheckFailure{Property: "workdir", Reason: fmt.Sprintf("workdir (%q) must be an absolute path", *args.Workdir)})
	}
	return f
}

// Check validates the definition at preview time.
func (r *PersistentSandbox) Check(ctx context.Context, req infer.CheckRequest) (infer.CheckResponse[PersistentSandboxArgs], error) {
	args, failures, err := infer.DefaultCheck[PersistentSandboxArgs](ctx, req.NewInputs)
	if err != nil {
		return infer.CheckResponse[PersistentSandboxArgs]{}, err
	}
	failures = append(failures, validatePersistentArgs(args)...)
	return infer.CheckResponse[PersistentSandboxArgs]{Inputs: args, Failures: dropUnknownFailures(failures, req.NewInputs)}, nil
}

// Diff replaces the resource when any input changes: the Modal API has no call
// that edits a persistent Sandbox definition. A Sandbox adopted by import has no
// recorded definition (Modal does not return it), so nothing is compared and the
// live definition is kept until the user replaces the resource.
func (r *PersistentSandbox) Diff(_ context.Context, req infer.DiffRequest[PersistentSandboxArgs, PersistentSandboxState]) (infer.DiffResponse, error) {
	resp := infer.DiffResponse{DetailedDiff: map[string]pgp.PropertyDiff{}}
	in, st := req.Inputs, req.State.PersistentSandboxArgs
	if st.AppId == "" && st.ImageId == "" {
		return resp, nil
	}
	replace := func(field string, changed bool) {
		if changed {
			resp.DetailedDiff[field] = pgp.PropertyDiff{Kind: pgp.UpdateReplace}
			resp.HasChanges = true
		}
	}
	replace("appId", in.AppId != st.AppId)
	replace("imageId", in.ImageId != st.ImageId)
	replace("command", !slices.Equal(in.Command, st.Command))
	replace("workdir", derefStr(in.Workdir) != derefStr(st.Workdir))
	replace("cpu", derefFloat(in.Cpu) != derefFloat(st.Cpu))
	replace("memoryMiB", derefInt(in.MemoryMiB) != derefInt(st.MemoryMiB))
	replace("regions", !sameStringSet(in.Regions, st.Regions))
	replace("blockNetwork", derefBool(in.BlockNetwork) != derefBool(st.BlockNetwork))
	replace("secretIds", !sameStringSet(in.SecretIds, st.SecretIds))
	replace("volumes", !sameStringMap(in.Volumes, st.Volumes))
	return resp, nil
}

// buildPersistentDefinition builds the Sandbox definition sent to Modal. It
// mirrors what the Go SDK sends for ExperimentalPersistent (sandbox.go,
// buildSandboxCreateRequestProto and sandbox_impl_persistent.go in v0.11.2),
// limited to the fields this resource models.
func buildPersistentDefinition(args PersistentSandboxArgs) (*pb.Sandbox, error) {
	if args.AppId == "" {
		return nil, fmt.Errorf("modal: appId is required")
	}
	if args.ImageId == "" {
		return nil, fmt.Errorf("modal: imageId is required")
	}
	if f := validatePersistentArgs(args); len(f) > 0 {
		return nil, fmt.Errorf("modal: %s", f[0].Reason)
	}
	resources := pb.Resources_builder{}
	if args.Cpu != nil {
		if *args.Cpu <= 0 {
			return nil, fmt.Errorf("modal: cpu (%v) must be a positive number", *args.Cpu)
		}
		resources.MilliCpu = uint32(1000 * *args.Cpu)
	}
	if args.MemoryMiB != nil {
		if *args.MemoryMiB <= 0 {
			return nil, fmt.Errorf("modal: memoryMiB (%d) must be a positive number", *args.MemoryMiB)
		}
		resources.MemoryMb = uint32(*args.MemoryMiB)
	}

	network := pb.NetworkAccess_builder{
		NetworkAccessType: pb.NetworkAccess_OPEN,
		AllowedCidrs:      []string{},
		AllowedDomains:    []string{},
	}
	if derefBool(args.BlockNetwork) {
		network.NetworkAccessType = pb.NetworkAccess_BLOCKED
	}

	mountPaths := make([]string, 0, len(args.Volumes))
	for p := range args.Volumes {
		mountPaths = append(mountPaths, p)
	}
	sort.Strings(mountPaths)
	mounts := make([]*pb.VolumeMount, 0, len(mountPaths))
	for _, p := range mountPaths {
		mounts = append(mounts, pb.VolumeMount_builder{
			VolumeId:               args.Volumes[p],
			MountPath:              p,
			AllowBackgroundCommits: true,
		}.Build())
	}

	var placement *pb.SchedulerPlacement
	if len(args.Regions) > 0 {
		placement = pb.SchedulerPlacement_builder{Regions: args.Regions}.Build()
	}
	idle := uint32(persistentSessionIdleTimeoutSecs)
	def := pb.Sandbox_builder{
		EntrypointArgs:     args.Command,
		ImageId:            args.ImageId,
		SecretIds:          args.SecretIds,
		TimeoutSecs:        persistentSessionTimeoutSecs,
		IdleTimeoutSecs:    &idle,
		Workdir:            args.Workdir,
		NetworkAccess:      network.Build(),
		Resources:          resources.Build(),
		VolumeMounts:       mounts,
		OpenPorts:          pb.PortSpecs_builder{Ports: []*pb.PortSpec{}}.Build(),
		SchedulerPlacement: placement,
	}.Build()
	return def, nil
}

func (r *PersistentSandbox) Create(ctx context.Context, req infer.CreateRequest[PersistentSandboxArgs]) (infer.CreateResponse[PersistentSandboxState], error) {
	args := req.Inputs
	cfg := infer.GetConfig[ProviderConfig](ctx)
	if req.DryRun {
		return infer.CreateResponse[PersistentSandboxState]{ID: "", Output: PersistentSandboxState{PersistentSandboxArgs: args}}, nil
	}
	def, err := buildPersistentDefinition(args)
	if err != nil {
		return infer.CreateResponse[PersistentSandboxState]{}, err
	}
	client, err := clientFor(cfg, args.TokenID, args.TokenSecret)
	if err != nil {
		return infer.CreateResponse[PersistentSandboxState]{}, fmt.Errorf("modal: %w", err)
	}
	defer client.Close()

	rpcCtx, cancel := callCtx()
	defer cancel()
	resp, err := client.stub.PersistentSandboxCreate(rpcCtx, pb.PersistentSandboxCreateRequest_builder{
		AppId:      args.AppId,
		Definition: def,
	}.Build())
	if err != nil {
		return infer.CreateResponse[PersistentSandboxState]{}, fmt.Errorf("modal: create persistent sandbox: %w", err)
	}
	state := PersistentSandboxState{
		PersistentSandboxArgs: args,
		PersistentSandboxId:   resp.GetPersistentSandboxId(),
		SandboxId:             resp.GetSandboxId(),
	}
	return infer.CreateResponse[PersistentSandboxState]{ID: resp.GetPersistentSandboxId(), Output: state}, nil
}

// Read refreshes the Sandbox by its persistent ID, so it works for refresh and
// for import from the ID alone. A terminated Sandbox is gone for good (Modal
// does not resume it), so it is dropped from state like a deleted one.
func (r *PersistentSandbox) Read(ctx context.Context, req infer.ReadRequest[PersistentSandboxArgs, PersistentSandboxState]) (infer.ReadResponse[PersistentSandboxArgs, PersistentSandboxState], error) {
	cfg := infer.GetConfig[ProviderConfig](ctx)
	empty := infer.ReadResponse[PersistentSandboxArgs, PersistentSandboxState]{}
	client, err := clientFor(cfg, req.State.TokenID, req.State.TokenSecret)
	if err != nil {
		return empty, fmt.Errorf("modal: %w", err)
	}
	defer client.Close()

	rpcCtx, cancel := callCtx()
	defer cancel()
	id := req.ID
	resp, err := client.stub.PersistentSandboxGet(rpcCtx, pb.PersistentSandboxGetRequest_builder{PersistentSandboxId: &id}.Build())
	if isNotFound(err) || (err == nil && resp.HasTerminatedAt()) {
		if err := notFoundOnImport("persistent sandbox", id, req.Inputs, req.State); err != nil {
			return empty, err
		}
		return empty, nil
	}
	if err != nil {
		return empty, fmt.Errorf("modal: read persistent sandbox %q: %w", id, err)
	}

	state := req.State
	state.PersistentSandboxId = id
	state.SandboxId = resp.GetSandboxId()
	inputs := req.Inputs
	inputs.TokenID, inputs.TokenSecret = state.TokenID, state.TokenSecret
	if req.State.AppId != "" || req.State.ImageId != "" {
		// Modal does not return the definition, so keep what was recorded.
		inputs = state.PersistentSandboxArgs
	}
	return infer.ReadResponse[PersistentSandboxArgs, PersistentSandboxState]{ID: id, Inputs: inputs, State: state}, nil
}

// Delete terminates the Sandbox. One that is already terminated or gone is a
// success.
func (r *PersistentSandbox) Delete(ctx context.Context, req infer.DeleteRequest[PersistentSandboxState]) (infer.DeleteResponse, error) {
	cfg := infer.GetConfig[ProviderConfig](ctx)
	client, err := clientFor(cfg, req.State.TokenID, req.State.TokenSecret)
	if err != nil {
		return infer.DeleteResponse{}, fmt.Errorf("modal: %w", err)
	}
	defer client.Close()

	rpcCtx, cancel := callCtx()
	defer cancel()
	id := req.ID
	got, err := client.stub.PersistentSandboxGet(rpcCtx, pb.PersistentSandboxGetRequest_builder{PersistentSandboxId: &id}.Build())
	if isNotFound(err) || (err == nil && got.HasTerminatedAt()) {
		return infer.DeleteResponse{}, nil
	}
	_, err = client.stub.PersistentSandboxTerminate(rpcCtx, pb.PersistentSandboxTerminateRequest_builder{PersistentSandboxId: id}.Build())
	if err != nil && !isNotFound(err) {
		return infer.DeleteResponse{}, fmt.Errorf("modal: terminate persistent sandbox %q: %w", id, err)
	}
	return infer.DeleteResponse{}, nil
}

// ---------------------------------------------------------------------------
// getPersistentSandbox
// ---------------------------------------------------------------------------

// GetPersistentSandbox looks up a persistent Sandbox by ID.
type GetPersistentSandbox struct{}

// GetPersistentSandboxArgs selects the Sandbox.
type GetPersistentSandboxArgs struct {
	// PersistentSandboxId is the Modal-assigned ID of the persistent Sandbox.
	PersistentSandboxId string `pulumi:"persistentSandboxId"`
}

// GetPersistentSandboxResult is the looked-up Sandbox.
type GetPersistentSandboxResult struct {
	PersistentSandboxId string `pulumi:"persistentSandboxId"`
	SandboxId           string `pulumi:"sandboxId"`
	Terminated          bool   `pulumi:"terminated"`
	TerminatedAt        string `pulumi:"terminatedAt"`
}

func (a *GetPersistentSandboxArgs) Annotate(ann infer.Annotator) {
	ann.Describe(&a.PersistentSandboxId, "The Modal-assigned ID of the persistent Sandbox.")
}

func (r *GetPersistentSandboxResult) Annotate(ann infer.Annotator) {
	ann.Describe(&r.PersistentSandboxId, "The Modal-assigned ID of the persistent Sandbox.")
	ann.Describe(&r.SandboxId, "The ID of the session running now; empty while the Sandbox is paused or terminated.")
	ann.Describe(&r.Terminated, "Whether the Sandbox has been terminated.")
	ann.Describe(&r.TerminatedAt, "When the Sandbox was terminated, as an RFC 3339 timestamp; empty while it exists.")
}

func (f *GetPersistentSandbox) Annotate(a infer.Annotator) {
	a.Describe(&f, "Look up a persistent Modal Sandbox by ID. Uses Modal's experimental persistent-sandbox API.")
}

func (f *GetPersistentSandbox) Invoke(ctx context.Context, req infer.FunctionRequest[GetPersistentSandboxArgs]) (infer.FunctionResponse[GetPersistentSandboxResult], error) {
	out := infer.FunctionResponse[GetPersistentSandboxResult]{}
	client, _, closeFn, err := lookupSession(ctx, nil)
	if err != nil {
		return out, err
	}
	defer closeFn()
	rpcCtx, cancel := callCtx()
	defer cancel()
	id := req.Input.PersistentSandboxId
	got, err := client.stub.PersistentSandboxGet(rpcCtx, pb.PersistentSandboxGetRequest_builder{PersistentSandboxId: &id}.Build())
	if err != nil {
		if isNotFound(err) {
			return out, fmt.Errorf("modal: persistent sandbox %q was not found", id)
		}
		return out, fmt.Errorf("modal: look up persistent sandbox %q: %w", id, err)
	}
	res := GetPersistentSandboxResult{PersistentSandboxId: got.GetPersistentSandboxId(), SandboxId: got.GetSandboxId()}
	if got.HasTerminatedAt() {
		res.Terminated = true
		res.TerminatedAt = got.GetTerminatedAt().AsTime().UTC().Format("2006-01-02T15:04:05Z07:00")
	}
	if res.PersistentSandboxId == "" {
		res.PersistentSandboxId = id
	}
	out.Output = res
	return out, nil
}
