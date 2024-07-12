package build

import (
	"archive/tar"
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/Microsoft/hcsshim/ext4/tar2ext4"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/consts"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/telemetry"
	"github.com/X-code-interpreter/sandbox-backend/packages/template-manager/constants"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const (
	ToMBShift = 20
	// Max size of the rootfs file in MB.
	maxRootfsSize = 15000 << ToMBShift
	cacheTimeout  = "48h"
)

//go:embed overlay-init
var overlayInitContent []byte

type Rootfs struct {
	docker *client.Client
	env    *Env
}

func NewRootfs(ctx context.Context, tracer trace.Tracer, docker *client.Client, env *Env) (*Rootfs, error) {
	childCtx, childSpan := tracer.Start(ctx, "new-rootfs")
	defer childSpan.End()

	rootfs := &Rootfs{
		docker: docker,
		env:    env,
	}

	// if user set NoPull explictly, then do not pull from registry
	if !env.NoPull {
		// TODO(huang-jl): remove docker image when failed ?
		err := rootfs.pullDockerImage(childCtx, tracer)
		if err != nil {
			errMsg := fmt.Errorf("error building docker image: %w", err)
			return nil, errMsg
		}
	}

	err := rootfs.createRootfsFile(childCtx, tracer)
	if err != nil {
		errMsg := fmt.Errorf("error creating rootfs file: %w", err)
		return nil, errMsg
	}

	return rootfs, nil
}

// TODO(huang-jl): do we need auth (in image.PullOptions)?
func (r *Rootfs) pullDockerImage(ctx context.Context, tracer trace.Tracer) error {
	childCtx, childSpan := tracer.Start(ctx, "pull-docker-image")
	defer childSpan.End()

	logs, err := r.docker.ImagePull(childCtx, r.dockerTag(), image.PullOptions{
		Platform: "linux/amd64",
	})
	if err != nil {
		errMsg := fmt.Errorf("error pulling image: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}

	_, err = io.Copy(os.Stdout, logs)
	if err != nil {
		errMsg := fmt.Errorf("error copying logs: %w", err)
		telemetry.ReportError(childCtx, errMsg)

		return errMsg
	}

	err = logs.Close()
	if err != nil {
		errMsg := fmt.Errorf("error closing logs: %w", err)
		telemetry.ReportError(childCtx, errMsg)

		return errMsg
	}

	telemetry.ReportEvent(childCtx, "pulled image")

	return nil
}

func (r *Rootfs) dockerTag() string {
	if r.env.DockerImage == "" {
		return "e2bdev/code-interpreter:latest"
	}
	return r.env.DockerImage
}

// This is a complex function
// it will
//  1. create a rootfs (ext4) through docker container
//     it will populate the necessary services.
//  2. start a FC and generate snapshot file
func (r *Rootfs) createRootfsFile(ctx context.Context, tracer trace.Tracer) error {
	childCtx, childSpan := tracer.Start(ctx, "create-rootfs-file")
	defer childSpan.End()

	var err error
	var scriptDef bytes.Buffer

	err = EnvInstanceTemplate.Execute(&scriptDef, struct {
		EnvID    string
		StartCmd string
	}{
		EnvID:    r.env.EnvID,
		StartCmd: strings.ReplaceAll(r.env.StartCmd, "\"", "\\\""),
	})
	if err != nil {
		errMsg := fmt.Errorf("error executing provision script: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}

	telemetry.ReportEvent(childCtx, "executed provision script env")

	overlayInitTmp, err := os.CreateTemp("", "overlay-init")
	if err != nil {
		errMsg := fmt.Errorf("error create temp file for overlay-init: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}
	if _, err := overlayInitTmp.Write(overlayInitContent); err != nil {
		errMsg := fmt.Errorf("error write overlay-init temp file: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}
	telemetry.ReportEvent(childCtx, "overlay-init temp file created")
	defer func() {
		overlayInitTmp.Close()
		os.Remove(overlayInitTmp.Name())
	}()

	pidsLimit := int64(200)

	cont, err := r.docker.ContainerCreate(childCtx, &container.Config{
		Image:        r.dockerTag(),
		Entrypoint:   []string{"/bin/bash", "-c"},
		User:         "root",
		Cmd:          []string{scriptDef.String()},
		Tty:          false,
		AttachStdout: true,
		AttachStderr: true,
		// TODO(huang-jl) provide option to setup proxy
		// Env: []string{"https_proxy=http://172.17.0.1:7890", "http_proxy=http://172.17.0.1:7890"},
	}, &container.HostConfig{
		SecurityOpt: []string{"no-new-privileges"},
		CapAdd:      []string{"CHOWN", "DAC_OVERRIDE", "FSETID", "FOWNER", "SETGID", "SETUID", "NET_RAW", "SYS_CHROOT"},
		CapDrop:     []string{"ALL"},
		// TODO: Network mode is causing problems with /etc/hosts - we want to find a way to fix this and enable network mode again
		// NetworkMode: container.NetworkMode(network.ID),
		Resources: container.Resources{
			Memory:     r.env.MemoryMB << ToMBShift,
			CPUPeriod:  100000,
			CPUQuota:   r.env.VCpuCount * 100000,
			MemorySwap: r.env.MemoryMB << ToMBShift,
			PidsLimit:  &pidsLimit,
		},
	}, nil, &v1.Platform{}, "")
	if err != nil {
		errMsg := fmt.Errorf("error creating container: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}

	telemetry.ReportEvent(childCtx, "created container")

	defer func() {
		go func() {
			cleanupContext, cleanupSpan := tracer.Start(
				trace.ContextWithSpanContext(context.Background(), childSpan.SpanContext()),
				"cleanup-container",
			)
			defer cleanupSpan.End()

			removeErr := r.docker.ContainerRemove(cleanupContext, cont.ID, container.RemoveOptions{
				RemoveVolumes: true,
				Force:         true,
			})
			if removeErr != nil {
				errMsg := fmt.Errorf("error removing container: %w", removeErr)
				telemetry.ReportError(cleanupContext, errMsg)
			} else {
				telemetry.ReportEvent(cleanupContext, "removed container")
			}

			// Move prunning to separate goroutine
			cacheTimeoutArg := filters.Arg("until", cacheTimeout)

			_, pruneErr := r.docker.BuildCachePrune(cleanupContext, types.BuildCachePruneOptions{
				Filters: filters.NewArgs(cacheTimeoutArg),
				All:     true,
			})
			if pruneErr != nil {
				errMsg := fmt.Errorf("error pruning build cache: %w", pruneErr)
				telemetry.ReportError(cleanupContext, errMsg)
			} else {
				telemetry.ReportEvent(cleanupContext, "pruned build cache")
			}

			_, pruneErr = r.docker.ImagesPrune(cleanupContext, filters.NewArgs(cacheTimeoutArg))
			if pruneErr != nil {
				errMsg := fmt.Errorf("error pruning images: %w", pruneErr)
				telemetry.ReportError(cleanupContext, errMsg)
			} else {
				telemetry.ReportEvent(cleanupContext, "pruned images")
			}

			_, pruneErr = r.docker.ContainersPrune(cleanupContext, filters.NewArgs(cacheTimeoutArg))
			if pruneErr != nil {
				errMsg := fmt.Errorf("error pruning containers: %w", pruneErr)
				telemetry.ReportError(cleanupContext, errMsg)
			} else {
				telemetry.ReportEvent(cleanupContext, "pruned containers")
			}
		}()
	}()

	filesToTar := []fileToTar{
		{
			localPath: consts.HostEnvdPath,
			tarPath:   consts.GuestEnvdPath,
		},
		{
			localPath: overlayInitTmp.Name(),
			tarPath:   constants.OverlayInitPath,
		},
	}
	pr, pw := io.Pipe()

	go func() {
		defer func() {
			closeErr := pw.Close()
			if closeErr != nil {
				errMsg := fmt.Errorf("error closing pipe: %w", closeErr)
				telemetry.ReportCriticalError(childCtx, errMsg)
			} else {
				telemetry.ReportEvent(childCtx, "closed pipe")
			}
		}()

		tw := tar.NewWriter(pw)
		defer func() {
			err = tw.Close()
			if err != nil {
				errMsg := fmt.Errorf("error closing tar writer: %w", err)
				telemetry.ReportCriticalError(childCtx, errMsg)
			} else {
				telemetry.ReportEvent(childCtx, "closed tar writer")
			}
		}()

		for _, file := range filesToTar {
			addErr := addFileToTarWriter(tw, file)
			if addErr != nil {
				errMsg := fmt.Errorf("error adding envd to tar writer: %w", addErr)
				telemetry.ReportCriticalError(childCtx, errMsg)

				return
			} else {
				telemetry.ReportEvent(childCtx, "added envd to tar writer")
			}
		}
	}()

	// Copy tar to the container
	err = r.docker.CopyToContainer(childCtx, cont.ID, "/", pr, types.CopyToContainerOptions{
		AllowOverwriteDirWithFile: true,
	})
	if err != nil {
		errMsg := fmt.Errorf("error copying envd to container: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}

	telemetry.ReportEvent(childCtx, "copied envd to container")

	err = r.docker.ContainerStart(childCtx, cont.ID, container.StartOptions{})
	if err != nil {
		errMsg := fmt.Errorf("error starting container: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}

	telemetry.ReportEvent(childCtx, "started container")

	go func() {
		anonymousChildCtx, anonymousChildSpan := tracer.Start(childCtx, "handle-container-logs", trace.WithSpanKind(trace.SpanKindConsumer))
		defer anonymousChildSpan.End()

		containerStdoutWriter := telemetry.NewEventWriter(anonymousChildCtx, "stdout")
		containerStderrWriter := telemetry.NewEventWriter(anonymousChildCtx, "stderr")

		logs, logsErr := r.docker.ContainerLogs(childCtx, cont.ID, container.LogsOptions{
			ShowStdout: true,
			ShowStderr: true,
			Timestamps: false,
			Follow:     true,
		})
		if logsErr != nil {
			errMsg := fmt.Errorf("error getting container logs: %w", logsErr)
			telemetry.ReportError(anonymousChildCtx, errMsg)
		}
		_, logsErr = stdcopy.StdCopy(containerStdoutWriter, containerStderrWriter, logs)
		if logsErr != nil {
			errMsg := fmt.Errorf("error copy container logs: %w", logsErr)
			telemetry.ReportError(anonymousChildCtx, errMsg)
		} else {
			telemetry.ReportEvent(anonymousChildCtx, "copy container logs")
		}
	}()

	wait, errWait := r.docker.ContainerWait(childCtx, cont.ID, container.WaitConditionNotRunning)
	select {
	case <-childCtx.Done():
		errMsg := fmt.Errorf("error waiting for container: %w", childCtx.Err())
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	case waitErr := <-errWait:
		if waitErr != nil {
			errMsg := fmt.Errorf("error waiting for container: %w", waitErr)
			telemetry.ReportCriticalError(childCtx, errMsg)

			return errMsg
		}
	case response := <-wait:
		if response.Error != nil {
			errMsg := fmt.Errorf("error waiting for container - code %d: %s", response.StatusCode, response.Error.Message)
			telemetry.ReportCriticalError(childCtx, errMsg)

			return errMsg
		}
	}

	telemetry.ReportEvent(childCtx, "waited for container exit")

	inspection, err := r.docker.ContainerInspect(ctx, cont.ID)
	if err != nil {
		errMsg := fmt.Errorf("error inspecting container: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}

	telemetry.ReportEvent(childCtx, "inspected container")

	if inspection.State.Running {
		errMsg := fmt.Errorf("container is still running")
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}

	if inspection.State.ExitCode != 0 {
		errMsg := fmt.Errorf("container exited with status %d: %s", inspection.State.ExitCode, inspection.State.Error)
		telemetry.ReportCriticalError(
			childCtx,
			errMsg,
			attribute.Int("exit_code", inspection.State.ExitCode),
			attribute.String("error", inspection.State.Error),
			attribute.Bool("oom", inspection.State.OOMKilled),
		)

		return errMsg
	}

	rootfsFile, err := os.Create(r.env.tmpRootfsPath())
	if err != nil {
		errMsg := fmt.Errorf("error creating rootfs file: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}

	telemetry.ReportEvent(childCtx, "created rootfs file")

	defer func() {
		rootfsErr := rootfsFile.Close()
		if rootfsErr != nil {
			errMsg := fmt.Errorf("error closing rootfs file: %w", rootfsErr)
			telemetry.ReportError(childCtx, errMsg)
		} else {
			telemetry.ReportEvent(childCtx, "closed rootfs file")
		}
	}()

	// TODO(huang-jl) use export?
	rootTar, downloadErr := r.docker.ContainerExport(childCtx, cont.ID)
	// rootTar, _, downloadErr := r.docker.CopyFromContainer(childCtx, cont.ID, "/")
	// downloadErr := r.docker.CopyFromContainer(cont.ID, docker.DownloadFromContainerOptions{
	// 	Context:      childCtx,
	// 	Path:         "/",
	// 	OutputStream: pw,
	// })
	if downloadErr != nil {
		errMsg := fmt.Errorf("error downloading from container: %w", downloadErr)
		telemetry.ReportCriticalError(childCtx, errMsg)
	}
	telemetry.ReportEvent(childCtx, "downloaded from container")
	defer rootTar.Close()

	// This package creates a read-only ext4 filesystem from a tar archive.
	// We need to use another program to make the filesystem writable.
	err = tar2ext4.ConvertTarToExt4(rootTar, rootfsFile, tar2ext4.MaximumDiskSize(maxRootfsSize))
	if err != nil {
		errMsg := fmt.Errorf("error converting tar to ext4: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}

	telemetry.ReportEvent(childCtx, "converted container tar to ext4")

	// if err = r.makeRootfsWritable(childCtx, tracer); err != nil {
	// 	errMsg := fmt.Errorf("error making rootfs file writable: %w", err)
	// 	telemetry.ReportCriticalError(childCtx, errMsg)

	// 	return errMsg
	// }
	// telemetry.ReportEvent(childCtx, "made rootfs file writable")

	// if err = r.resizeRootfs(childCtx, tracer, rootfsFile); err != nil {
	// 	errMsg := fmt.Errorf("error resizing rootfs file: %w", err)
	// 	telemetry.ReportCriticalError(childCtx, errMsg)

	// 	return errMsg
	// }
	// telemetry.ReportEvent(childCtx, "resized rootfs file")

	if err = r.prepareWritableRootfs(childCtx, tracer); err != nil {
		errMsg := fmt.Errorf("error prepare writable roofs file: %w", err)
		telemetry.ReportCriticalError(childCtx, errMsg)

		return errMsg
	}

	return nil
}

func (r *Rootfs) prepareWritableRootfs(ctx context.Context, tracer trace.Tracer) error {
	childCtx, childSpan := tracer.Start(ctx, "prepare-writable-rootfs")
	defer childSpan.End()
	writableRootfs, err := os.Create(r.env.tmpWritableRootfsPath())
	if err != nil {
		errMsg := fmt.Errorf("error creating writable rootfs file")
		telemetry.ReportCriticalError(childCtx, errMsg)
		return errMsg
	}
	defer writableRootfs.Close() // ignore error here

	if err := writableRootfs.Truncate(r.env.DiskSizeMB << ToMBShift); err != nil {
		errMsg := fmt.Errorf("error truncate writable rootfs file")
		telemetry.ReportCriticalError(childCtx, errMsg)
		return errMsg
	}

	cmd := exec.CommandContext(childCtx, "mkfs.ext4", r.env.tmpWritableRootfsPath())
	mkfsStdoutWriter := telemetry.NewEventWriter(childCtx, "stdout")
	cmd.Stdout = mkfsStdoutWriter

	mkfsStderrWriter := telemetry.NewEventWriter(childCtx, "stderr")
	cmd.Stderr = mkfsStderrWriter

	return cmd.Run()
}

func (r *Rootfs) makeRootfsWritable(ctx context.Context, tracer trace.Tracer) error {
	tuneContext, tuneSpan := tracer.Start(ctx, "tune-rootfs-file-cmd")
	defer tuneSpan.End()

	cmd := exec.CommandContext(tuneContext, "tune2fs", "-O ^read-only", r.env.tmpRootfsPath())

	tuneStdoutWriter := telemetry.NewEventWriter(tuneContext, "stdout")
	cmd.Stdout = tuneStdoutWriter

	tuneStderrWriter := telemetry.NewEventWriter(tuneContext, "stderr")
	cmd.Stderr = tuneStderrWriter

	return cmd.Run()
}

// 1. use truncate to enlarge the rootfs ext4 image
// 2. use resize2fs to make the ext4 image recognize the previous truncate
func (r *Rootfs) resizeRootfs(ctx context.Context, tracer trace.Tracer, rootfsFile *os.File) error {
	resizeContext, resizeSpan := tracer.Start(ctx, "resize-rootfs-file-cmd")
	defer resizeSpan.End()

	rootfsStats, err := rootfsFile.Stat()
	if err != nil {
		errMsg := fmt.Errorf("error statting rootfs file: %w", err)
		telemetry.ReportCriticalError(resizeContext, errMsg)

		return errMsg
	}

	telemetry.ReportEvent(resizeContext, "statted rootfs file")

	// In bytes
	rootfsSize := rootfsStats.Size() + r.env.DiskSizeMB<<ToMBShift

	r.env.rootfsSize = rootfsSize

	err = rootfsFile.Truncate(rootfsSize)
	if err != nil {
		errMsg := fmt.Errorf("error truncating rootfs file: %w to size of build + defaultDiskSizeMB", err)
		telemetry.ReportCriticalError(resizeContext, errMsg)

		return errMsg
	}

	telemetry.ReportEvent(resizeContext, "truncated rootfs file to size of build + defaultDiskSizeMB")

	cmd := exec.CommandContext(resizeContext, "resize2fs", r.env.tmpRootfsPath())

	resizeStdoutWriter := telemetry.NewEventWriter(resizeContext, "stdout")
	cmd.Stdout = resizeStdoutWriter

	resizeStderrWriter := telemetry.NewEventWriter(resizeContext, "stderr")
	cmd.Stderr = resizeStderrWriter

	return cmd.Run()
}
