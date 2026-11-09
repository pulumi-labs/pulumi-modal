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
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	pb "github.com/modal-labs/modal-client/go/proto/modal_proto"
	pgp "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ---------------------------------------------------------------------------
// AppDeployment resource
// ---------------------------------------------------------------------------

// AppDeployment deploys a Modal App's code. Modal compiles a Python app file into
// Functions and Classes on the client side (it cloudpickles the code and
// builds the Function definitions), and the control plane only accepts the
// result, so the one supported way to ship code is the Modal CLI. This
// resource runs `modal deploy` for an app file and then manages the resulting
// deployed App: it reads it back, reports its web endpoints, and stops it on
// delete.
type AppDeployment struct{}

// AppDeploymentArgs are the user-supplied inputs for a Modal AppDeployment.
type AppDeploymentArgs struct {
	// TokenID authenticates with the Modal API. Falls back to the
	// provider-level tokenId when not set.
	TokenID *string `pulumi:"tokenId,optional" provider:"secret"`
	// TokenSecret authenticates with the Modal API. Falls back to the
	// provider-level tokenSecret when not set.
	TokenSecret *string `pulumi:"tokenSecret,optional" provider:"secret"`

	// Name is the deployed App's name (the CLI's --name). Changing this forces
	// a new resource.
	Name string `pulumi:"name"`
	// AppFile is the path to the Python file that defines the Modal App.
	AppFile string `pulumi:"appFile"`
	// Environment is the Modal environment to deploy into. Changing this
	// forces a new resource. Falls back to the provider-level environment.
	Environment *string `pulumi:"environment,optional"`
	// Tag labels this deployment version (the CLI's --tag).
	Tag *string `pulumi:"tag,optional"`
	// SourceDir is the directory whose contents are fingerprinted to decide
	// when to redeploy. Defaults to the directory of appFile.
	SourceDir *string `pulumi:"sourceDir,optional"`
	// WorkingDirectory is where the CLI runs, which decides how the app file
	// imports its sibling modules. Defaults to the directory of appFile.
	WorkingDirectory *string `pulumi:"workingDirectory,optional"`
	// ModalCommand is the command (and leading arguments) that starts the
	// Modal CLI. Defaults to ["modal"].
	ModalCommand []string `pulumi:"modalCommand,optional"`
	// Env adds environment variables to the CLI process, for example the
	// secrets or settings the app file reads while it is being built.
	Env map[string]string `pulumi:"env,optional" provider:"secret"`
	// Triggers redeploy the App whenever any value changes, for inputs the
	// source fingerprint cannot see, such as a pinned dependency version.
	Triggers map[string]string `pulumi:"triggers,optional"`
}

// AppDeploymentState holds the computed outputs for a Modal AppDeployment.
type AppDeploymentState struct {
	AppDeploymentArgs

	// AppId is the Modal-assigned identifier of the deployed App.
	AppId string `pulumi:"appId"`
	// SourceHash is the fingerprint of the source directory at the last deploy.
	SourceHash string `pulumi:"sourceHash"`
	// Functions are the names of the Functions the deployed App exposes.
	Functions []string `pulumi:"functions"`
	// WebUrls maps a Function name to its public web endpoint URL, for
	// Functions that have one.
	WebUrls map[string]string `pulumi:"webUrls"`
}

func (r *AppDeployment) Annotate(a infer.Annotator) {
	a.Describe(&r, "Deploys a Modal App from a Python app file by running `modal deploy`, and manages the deployed App. "+
		"The Modal CLI must be installed on the machine that runs Pulumi (`pip install modal`), because Modal builds Function "+
		"definitions client side and only the CLI can ship them. The App is redeployed in place whenever the contents of the "+
		"source directory (or a trigger) change, and stopped on delete. Reports the App's Functions and web endpoint URLs. "+
		"Importing by App ID restores the outputs but not appFile, so the first update after an import redeploys. "+
		"See https://modal.com/docs/guide/managing-deployments.")
}

