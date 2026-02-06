package pin

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/containers/image/v5/docker/reference"
	"github.com/dustin/go-humanize"
	"github.com/opencontainers/go-digest"
	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
	"golang.org/x/sync/errgroup"
	"golang.org/x/term"

	httpclient "github.com/wharflab/container-source-policy/httpchecksum"
	"github.com/wharflab/container-source-policy/internal/dhi"
	"github.com/wharflab/container-source-policy/internal/dockerfile"
	"github.com/wharflab/container-source-policy/internal/ecrpublic"
	"github.com/wharflab/container-source-policy/internal/git"
	"github.com/wharflab/container-source-policy/internal/policy"
	"github.com/wharflab/container-source-policy/internal/registry"
)

// Options configures the pin operation
type Options struct {
	Dockerfiles     []string
	PreferDHI       bool // Prefer Docker Hardened Images (dhi.io) when available
	PreferECRPublic bool // Prefer AWS ECR Public Gallery (public.ecr.aws) when available
}

// imageTask represents an image to pin
type imageTask struct {
	index    int // original order in Dockerfile
	original string
	ref      reference.Named
}

// httpTask represents an HTTP source to checksum
type httpTask struct {
	index int // original order in Dockerfile
	url   string
}

// gitTask represents a git source to resolve
type gitTask struct {
	index int // original order in Dockerfile
	url   string
}

// pinResult holds the result of a pin operation
type pinResult struct {
	index    int // original order in Dockerfile
	original string
	pinned   string
}

// httpResult holds the result of an HTTP checksum operation
type httpResult struct {
	index    int // original order in Dockerfile
	url      string
	checksum string
	headers  map[string]string
}

// gitResult holds the result of a git resolution
type gitResult struct {
	index    int // original order in Dockerfile
	url      string
	checksum string
}

// taskCollector collects unique tasks from Dockerfiles
type taskCollector struct {
	imageTasks []imageTask
	httpTasks  []httpTask
	gitTasks   []gitTask
	seenImages map[string]bool
	seenHTTP   map[string]bool
	seenGit    map[string]bool
	orderIndex int
}

func newTaskCollector() *taskCollector {
	return &taskCollector{
		seenImages: make(map[string]bool),
		seenHTTP:   make(map[string]bool),
		seenGit:    make(map[string]bool),
	}
}

func (c *taskCollector) collect(ctx context.Context, dockerfilePath string) error {
	parseResult, err := dockerfile.ParseAllFile(ctx, dockerfilePath)
	if err != nil {
		return fmt.Errorf("failed to parse %s: %w", dockerfilePath, err)
	}

	for _, ref := range parseResult.Images {
		if c.seenImages[ref.Original] {
			continue
		}
		c.seenImages[ref.Original] = true

		if _, ok := ref.Ref.(reference.Digested); ok {
			continue
		}

		c.imageTasks = append(c.imageTasks, imageTask{
			index:    c.orderIndex,
			original: ref.Original,
			ref:      ref.Ref,
		})
		c.orderIndex++
	}

	for _, httpRef := range parseResult.HTTPSources {
		if c.seenHTTP[httpRef.URL] {
			continue
		}
		c.seenHTTP[httpRef.URL] = true
		c.httpTasks = append(c.httpTasks, httpTask{index: c.orderIndex, url: httpRef.URL})
		c.orderIndex++
	}

	for _, gitRef := range parseResult.GitSources {
		if c.seenGit[gitRef.URL] {
			continue
		}
		c.seenGit[gitRef.URL] = true
		c.gitTasks = append(c.gitTasks, gitTask{index: c.orderIndex, url: gitRef.URL})
		c.orderIndex++
	}

	return nil
}

func (c *taskCollector) isEmpty() bool {
	return len(c.imageTasks) == 0 && len(c.httpTasks) == 0 && len(c.gitTasks) == 0
}

// resultCollector safely collects results from concurrent operations
type resultCollector struct {
	pinResults  []pinResult
	httpResults []httpResult
	gitResults  []gitResult
	mu          sync.Mutex
}

func (r *resultCollector) addPin(result pinResult) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pinResults = append(r.pinResults, result)
}

func (r *resultCollector) addHTTP(result httpResult) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.httpResults = append(r.httpResults, result)
}

func (r *resultCollector) addGit(result gitResult) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gitResults = append(r.gitResults, result)
}

func (r *resultCollector) buildPolicy() *policy.Policy {
	// Sort results by original Dockerfile order
	slices.SortFunc(r.pinResults, func(a, b pinResult) int { return cmp.Compare(a.index, b.index) })
	slices.SortFunc(r.httpResults, func(a, b httpResult) int { return cmp.Compare(a.index, b.index) })
	slices.SortFunc(r.gitResults, func(a, b gitResult) int { return cmp.Compare(a.index, b.index) })

	pol := policy.NewPolicy()

	for _, res := range r.pinResults {
		policy.AddPinRule(pol, res.original, res.pinned)
	}
	for _, res := range r.httpResults {
		policy.AddHTTPChecksumRuleWithHeaders(pol, res.url, res.checksum, res.headers)
	}
	for _, res := range r.gitResults {
		policy.AddGitChecksumRule(pol, res.url, res.checksum)
	}

	return pol
}

