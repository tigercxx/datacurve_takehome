package qa

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	img "github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

type TestResult struct {
	OK       bool   `json:"ok"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
}

// RunTests clones repo@startCommit into a docker *volume*, applies finalPatch,
// then runs `cmd` inside `image` with /repo mounted read-write.
// Requires DOCKER_HOST to point to your DinD (e.g., tcp://dind:2375).
func RunTests(ctx context.Context, repoURL, startCommit, finalPatch, image, cmd string) (*TestResult, error) {
	log.Println("QA runner: starting test run")
	cli, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}
	log.Println("QA runner: connected to docker")

	if _, err := cli.Ping(ctx); err != nil {
		return nil, fmt.Errorf("cannot reach docker daemon (%s): %w", os.Getenv("DOCKER_HOST"), err)
	}
	log.Println("QA runner: docker daemon reachable")

	// We scope a generous timeout per phase
	phaseCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()

	log.Println("QA runner: pulling test image", image)
	if err := pullIfNeeded(phaseCtx, cli, image); err != nil {
		return nil, fmt.Errorf("pull test image %s: %w", image, err)
	}

	log.Println("QA runner: preparing volume and running tests")
	volName := fmt.Sprintf("qa-runner-%d", time.Now().UnixNano())

	// Create a named volume for repo contents
	if _, err := cli.VolumeCreate(phaseCtx, volume.CreateOptions{Name: volName}); err != nil {
		return nil, fmt.Errorf("volume create: %w", err)
	}
	// Always attempt cleanup on exit
	defer func() {
		if err := cli.VolumeRemove(context.Background(), volName, true); err != nil {
			log.Printf("warn: remove volume %s: %v", volName, err)
		}
	}()

	const gitImage = "alpine/git:latest"

	// Phase 1: clone (requires network)
	if err := pullIfNeeded(phaseCtx, cli, gitImage); err != nil {
		return nil, fmt.Errorf("pull %s: %w", gitImage, err)
	}
	log.Println("QA runner: cloning repo", repoURL, "commit", startCommit)
	// 1) git clone
	if err := runOneShotNet(phaseCtx, cli, gitImage, volName,
		[]string{"clone", repoURL, "/repo"},
		true, nil, true,
	); err != nil {
		return nil, fmt.Errorf("git clone phase: %w", err)
	}
	log.Println("QA runner: cloned repo")
	// 2) optional checkout
	if c := strings.TrimSpace(startCommit); c != "" && c != "HEAD" {
		if err := runOneShotNet(phaseCtx, cli, gitImage, volName,
			[]string{"-C", "/repo", "checkout", c},
			true, nil, true,
		); err != nil {
			return nil, fmt.Errorf("git checkout %q: %w", c, err)
		}
	}
	log.Println("QA runner: checked out commit", startCommit)

	// --- Phase 2: apply patch if provided ---
	if strings.TrimSpace(finalPatch) != "" {
		// Put /patch.diff into a tiny helper container (alpine/git has sh)
		if err := copyBytesToVolume(phaseCtx, cli, volName, "patch.diff", []byte(finalPatch)); err != nil {
			return nil, fmt.Errorf("copy patch: %w", err)
		}

		log.Println("QA runner: applying patch")
		applyCmd := []string{"-C", "/repo", "apply", "patch.diff"}
		if err := runOneShot(phaseCtx, cli, "alpine/git:latest", volName, applyCmd, true, nil); err != nil {
			return nil, fmt.Errorf("git apply: %w", err)
		}
		log.Println("QA runner: applied patch")
	}

	log.Println("QA runner: running tests with command:", cmd)

	// --- Phase 3: run tests ---
	// Security: disable network by default; set small resources as an example.
	res := &TestResult{}
	testCmd := []string{"sh", "-c", fmt.Sprintf("cd /repo && %s", cmd)}
	stdout, stderr, exitCode, err := runWithLogs(phaseCtx, cli, image, volName, testCmd, false, &container.Resources{
		Memory:   1 << 30, // 1 GiB
		NanoCPUs: 2e9,     // 2 CPUs
	})
	res.Stdout, res.Stderr, res.ExitCode = stdout, stderr, exitCode
	res.OK = (err == nil && exitCode == 0)
	if err != nil {
		// Return both error and captured logs/exit for debugging
		return res, fmt.Errorf("test run: %w", err)
	}
	return res, nil
}

// --- helpers ---

func pullIfNeeded(ctx context.Context, cli *client.Client, image string) error {
	reader, err := cli.ImagePull(ctx, imageRef(image), img.PullOptions{})
	if err != nil {
		return err
	}
	defer reader.Close()
	_, _ = io.Copy(io.Discard, reader) // eat the progress stream
	return nil
}

func imageRef(img string) string {
	// allow "python:3.12", "alpine/git:latest", etc.
	if strings.Contains(img, "/") || strings.Contains(img, ":") {
		return img
	}
	return "docker.io/library/" + img + ":latest"
}

func checkoutCmd(commit string) string {
	commit = strings.TrimSpace(commit)
	if commit == "" || commit == "HEAD" {
		return "true"
	}
	// allow branches/tags/sha
	return fmt.Sprintf("git checkout %q", commit)
}

// runOneShot runs a short-lived container with /repo mounted from a volume.
// If expectZeroExit is true, returns error on non-zero exit.
func runOneShot(ctx context.Context, cli *client.Client, image, volName string, cmd []string, expectZeroExit bool, res *container.Resources) error {
	stdout, stderr, exitCode, err := runWithLogs(ctx, cli, image, volName, cmd, false, res)
	if err != nil {
		// surface logs on error for easier debugging
		return fmt.Errorf("%s failed (exit=%d)\nstdout:\n%s\nstderr:\n%s\nerr: %w",
			strings.Join(cmd, " "), exitCode, stdout, stderr, err)
	}
	if expectZeroExit && exitCode != 0 {
		return fmt.Errorf("%s exit code=%d", strings.Join(cmd, " "), exitCode)
	}
	return nil
}

func runOneShotNet(ctx context.Context, cli *client.Client, image, volName string, cmd []string, expectZeroExit bool, res *container.Resources, netEnabled bool) error {
	stdout, stderr, exitCode, err := runWithLogs(ctx, cli, image, volName, cmd, netEnabled, res)
	if err != nil {
		// surface logs on error for easier debugging
		return fmt.Errorf("%s failed (exit=%d)\nstdout:\n%s\nstderr:\n%s\nerr: %w",
			strings.Join(cmd, " "), exitCode, stdout, stderr, err)
	}
	if expectZeroExit && exitCode != 0 {
		return fmt.Errorf("%s exit code=%d\nstdout:\n%s\nstderr:\n%s",
			strings.Join(cmd, " "), exitCode, stdout, stderr)
	}
	return nil
}

// runWithLogs creates a container, attaches /repo volume, runs cmd, collects logs, cleans up.
func runWithLogs(ctx context.Context, cli *client.Client, image, volName string, cmd []string, netEnabled bool, res *container.Resources) (stdout, stderr string, exitCode int, err error) {
	networkMode := container.NetworkMode("none")
	if netEnabled {
		networkMode = ""
	}
	hostCfg := &container.HostConfig{
		NetworkMode: networkMode,
		Mounts: []mount.Mount{{
			Type:   mount.TypeVolume,
			Source: volName,
			Target: "/repo",
		}},
	}
	if res != nil {
		hostCfg.Resources = *res
	}

	create, err := cli.ContainerCreate(ctx, &container.Config{
		Image: image,
		Cmd:   cmd,
		Tty:   false,
	}, hostCfg, nil, nil, "")
	if err != nil {
		return "", "", 0, fmt.Errorf("create: %w", err)
	}
	cid := create.ID
	defer func() {
		timeout := 5
		_ = cli.ContainerStop(context.Background(), cid, container.StopOptions{Timeout: &timeout})
		_ = cli.ContainerRemove(context.Background(), cid, container.RemoveOptions{Force: true})
	}()

	if err := cli.ContainerStart(ctx, cid, container.StartOptions{}); err != nil {
		return "", "", 0, fmt.Errorf("start: %w", err)
	}

	// Wait for exit
	statusCh, errCh := cli.ContainerWait(ctx, cid, container.WaitConditionNotRunning)
	select {
	case err = <-errCh:
		if err != nil {
			return "", "", 0, fmt.Errorf("wait: %w", err)
		}
	case st := <-statusCh:
		exitCode = int(st.StatusCode)
	}

	// Collect logs
	var outBuf, errBuf bytes.Buffer
	logs, err := cli.ContainerLogs(ctx, cid, container.LogsOptions{ShowStdout: true, ShowStderr: true})
	if err == nil {
		defer logs.Close()
		// demux stream (it's a multiplexed stdcopy stream)
		// but for simplicity, read all and split by heuristic:
		// better: use stdcopy.StdCopy
		var sb bytes.Buffer
		_, _ = io.Copy(&sb, logs)
		// Prefer stdcopy to separate streams:
		outBuf.Reset()
		errBuf.Reset()
		if _, err := stdcopy.StdCopy(&outBuf, &errBuf, bytes.NewReader(sb.Bytes())); err != nil {
			// fallback: put everything into stdout
			outBuf = sb
		}
	}
	return outBuf.String(), errBuf.String(), exitCode, nil
}

// Copy a small file into the /repo volume by spinning a helper container that mounts the volume,
// then using the "archive upload" API (CopyToContainer).
func copyBytesToVolume(ctx context.Context, cli *client.Client, volName, destPath string, data []byte) error {
	log.Printf("copyBytesToVolume: copying to %s in volume %s", destPath, volName)
	// Helper container (alpine/git) with /repo mounted
	create, err := cli.ContainerCreate(ctx, &container.Config{
		Image: "alpine/git:latest",
		Cmd:   []string{"sleep", "60"},
		Tty:   false,
	}, &container.HostConfig{
		Mounts: []mount.Mount{{
			Type:   mount.TypeVolume,
			Source: volName,
			Target: "/repo",
		}},
	}, nil, nil, "")
	if err != nil {
		return fmt.Errorf("copy helper create: %w", err)
	}
	cid := create.ID
	defer func() {
		timeout := 2
		_ = cli.ContainerStop(context.Background(), cid, container.StopOptions{Timeout: &timeout})
		_ = cli.ContainerRemove(context.Background(), cid, container.RemoveOptions{Force: true})
	}()

	if err := cli.ContainerStart(ctx, cid, container.StartOptions{}); err != nil {
		return fmt.Errorf("copy helper start: %w", err)
	}

	// Build a tar archive with the file at the requested path
	tarBuf := new(bytes.Buffer)
	tw := tar.NewWriter(tarBuf)
	hdr := &tar.Header{
		Name: destPath,
		Mode: 0644,
		Size: int64(len(data)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	if _, err := tw.Write(data); err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}

	// Upload to /repo (destPath is absolute inside container root)
	path := "/repo"
	log.Printf("copyBytesToVolume: uploading to path %s with destPath %s", path, destPath)
	err = cli.CopyToContainer(ctx, cid, path, bytes.NewReader(tarBuf.Bytes()), container.CopyToContainerOptions{AllowOverwriteDirWithFile: true})
	if err != nil {
		log.Printf("copyBytesToVolume: CopyToContainer failed: %v", err)
		return err
	}
	log.Printf("copyBytesToVolume: successfully copied %s", destPath)
	return nil
}