func (a *AppDeploymentArgs) Annotate(ann infer.Annotator) {
	ann.Describe(&a.TokenID, "Modal API token ID. Overrides the provider-level tokenId.")
	ann.Describe(&a.TokenSecret, "Modal API token secret. Overrides the provider-level tokenSecret.")
	ann.Describe(&a.Name, "The deployed App's name, passed to the CLI as --name. Changing this forces a new resource.")
	ann.Describe(&a.AppFile, "Path to the Python file that defines the Modal App. A relative path resolves against the "+
		"directory Pulumi runs the program from.")
	ann.Describe(&a.Environment, "The Modal environment to deploy into. Changing this forces a new resource. "+
		"When unset, the provider-level environment or the account's default environment is used.")
	ann.Describe(&a.Tag, "Label for this deployment version, passed to the CLI as --tag.")
	ann.Describe(&a.SourceDir, "Directory whose contents are fingerprinted to decide when to redeploy; .git, __pycache__, "+
		".venv, venv and node_modules are skipped. Defaults to the directory of appFile.")
	ann.Describe(&a.WorkingDirectory, "Directory the Modal CLI runs in. Defaults to the directory of appFile.")
	ann.Describe(&a.ModalCommand, "The command, with any leading arguments, that starts the Modal CLI, for example "+
		"[\"uv\", \"run\", \"modal\"]. Defaults to [\"modal\"].")
	ann.Describe(&a.Env, "Extra environment variables for the CLI process, for values the app file reads while it is being built. "+
		"Treated as secret. The Modal credentials, environment and endpoint are always set from the provider configuration.")
	ann.Describe(&a.Triggers, "Values that redeploy the App when any of them changes, for inputs the source fingerprint cannot see.")
}

func (s *AppDeploymentState) Annotate(ann infer.Annotator) {
	ann.Describe(&s.AppId, "The Modal-assigned identifier of the deployed App.")
	ann.Describe(&s.SourceHash, "SHA-256 fingerprint of the source directory at the last deploy.")
	ann.Describe(&s.Functions, "Names of the Functions the deployed App exposes, sorted.")
	ann.Describe(&s.WebUrls, "Public web endpoint URL of each Function that has one, keyed by Function name.")
}

var _ infer.CustomResource[AppDeploymentArgs, AppDeploymentState] = (*AppDeployment)(nil)
var _ infer.CustomDiff[AppDeploymentArgs, AppDeploymentState] = (*AppDeployment)(nil)
var _ infer.CustomUpdate[AppDeploymentArgs, AppDeploymentState] = (*AppDeployment)(nil)
var _ infer.CustomRead[AppDeploymentArgs, AppDeploymentState] = (*AppDeployment)(nil)
var _ infer.CustomDelete[AppDeploymentState] = (*AppDeployment)(nil)

// ---------------------------------------------------------------------------
// Source fingerprint
// ---------------------------------------------------------------------------

// skippedSourceDirs are never part of the fingerprint: they change without the
// app's code changing.
var skippedSourceDirs = map[string]bool{
	".git": true, "__pycache__": true, ".venv": true, "venv": true,
	"node_modules": true, ".mypy_cache": true, ".pytest_cache": true, ".ruff_cache": true,
}

const maxFingerprintFiles = 20000

func (a AppDeploymentArgs) sourceRoot() string {
	if a.SourceDir != nil && *a.SourceDir != "" {
		return *a.SourceDir
	}
	return filepath.Dir(a.AppFile)
}