// GeneratePolicy parses Dockerfiles and generates a source policy with pinned digests
func GeneratePolicy(ctx context.Context, opts Options) (*policy.Policy, error) {
	// Phase 1: Parse all Dockerfiles and collect unique sources
	collector := newTaskCollector()
	for _, dockerfilePath := range opts.Dockerfiles {
		if err := collector.collect(ctx, dockerfilePath); err != nil {
			return nil, err
		}
	}

	if collector.isEmpty() {
		return policy.NewPolicy(), nil
	}

	registryClient := registry.NewClient()

	// Phase 1.5: If DHI preference is enabled, verify authentication upfront
	if opts.PreferDHI {
		// Check if any images are eligible for DHI
		hasDHIEligible := false
		for _, task := range collector.imageTasks {
			if dhi.CanMapToDHI(task.ref) {
				hasDHIEligible = true
				break
			}
		}

		if hasDHIEligible {
			if err := registryClient.CheckAuth(ctx, dhi.Registry); err != nil {
				return nil, err
			}
		}
	}

	// Phase 2: Process all sources concurrently with progress bars
	progress := newProgressContainer()
	results := &resultCollector{}

	baseHTTPClient := httpclient.NewClient()
	gitClient := git.NewClient()

	g, ctx := errgroup.WithContext(ctx)

	for _, task := range collector.imageTasks {
		g.Go(processImage(ctx, task, registryClient, progress, results, opts.PreferDHI, opts.PreferECRPublic))
	}

	for _, task := range collector.httpTasks {
		g.Go(processHTTP(ctx, task, baseHTTPClient, progress, results))
	}

	for _, task := range collector.gitTasks {
		g.Go(processGit(ctx, task, gitClient, progress, results))
	}

	if err := g.Wait(); err != nil {
		progress.Wait()
		return nil, err
	}

	progress.Wait()

	return results.buildPolicy(), nil
}

func newProgressContainer() *mpb.Progress {
	isTTY := term.IsTerminal(int(os.Stderr.Fd()))
	var output io.Writer
	if isTTY {
		output = os.Stderr
	}
	return mpb.New(mpb.WithOutput(output))
}

func processImage(
	ctx context.Context,
	task imageTask,
	client *registry.Client,
	progress *mpb.Progress,
	results *resultCollector,
	preferDHI bool,
	preferECRPublic bool,
) func() error {
	return func() error {
		name := truncateLeft(task.original, 40)
		bar := progress.AddSpinner(
			0,
			mpb.PrependDecorators(
				decor.Name("📦 ", decor.WC{C: decor.DindentRight}),
				decor.Name(name, decor.WCSyncSpaceR),
			),
			mpb.AppendDecorators(
				decor.OnComplete(decor.Name("resolving..."), "✓"),
			),
		)
		defer bar.SetTotal(0, true)

		var pinnedRef reference.Named
		var digestStr string
		var err error

		// Try DHI (Docker Hardened Images) first if enabled
		if preferDHI {
			pinnedRef, digestStr, err = tryPreferredRegistry(
				ctx,
				task.ref,
				dhi.CanMapToDHI,
				dhi.MapToDHI,
				dhi.ErrNotEligible,
				client,
			)
			if err != nil {
				bar.Abort(true)
				return fmt.Errorf("failed to resolve DHI image for %s: %w", task.original, err)
			}
		}

		// Try ECR Public Gallery if enabled (and DHI wasn't used/found)
		if pinnedRef == nil && preferECRPublic {
			pinnedRef, digestStr, err = tryPreferredRegistry(
				ctx,
				task.ref,
				ecrpublic.CanMapToECRPublic,
				ecrpublic.MapToECRPublic,
				ecrpublic.ErrNotEligible,
				client,
			)
			if err != nil {
				bar.Abort(true)
				return fmt.Errorf("failed to resolve ECR Public image for %s: %w", task.original, err)
			}
		}

		// Fall back to original image if preferred registry not used or not found
		if pinnedRef == nil {
			digestStr, err = client.GetDigest(ctx, task.ref)
			if err != nil {
				bar.Abort(true)
				return fmt.Errorf("failed to get digest for %s: %w", task.original, err)
			}
			pinnedRef = task.ref
		}

		d, err := digest.Parse(digestStr)
		if err != nil {
			bar.Abort(true)
			return fmt.Errorf("failed to parse digest %s: %w", digestStr, err)
		}

		pinnedRefWithDigest, err := reference.WithDigest(pinnedRef, d)
		if err != nil {
			bar.Abort(true)
			return fmt.Errorf("failed to create pinned reference for %s: %w", task.original, err)
		}

		results.addPin(pinResult{
			index:    task.index,
			original: task.original,
			pinned:   pinnedRefWithDigest.String(),
		})

		return nil
	}
}