// sourceFingerprint hashes the relative path and contents of every file under
// root. A missing root yields "" so a Diff on a not-yet-generated tree stays
// quiet and Create reports the real error.
func sourceFingerprint(root string) (string, error) {
	info, err := os.Stat(root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("source directory %q is not a directory", root)
	}
	var files []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && skippedSourceDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		files = append(files, path)
		if len(files) > maxFingerprintFiles {
			return fmt.Errorf("more than %d files under %q; point sourceDir at the app's own directory", maxFingerprintFiles, root)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(files)
	h := sha256.New()
	for _, f := range files {
		rel, err := filepath.Rel(root, f)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%s\x00", filepath.ToSlash(rel))
		fh, err := os.Open(f)
		if err != nil {
			return "", err
		}
		_, err = io.Copy(h, fh)
		_ = fh.Close()
		if err != nil {
			return "", err
		}
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ---------------------------------------------------------------------------
// Diff
// ---------------------------------------------------------------------------

func eqStrMap(a, b map[string]string) bool {
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

func eqStrSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func eqStrPtr(a, b *string) bool { return derefStr(a) == derefStr(b) }

// Diff replaces on a name or environment change (an App cannot be renamed or
// moved) and redeploys in place for everything else, including a change to the
// source directory's contents.
func (r *AppDeployment) Diff(ctx context.Context, req infer.DiffRequest[AppDeploymentArgs, AppDeploymentState]) (infer.DiffResponse, error) {
	cfg := infer.GetConfig[ProviderConfig](ctx)
	in, st := req.Inputs, req.State
	resp := infer.DiffResponse{DetailedDiff: map[string]pgp.PropertyDiff{}}
	update := func(field string) { resp.DetailedDiff[field] = pgp.PropertyDiff{Kind: pgp.Update} }

	if in.Name != st.Name {
		resp.DetailedDiff["name"] = pgp.PropertyDiff{Kind: pgp.UpdateReplace}
	}
	if environmentChanged(in.Environment, st.Environment, withResourceTokens(cfg, in.TokenID, in.TokenSecret)) {
		resp.DetailedDiff["environment"] = pgp.PropertyDiff{Kind: pgp.UpdateReplace}
	}
	if in.AppFile != st.AppFile {
		update("appFile")
	}
	if !eqStrPtr(in.Tag, st.Tag) {
		update("tag")
	}
	if !eqStrPtr(in.SourceDir, st.SourceDir) {
		update("sourceDir")
	}
	if !eqStrPtr(in.WorkingDirectory, st.WorkingDirectory) {
		update("workingDirectory")
	}
	if !eqStrSlice(in.ModalCommand, st.ModalCommand) {
		update("modalCommand")
	}
	if !eqStrMap(in.Env, st.Env) {
		update("env")
	}
	if !eqStrMap(in.Triggers, st.Triggers) {
		update("triggers")
	}
	// Compare the source fingerprint only when both sides have one; an
	// imported AppDeployment has none in state, which counts as changed.
	if in.AppFile != "" {
		hash, err := sourceFingerprint(in.sourceRoot())
		if err != nil {
			return resp, fmt.Errorf("modal: fingerprint %q: %w", in.sourceRoot(), err)
		}
		if hash != "" && hash != st.SourceHash {
			update("sourceHash")
		}
	}
	resp.HasChanges = len(resp.DetailedDiff) > 0
	return resp, nil
}

// ---------------------------------------------------------------------------
// Running the CLI
// ---------------------------------------------------------------------------

const deployTimeout = 30 * time.Minute

// cliServerURL turns the gRPC host:port override into the https URL the CLI's
// MODAL_SERVER_URL expects (plain http for a loopback test endpoint).
func cliServerURL(addr string) string {
	if addr == "" || strings.Contains(addr, "://") {
		return addr
	}
	host, _, err := net.SplitHostPort(addr)
	if err == nil && (host == "localhost" || net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()) {
		return "http://" + addr
	}
	return "https://" + addr
}

// redactor masks secret values in text that is going to be shown to a user.
type redactor []string

func (r redactor) apply(s string) string {
	for _, secret := range r {
		if len(secret) >= 4 {
			s = strings.ReplaceAll(s, secret, "[redacted]")
		}
	}
	return s
}

func tailString(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return "..." + s[len(s)-n:]
}

// runModalDeploy runs `modal deploy` for the app file with the resolved
// credentials in its environment and returns the tail of its output on error.
func runModalDeploy(ctx context.Context, cfg ProviderConfig, args AppDeploymentArgs, environment string) error {
	tokenID, tokenSecret, serverAddr, err := credentialsFor(cfg, args.TokenID, args.TokenSecret)
	if err != nil {
		return err
	}
	appFile, err := filepath.Abs(args.AppFile)
	if err != nil {
		return err
	}
	if _, err := os.Stat(appFile); err != nil {
		return fmt.Errorf("appFile %q: %w", args.AppFile, err)
	}
	workdir := filepath.Dir(appFile)
	if args.WorkingDirectory != nil && *args.WorkingDirectory != "" {
		if workdir, err = filepath.Abs(*args.WorkingDirectory); err != nil {
			return err
		}
	}

	command := args.ModalCommand
	if len(command) == 0 {
		command = []string{"modal"}
	}
	exe, err := exec.LookPath(command[0])
	if err != nil {
		return fmt.Errorf("the Modal CLI %q was not found on PATH; install it with `pip install modal` "+
			"or set modalCommand to a command that starts it: %w", command[0], err)
	}
	argv := append(append([]string{}, command[1:]...), "deploy", "--name", args.Name)
	if environment != "" {
		argv = append(argv, "--env", environment)
	}
	if args.Tag != nil && *args.Tag != "" {
		argv = append(argv, "--tag", *args.Tag)
	}
	argv = append(argv, appFile)

	ctx, cancel := context.WithTimeout(ctx, deployTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, argv...)
	cmd.Dir = workdir
	cmd.Env = cliEnv(os.Environ(), tokenID, tokenSecret, environment, serverAddr, args.Env)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		secrets := redactor{tokenID, tokenSecret}
		for _, v := range args.Env {
			secrets = append(secrets, v)
		}
		if ctx.Err() != nil {
			err = fmt.Errorf("timed out after %s: %w", deployTimeout, err)
		}
		return fmt.Errorf("`modal deploy` failed: %w\n%s", err, secrets.apply(tailString(out.String(), 4000)))
	}
	return nil
}

// cliEnv builds the CLI's environment: the caller's own, minus any MODAL_*
// settings that would compete with the provider's, plus the resolved ones and
// the user's extras.
func cliEnv(base []string, tokenID, tokenSecret, environment, serverAddr string, extra map[string]string) []string {
	env := make([]string, 0, len(base)+len(extra)+4)
	for _, kv := range base {
		switch name, _, _ := strings.Cut(kv, "="); name {
		case "MODAL_TOKEN_ID", "MODAL_TOKEN_SECRET", "MODAL_ENVIRONMENT", "MODAL_SERVER_URL", "MODAL_PROFILE":
			continue
		}
		env = append(env, kv)
	}
	env = append(env, "MODAL_TOKEN_ID="+tokenID, "MODAL_TOKEN_SECRET="+tokenSecret)
	if environment != "" {
		env = append(env, "MODAL_ENVIRONMENT="+environment)
	}
	if u := cliServerURL(serverAddr); u != "" {
		env = append(env, "MODAL_SERVER_URL="+u)
	}
	keys := make([]string, 0, len(extra))
	for k := range extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		env = append(env, k+"="+extra[k])
	}
	return env
}

// ---------------------------------------------------------------------------
// Create / Update
// ---------------------------------------------------------------------------

// postDeployError marks a failure that happened after `modal deploy` exited
// 0: the App is live (and billed), so the caller must keep partial state.
type postDeployError struct{ err error }

func (e postDeployError) Error() string { return e.err.Error() }
func (e postDeployError) Unwrap() error { return e.err }

// deployed runs the deploy and reads the resulting App back. When it fails
// after the CLI succeeded it returns the partial state (App ID when known)
// together with a postDeployError, so Pulumi keeps tracking the live App.
func (r *AppDeployment) deployed(ctx context.Context, args AppDeploymentArgs) (AppDeploymentState, error) {
	cfg := infer.GetConfig[ProviderConfig](ctx)
	client, err := clientFor(cfg, args.TokenID, args.TokenSecret)
	if err != nil {
		return AppDeploymentState{}, fmt.Errorf("modal: %w", err)
	}
	defer client.Close()

	rpcCtx, cancel := callCtx()
	environment := effectiveEnvironment(rpcCtx, client, args.Environment, withResourceTokens(cfg, args.TokenID, args.TokenSecret))
	cancel()

	hash, err := sourceFingerprint(args.sourceRoot())
	if err != nil {
		return AppDeploymentState{}, fmt.Errorf("modal: fingerprint %q: %w", args.sourceRoot(), err)
	}
	if err := runModalDeploy(ctx, cfg, args, environment); err != nil {
		return AppDeploymentState{}, fmt.Errorf("modal: deploy %q: %w", args.Name, err)
	}

	// From here the App exists. Keep what we know in state on any failure.
	// Functions and WebUrls are required outputs: leave them empty, not absent,
	// so the partial state still decodes on the next Read or Delete.
	state := AppDeploymentState{AppDeploymentArgs: args, AppId: args.Name, SourceHash: hash,
		Functions: []string{}, WebUrls: map[string]string{}}
	if environment != "" {
		state.Environment = &environment
	}
	rpcCtx, cancel = callCtx()
	defer cancel()
	id, ok, err := lookupAppID(rpcCtx, client, args.Name, environment)
	if err != nil {
		return state, postDeployError{fmt.Errorf("modal: look up deployed app %q: %w", args.Name, err)}
	}
	if !ok {
		return state, postDeployError{fmt.Errorf("modal: `modal deploy` succeeded but no app named %q exists in %s; "+
			"check that the app file declares modal.App(%q) or that --name overrides it", args.Name, environmentLabel(environment), args.Name)}
	}
	state.AppId = id
	state.Functions, state.WebUrls, err = readDeployedFunctions(rpcCtx, client, id, args.Name, environment)
	if err != nil {
		return state, postDeployError{err}
	}
	return state, nil
}

// readDeployedFunctions lists an App's Functions and the web URL of each one
// that has one.
func readDeployedFunctions(ctx context.Context, client *modalClient, appID, appName, environment string) ([]string, map[string]string, error) {
	layout, err := client.stub.AppGetLayout(ctx, pb.AppGetLayoutRequest_builder{AppId: appID}.Build())
	if err != nil {
		return nil, nil, fmt.Errorf("modal: read the layout of app %q: %w", appName, err)
	}
	names := make([]string, 0, len(layout.GetAppLayout().GetFunctionIds()))
	for name := range layout.GetAppLayout().GetFunctionIds() {
		names = append(names, name)
	}
	sort.Strings(names)
	urls := map[string]string{}
	for _, name := range names {
		got, err := client.stub.FunctionGet(ctx, pb.FunctionGetRequest_builder{
			AppName: appName, ObjectTag: name, EnvironmentName: environment,
		}.Build())
		if err != nil {
			if isNotFound(err) {
				continue
			}
			return nil, nil, fmt.Errorf("modal: read function %q of app %q: %w", name, appName, err)
		}
		if u := got.GetHandleMetadata().GetWebUrl(); u != "" {
			urls[name] = u
		}
	}
	return names, urls, nil
}

func (r *AppDeployment) Create(ctx context.Context, req infer.CreateRequest[AppDeploymentArgs]) (infer.CreateResponse[AppDeploymentState], error) {
	args := req.Inputs
	cfg := infer.GetConfig[ProviderConfig](ctx)
	if req.DryRun {
		state := AppDeploymentState{AppDeploymentArgs: args}
		if env := resolveEnv(args.Environment, withResourceTokens(cfg, args.TokenID, args.TokenSecret)); env != "" {
			state.Environment = &env
		}
		return infer.CreateResponse[AppDeploymentState]{ID: "", Output: state}, nil
	}
	state, err := r.deployed(ctx, args)
	var post postDeployError
	if errors.As(err, &post) {
		// The App is live: keep it in state so destroy can stop it, and let
		// the next refresh recover the rest.
		return infer.CreateResponse[AppDeploymentState]{ID: state.AppId, Output: state},
			infer.ResourceInitFailedError{Reasons: []string{post.Error()}}
	}
	if err != nil {
		return infer.CreateResponse[AppDeploymentState]{}, err
	}
	return infer.CreateResponse[AppDeploymentState]{ID: state.AppId, Output: state}, nil
}

// Update redeploys the App in place.
func (r *AppDeployment) Update(ctx context.Context, req infer.UpdateRequest[AppDeploymentArgs, AppDeploymentState]) (infer.UpdateResponse[AppDeploymentState], error) {
	if req.DryRun {
		state := req.State
		state.AppDeploymentArgs = req.Inputs
		return infer.UpdateResponse[AppDeploymentState]{Output: state}, nil
	}
	state, err := r.deployed(ctx, req.Inputs)
	var post postDeployError
	if errors.As(err, &post) {
		// Keep the App ID we already have if the lookup could not find it again.
		if state.AppId == req.Inputs.Name {
			state.AppId = req.ID
		}
		return infer.UpdateResponse[AppDeploymentState]{Output: state},
			infer.ResourceInitFailedError{Reasons: []string{post.Error()}}
	}
	if err != nil {
		return infer.UpdateResponse[AppDeploymentState]{}, err
	}
	return infer.UpdateResponse[AppDeploymentState]{Output: state}, nil
}

// resolveAppID turns a resource ID into an App ID. After a deploy whose
// read-back failed, the ID is the App's name (the ID was not known yet), so
// look the App up by name in the recorded environment. Real App IDs start with
// "ap-" and are used as they are.
func resolveAppID(ctx context.Context, client *modalClient, id string, state AppDeploymentState) (string, error) {
	if strings.HasPrefix(id, "ap-") || state.Name == "" {
		return id, nil
	}
	found, ok, err := lookupAppID(ctx, client, state.Name, derefStr(state.Environment))
	if err != nil {
		return "", err
	}
	if !ok {
		return id, nil
	}
	return found, nil
}

// ---------------------------------------------------------------------------
// Read / Delete
// ---------------------------------------------------------------------------

// Read refreshes the deployed App by ID: environment, name, Functions and web
// URLs. A stopped App is reported as gone. An import by ID cannot recover the
// app file, so appFile stays empty and the next update redeploys.
func (r *AppDeployment) Read(ctx context.Context, req infer.ReadRequest[AppDeploymentArgs, AppDeploymentState]) (infer.ReadResponse[AppDeploymentArgs, AppDeploymentState], error) {
	cfg := infer.GetConfig[ProviderConfig](ctx)
	empty := infer.ReadResponse[AppDeploymentArgs, AppDeploymentState]{}
	client, err := clientFor(cfg, req.State.TokenID, req.State.TokenSecret)
	if err != nil {
		return empty, fmt.Errorf("modal: %w", err)
	}
	defer client.Close()

	rpcCtx, cancel := callCtx()
	defer cancel()
	appID, err := resolveAppID(rpcCtx, client, req.ID, req.State)
	if err != nil {
		return empty, fmt.Errorf("modal: read deployment %q: %w", req.ID, err)
	}
	info, err := client.stub.AppGetInfo(rpcCtx, pb.AppGetInfoRequest_builder{AppId: appID}.Build())
	if isNotFound(err) {
		if err := notFoundOnImport("app deployment", req.ID, req.Inputs, req.State); err != nil {
			return empty, err
		}
		return empty, nil
	}
	if err != nil {
		return empty, fmt.Errorf("modal: read deployment %q: %w", req.ID, err)
	}
	if appStopped(info.GetInfo()) {
		if err := notFoundOnImport("app deployment", req.ID, req.Inputs, req.State); err != nil {
			return empty, err
		}
		return empty, nil
	}
	environment := info.GetInfo().GetEnvironmentName()

	list, err := client.stub.AppList(rpcCtx, pb.AppListRequest_builder{EnvironmentName: environment}.Build())
	if err != nil {
		return empty, fmt.Errorf("modal: list apps to read %q: %w", appID, err)
	}
	name, found := "", false
	for _, item := range list.GetApps() {
		if item.GetAppId() == appID {
			name, found = item.GetName(), true
			break
		}
	}
	if !found {
		if err := notFoundOnImport("app deployment", req.ID, req.Inputs, req.State); err != nil {
			return empty, err
		}
		return empty, nil
	}

	importing := req.State.Name == "" && req.Inputs.Name == ""
	state := req.State
	state.AppId = appID
	state.Name = name
	if environment != "" {
		state.Environment = &environment
	}
	state.Functions, state.WebUrls, err = readDeployedFunctions(rpcCtx, client, appID, name, environment)
	if err != nil {
		return empty, err
	}

	inputs := req.Inputs
	inputs.TokenID, inputs.TokenSecret = state.TokenID, state.TokenSecret
	inputs.Name = name
	if importing && environment != "" {
		inputs.Environment = &environment
	}
	return infer.ReadResponse[AppDeploymentArgs, AppDeploymentState]{ID: appID, Inputs: inputs, State: state}, nil
}

// Delete stops the deployed App. Modal retires stopped Apps, so their name
// becomes free.
func (r *AppDeployment) Delete(ctx context.Context, req infer.DeleteRequest[AppDeploymentState]) (infer.DeleteResponse, error) {
	cfg := infer.GetConfig[ProviderConfig](ctx)
	client, err := clientFor(cfg, req.State.TokenID, req.State.TokenSecret)
	if err != nil {
		return infer.DeleteResponse{}, fmt.Errorf("modal: %w", err)
	}
	defer client.Close()

	rpcCtx, cancel := callCtx()
	defer cancel()
	appID, err := resolveAppID(rpcCtx, client, req.ID, req.State)
	if err != nil {
		return infer.DeleteResponse{}, fmt.Errorf("modal: look up deployment %q to stop it: %w", req.State.Name, err)
	}
	_, err = client.stub.AppStop(rpcCtx, pb.AppStopRequest_builder{
		AppId:  appID,
		Source: pb.AppStopSource_APP_STOP_SOURCE_PYTHON_CLIENT,
	}.Build())
	if err != nil && !isNotFound(err) {
		return infer.DeleteResponse{}, fmt.Errorf("modal: stop deployment %q: %w", req.State.Name, err)
	}
	return infer.DeleteResponse{}, nil
}

// ---------------------------------------------------------------------------
// getAppDeployment function
// ---------------------------------------------------------------------------

// GetAppDeployment looks up a deployed App with its Functions and web URLs.
type GetAppDeployment struct{}

// GetAppDeploymentResult is the looked-up deployed App.
type GetAppDeploymentResult struct {
	AppId       string            `pulumi:"appId"`
	Name        string            `pulumi:"name"`
	Environment string            `pulumi:"environment"`
	Stopped     bool              `pulumi:"stopped"`
	Functions   []string          `pulumi:"functions"`
	WebUrls     map[string]string `pulumi:"webUrls"`
}

func (r *GetAppDeploymentResult) Annotate(ann infer.Annotator) {
	ann.Describe(&r.AppId, "The Modal-assigned identifier of the deployed App.")
	ann.Describe(&r.Name, "The deployed App's name.")
	ann.Describe(&r.Environment, "The environment the App is deployed in.")
	ann.Describe(&r.Stopped, "Whether the App is stopped.")
	ann.Describe(&r.Functions, "Names of the Functions the App exposes, sorted.")
	ann.Describe(&r.WebUrls, "Public web endpoint URL of each Function that has one, keyed by Function name.")
}

func (f *GetAppDeployment) Annotate(a infer.Annotator) {
	a.Describe(&f, "Look up a deployed Modal App by name, with its Functions and their web endpoint URLs.")
}

func (f *GetAppDeployment) Invoke(ctx context.Context, req infer.FunctionRequest[NamedLookupArgs]) (infer.FunctionResponse[GetAppDeploymentResult], error) {
	out := infer.FunctionResponse[GetAppDeploymentResult]{}
	client, env, closeFn, err := lookupSession(ctx, req.Input.Environment)
	if err != nil {
		return out, err
	}
	defer closeFn()
	rpcCtx, cancel := callCtx()
	defer cancel()
	got, err := lookupApp(rpcCtx, client, req.Input.Name, env)
	if err != nil {
		return out, lookupErr("deployment", req.Input.Name, env, err)
	}
	if got == nil {
		return out, lookupErr("deployment", req.Input.Name, env, status.Error(codes.NotFound, "not found"))
	}
	env = lookupEnvironment(rpcCtx, client, env)
	if e := got.GetEnvironmentName(); e != "" {
		env = e
	}
	fns, urls, err := readDeployedFunctions(rpcCtx, client, got.GetAppId(), req.Input.Name, env)
	if err != nil {
		return out, err
	}
	out.Output = GetAppDeploymentResult{
		AppId: got.GetAppId(), Name: req.Input.Name, Environment: env,
		Stopped:   got.GetLifecycle().GetAppState() == pb.AppState_APP_STATE_STOPPED,
		Functions: fns, WebUrls: urls,
	}
	return out, nil
}