// tryPreferredRegistry attempts to resolve an image via a preferred registry mapper.
// Returns (mappedRef, digest, nil) on success, or (nil, "", nil) to signal fallback.
// Only returns a non-nil error on unexpected failures.
func tryPreferredRegistry(
	ctx context.Context,
	ref reference.Named,
	canMap func(reference.Named) bool,
	mapFn func(reference.Named) (reference.Named, error),
	notEligibleErr error,
	client *registry.Client,
) (reference.Named, string, error) {
	if !canMap(ref) {
		return nil, "", nil
	}
	mapped, err := mapFn(ref)
	if err != nil {
		if errors.Is(err, notEligibleErr) {
			return nil, "", nil
		}
		return nil, "", err
	}
	if mapped == nil {
		return nil, "", nil
	}
	digestStr, err := client.GetDigest(ctx, mapped)
	if err == nil {
		return mapped, digestStr, nil
	}
	if registry.IsNotFoundOrAuthError(err) {
		return nil, "", nil
	}
	return nil, "", fmt.Errorf("failed to check image %s: %w", mapped.String(), err)
}

func processHTTP(
	ctx context.Context,
	task httpTask,
	baseClient *httpclient.Client,
	progress *mpb.Progress,
	results *resultCollector,
) func() error {
	return func() error {
		// Extract filename from URL path, stripping query parameters to avoid leaking secrets
		displayName := task.url
		if u, err := url.Parse(task.url); err == nil {
			displayName = filepath.Base(u.Path)
		}
		name := truncateLeft(displayName, 40)
		bar := progress.AddBar(
			0,
			mpb.PrependDecorators(
				decor.Name("🌐 ", decor.WC{C: decor.DindentRight}),
				decor.Name(name, decor.WCSyncSpaceR),
			),
			mpb.AppendDecorators(
				decor.OnComplete(
					decor.Any(func(s decor.Statistics) string {
						if s.Total <= 0 {
							return "checking..."
						}
						//nolint:gosec // values are non-negative by design
						return humanize.Bytes(
							uint64(max(0, s.Current)),
						) + " / " + humanize.Bytes(
							uint64(max(0, s.Total)),
						)
					}),
					"✓",
				),
			),
		)

		httpClient := baseClient.WithProgressFactory(func(contentLength int64) io.Writer {
			if contentLength > 0 {
				bar.SetTotal(contentLength, false)
			}
			return &barWriter{bar: bar}
		})

		result, err := httpClient.GetChecksumWithHeaders(ctx, task.url)
		if err != nil {
			bar.Abort(true)
			if httpclient.IsAuthError(err) {
				log.Printf("Warning: Skipping %s (authentication required)", task.url)
				return nil
			}
			if httpclient.IsVolatileContentError(err) {
				log.Printf("Warning: Skipping %s (%s)", task.url, err.Error())
				return nil
			}
			return fmt.Errorf("failed to get checksum for %s: %w", task.url, err)
		}

		bar.SetTotal(bar.Current(), true)

		results.addHTTP(httpResult{
			index:    task.index,
			url:      task.url,
			checksum: result.Checksum,
			headers:  result.Headers,
		})

		return nil
	}
}

func processGit(
	ctx context.Context,
	task gitTask,
	client *git.Client,
	progress *mpb.Progress,
	results *resultCollector,
) func() error {
	return func() error {
		name := truncateLeft(task.url, 40)
		bar := progress.AddSpinner(
			0,
			mpb.PrependDecorators(
				decor.Name("📂 ", decor.WC{C: decor.DindentRight}),
				decor.Name(name, decor.WCSyncSpaceR),
			),
			mpb.AppendDecorators(
				decor.OnComplete(decor.Name("resolving..."), "✓"),
			),
		)
		defer bar.SetTotal(0, true)

		checksum, err := client.GetCommitChecksum(ctx, task.url)
		if err != nil {
			bar.Abort(true)
			return fmt.Errorf("failed to get commit checksum for %s: %w", task.url, err)
		}

		results.addGit(gitResult{
			index:    task.index,
			url:      task.url,
			checksum: checksum,
		})

		return nil
	}
}

// WritePolicy writes a policy to the given writer as JSON
func WritePolicy(w io.Writer, pol *policy.Policy) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(pol)
}

// truncateLeft truncates a string from the left if it exceeds maxLen
func truncateLeft(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return "..." + s[len(s)-(maxLen-3):]
}

// barWriter wraps an mpb.Bar to implement io.Writer
type barWriter struct {
	bar *mpb.Bar
}

func (w *barWriter) Write(p []byte) (int, error) {
	n := len(p)
	w.bar.IncrBy(n)
	return n, nil
}
